package controllers

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func getName(req ctrl.Request, append string) types.NamespacedName {
	nsName := req.NamespacedName
	// FIXME this should probably be less stupid
	nsName.Name += "-" + append

	return nsName
}

func getState(pod *corev1.Pod) *corev1.ContainerStateTerminated {
	return pod.Status.ContainerStatuses[0].State.Terminated
}
