package controllers

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	podLog.Info("giving up after 5 errors to reconcile")
}

func (r *CIJobReconciler) safeDelete(ctx context.Context, nsName types.NamespacedName, obj runtime.Object) error {
	if err := r.Get(ctx, nsName, obj); !apierrors.IsNotFound(err) {
		return client.IgnoreNotFound(r.Delete(ctx, obj))
	}

	return nil
}

func (r *CIJobReconciler) safeCreate(ctx context.Context, obj runtime.Object) error {
	if err := r.Client.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
		return err
	}

	return r.Client.Create(ctx, obj)
}

type deleteItem struct {
	logName string
	nsName  types.NamespacedName
	obj     runtime.Object
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

		toDelete := []deleteItem{
			{
				logName: "git repository",
				nsName:  getGitName(req),
				obj:     &sourcev1alpha1.GitRepository{},
			},
			{
				logName: "git secrets",
				nsName:  getSecretName(req),
				obj:     &corev1.Secret{},
			},
			{
				logName: "pod",
				nsName:  getPodName(req),
				obj:     &corev1.Pod{},
			},
		}

		for _, del := range toDelete {
			r.Log.Info("deleting resource", "type", del.logName)
			if err := r.safeDelete(ctx, del.nsName, del.obj); err != nil {
				return defaultResult, err
			}
		}

		return defaultResult, nil
	}

	if err := cijob.Validate(); err != nil {
		r.Log.Error(err, "encountered cijob validation error")
		return defaultResult, err
	}

	_, err := r.getPod(ctx, req)
	if err != nil {
		gn := getGitName(req)
		sn := getSecretName(req)

		if err := r.safeCreate(ctx, cijob.Secret(sn)); err != nil {
			return defaultResult, err
		}

		if err := r.safeCreate(ctx, cijob.GitRepository(gn, sn.Name)); err != nil {
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

			if err := r.Client.Get(ctx, gn, fetchedRepo); client.IgnoreNotFound(err) != nil {
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
