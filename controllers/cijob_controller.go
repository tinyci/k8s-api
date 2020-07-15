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

var (
	defaultResult = ctrl.Result{}
	requeueResult = ctrl.Result{Requeue: true}
)

// CIJobReconciler reconciles a CIJob object
type CIJobReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *CIJobReconciler) getGit(req ctrl.Request) *supervisedGit {
	return &supervisedGit{
		request: request{
			log: r.Log,
			req: req,
		},
	}
}

func (r *CIJobReconciler) getPod(req ctrl.Request) *supervisedPod {
	return &supervisedPod{
		request: request{
			log: r.Log,
			req: req,
		},
	}
}

func (r *CIJobReconciler) getCIJob(req ctrl.Request) *supervisedCIJob {
	return &supervisedCIJob{
		request: request{
			log: r.Log,
			req: req,
		},
	}
}

// +kubebuilder:rbac:groups=objects.tinyci.org,resources=cijobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=objects.tinyci.org,resources=cijobs/status,verbs=get;update;patch

// Reconcile the resource
func (r *CIJobReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.getCIJob(req).Logger()

	cijob := &objectsv1alpha1.CIJob{}

	if err := r.Get(ctx, req.NamespacedName, cijob); err != nil {
		log.Error(err, "encountered err while retrieving record")
		return requeueResult, err
	}

	if err := cijob.Validate(); err != nil {
		log.Error(err, "encountered cijob validation error")
		return defaultResult, err
	}

	pod := &corev1.Pod{}

	if err := r.Get(ctx, r.getPod(req).Name(), pod); apierrors.IsNotFound(err) {
		return defaultResult, r.buildJob(ctx, req)
	} else if err != nil {
		return requeueResult, err
	} else {
		// was created and we just restarted
		go r.supervisePod(ctx, req)
	}

	return defaultResult, nil
}

// SetupWithManager sets up the manager by installing the controller
func (r *CIJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&objectsv1alpha1.CIJob{}).
		Complete(r)
}

func (r *CIJobReconciler) supervisePod(ctx context.Context, req ctrl.Request) {
	intpod := r.getPod(req)

	podLog := intpod.Logger()
	podLog.Info("starting status supervisor")
	errCount := 0

	for errCount < 5 {
		time.Sleep(5 * time.Second)

		pod := &corev1.Pod{}

		if err := r.Get(ctx, intpod.Name(), pod); err != nil {
			podLog.Error(err, "error while retrieving ci job pod")
			errCount++
			continue
		}

		state := getState(pod)
		switch pod.Status.Phase {
		case corev1.PodPending:
		case corev1.PodFailed:
			podLog.Info("Pod failed, recreating job")
			cijob := &objectsv1alpha1.CIJob{}
			if err := r.Get(ctx, req.NamespacedName, cijob); err != nil {
				podLog.Error(err, "pod is in failed state; could not retrieve CI job")
				errCount++
				goto end
			}

			if err := r.Delete(ctx, cijob); client.IgnoreNotFound(err) != nil {
				podLog.Error(err, "failed cijob could not be deleted")
				errCount++
				goto end
			}

			cijob.ResourceVersion = ""

			if err := r.Create(ctx, cijob); err != nil {
				podLog.Error(err, "failed cijob could not be recreated")
				errCount++
				goto end
			}

			return
		case corev1.PodSucceeded:
			if state != nil {
				cijob := &objectsv1alpha1.CIJob{}
				if err := r.Get(ctx, req.NamespacedName, cijob); err != nil {
					podLog.Error(err, "pod is finished; could not retrieve CI job")
					errCount++
					goto end
				}

				cijob.Status.Finished = true
				cijob.Status.Success = state.ExitCode == 0

				if err := r.Update(ctx, cijob); err != nil {
					podLog.Error(err, "error updating cijob with run state")
					errCount++
					goto end
				}

				return
			}
		default:
			return
		}
	end:
	}

	podLog.Info("giving up after 5 errors to reconcile")
}

func (r *CIJobReconciler) waitForRepository(ctx context.Context, gn types.NamespacedName) (*sourcev1alpha1.GitRepository, error) {
	var fetchedRepo *sourcev1alpha1.GitRepository

	for {
		select {
		case <-ctx.Done():
			// FIXME probably need to cleanup here
			return nil, ctx.Err()
		default:
		}

		fetchedRepo = &sourcev1alpha1.GitRepository{}

		if err := r.Client.Get(ctx, gn, fetchedRepo); client.IgnoreNotFound(err) != nil {
			return nil, err
		}

		if fetchedRepo.Status.Artifact != nil && fetchedRepo.Status.Artifact.URL != "" {
			r.Log.Info("fetched repository, moving forward with create", "artifact", fetchedRepo.Status.Artifact.URL)
			break
		}
	}

	return fetchedRepo.DeepCopy(), nil
}

func (r *CIJobReconciler) buildJob(ctx context.Context, req ctrl.Request) error {
	git := r.getGit(req)
	pod := r.getPod(req)

	cijob := &objectsv1alpha1.CIJob{}

	if err := r.Get(ctx, req.NamespacedName, cijob); err != nil {
		return err
	}

	if err := r.Create(ctx, cijob.GitRepository(git.Name())); err != nil {
		return err
	}

	repo, err := r.waitForRepository(ctx, git.Name())
	if err != nil {
		return err
	}

	if err := r.Client.Create(ctx, cijob.Pod(pod.Name(), repo)); err != nil {
		return err
	}

	go r.supervisePod(ctx, req)

	// only the name can be used here, otherwise badness in the runner. We already know the namespace.
	cijob.Status.PodName = pod.Name().Name
	return r.Update(ctx, cijob)
}
