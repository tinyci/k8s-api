package controllers

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

func getPodName(req ctrl.Request) types.NamespacedName {
	nsName := req.NamespacedName
	// FIXME this should probably be less stupid
	nsName.Name += "-pod"

	return nsName
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

	_, err := r.getPod(ctx, req)
	if err != nil {
		if err := r.Client.Create(ctx, cijob.Pod(getPodName(req))); err != nil {
			return defaultResult, err
		}

		go r.supervisePod(ctx, req)

		cijob.Status.PodName = getPodName(req).String()
		return defaultResult, r.Update(ctx, cijob)
	}

	return defaultResult, nil
}

// SetupWithManager sets up the manager by installing the controller
func (r *CIJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&objectsv1alpha1.CIJob{}).
		Complete(r)
}
