package controllers

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sourcev1alpha1 "github.com/fluxcd/source-controller/api/v1alpha1"
	objectsv1alpha1 "github.com/tinyci/k8s-api/api/v1alpha1"
)

var defaultResult = ctrl.Result{}

// CIJobReconciler reconciles a CIJob object
type CIJobReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *CIJobReconciler) getPod(ctx context.Context, req ctrl.Request) (*corev1.Pod, error) {
	pod := &corev1.Pod{}

	podLog := r.getPodLogger(req)
	nsName := getPodName(req)

	podLog.Info("retrieving pod information for CI job", "pod", nsName)
	if err := r.Get(ctx, nsName, pod); err != nil {
		podLog.Info("pod could not be found for CI job", "pod", nsName)
		return nil, err
	}

	return pod, nil
}

func (r *CIJobReconciler) getPodLogger(req ctrl.Request) logr.Logger {
	return r.Log.WithValues("cijob", req.NamespacedName, "pod", getPodName(req))
}

func getName(req ctrl.Request, append string) types.NamespacedName {
	nsName := req.NamespacedName
	// FIXME this should probably be less stupid
	nsName.Name += "-" + append

	return nsName
}

func getPodName(req ctrl.Request) types.NamespacedName {
	return getName(req, "pod")
}

func getGitName(req ctrl.Request) types.NamespacedName {
	return getName(req, "git")
}

func getSecretName(req ctrl.Request) types.NamespacedName {
	return getName(req, "secret")
}

func getState(pod *corev1.Pod) *corev1.ContainerStateTerminated {
	return pod.Status.ContainerStatuses[0].State.Terminated
}

func (r *CIJobReconciler) supervisePod(ctx context.Context, req ctrl.Request) {
	podLog := r.getPodLogger(req)
	podLog.Info("starting status supervisor")
	errCount := 0

	for errCount < 5 {
		time.Sleep(time.Second)

		pod, err := r.getPod(ctx, req)
		if err != nil {
			podLog.Info("error while retrieving ci job pod", "error", err.Error())
			errCount++
			continue
		}

		state := getState(pod)
		if pod.Status.Phase != corev1.PodPending && state != nil {
			cijob := &objectsv1alpha1.CIJob{}
			if err := r.Get(ctx, req.NamespacedName, cijob); err != nil {
				podLog.Info("pod is finished; could not retrieve CI job", "error", err.Error())
				errCount++
				continue
			}

			cijob.Status.Finished = true
			cijob.Status.Success = state.ExitCode == 0

			if err := r.Update(ctx, cijob); err != nil {
				podLog.Info("error updating cijob with run state", "error", err.Error())
				errCount++
				continue
			}

			return
		}
	}

	podLog.Info("giving up after 5 tries to reconcile")
}

// +kubebuilder:rbac:groups=objects.tinyci.org,resources=cijobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=objects.tinyci.org,resources=cijobs/status,verbs=get;update;patch

// Reconcile the resource
func (r *CIJobReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("cijob", req.NamespacedName)

	cijob := &objectsv1alpha1.CIJob{}

	if err := r.Get(ctx, req.NamespacedName, cijob); err != nil {
		log.Info("cijob removed")

		pod, err := r.getPod(ctx, req)
		if err != nil {
			return defaultResult, client.IgnoreNotFound(err)
		}

		r.getPodLogger(req).Info("deleting pod")

		if err := r.Delete(ctx, pod); err != nil {
			return defaultResult, err
		}

		return defaultResult, client.IgnoreNotFound(err)
	}

	if err := cijob.Validate(); err != nil {
		r.Log.Error(err, "encountered cijob validation error")
		return defaultResult, err
	}

	_, err := r.getPod(ctx, req)
	if err != nil {
		sn := getSecretName(req)
		gn := getGitName(req)

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: sn.Namespace,
				Name:      sn.Name,
			},
			StringData: map[string]string{
				"username": "",
				"password": cijob.Spec.Repository.Token,
			},
		}

		if err := r.Client.Delete(ctx, secret); client.IgnoreNotFound(err) != nil {
			return defaultResult, err
		}

		if err := r.Client.Create(ctx, secret); err != nil {
			return defaultResult, err
		}

		repo := &sourcev1alpha1.GitRepository{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: gn.Namespace,
				Name:      gn.Name,
			},
			Spec: sourcev1alpha1.GitRepositorySpec{
				URL:       cijob.Spec.Repository.URL,
				Interval:  metav1.Duration{Duration: time.Hour},
				SecretRef: &corev1.LocalObjectReference{Name: getSecretName(req).Name},
			},
		}

		if err := r.Client.Delete(ctx, repo); client.IgnoreNotFound(err) != nil {
			return defaultResult, err
		}

		if err := r.Client.Create(ctx, repo); err != nil {
			return defaultResult, err
		}

		for {
			select {
			case <-ctx.Done():
				// FIXME probably need to cleanup here
				return defaultResult, ctx.Err()
			default:
			}

			fetchedRepo := &sourcev1alpha1.GitRepository{}

			if err := r.Client.Get(ctx, gn, fetchedRepo); err != nil {
				return defaultResult, err
			}

			if fetchedRepo.Status.Artifact != nil && fetchedRepo.Status.Artifact.URL != "" {
				r.Log.Info("fetched repository, moving forward with create", "artifact", fetchedRepo.Status.Artifact.URL)
				break
			}
		}

		// FIXME sew artifact into container image

		if err := r.Client.Create(ctx, cijob.Pod(getPodName(req))); err != nil {
			return defaultResult, err
		}

		go r.supervisePod(ctx, req)

		// only the name can be used here, otherwise badness in the runner. We already know the namespace.
		cijob.Status.PodName = getPodName(req).Name
		return defaultResult, r.Update(ctx, cijob)
	}

	return defaultResult, nil
}

// SetupWithManager sets up the manager by installing the controller
func (r *CIJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&objectsv1alpha1.CIJob{}).
		Owns(&sourcev1alpha1.GitRepository{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
