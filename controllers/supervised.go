package controllers

import (
	sourcev1alpha1 "github.com/fluxcd/source-controller/api/v1alpha1"
	"github.com/go-logr/logr"
	objectsv1alpha1 "github.com/tinyci/k8s-api/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

type request struct {
	log logr.Logger
	req ctrl.Request
}

type supervised interface {
	Logger() logr.Logger
	Name() types.NamespacedName
}

type supervisedGit struct {
	request
	git *sourcev1alpha1.GitRepository
}

func (sg *supervisedGit) Name() types.NamespacedName {
	return getName(sg.req, "git")
}

func (sg *supervisedGit) Logger() logr.Logger {
	return sg.log.WithValues("cijob", sg.req.NamespacedName, "gitrepository", sg.Name())
}

type supervisedPod struct {
	request
	pod *corev1.Pod
}

func (sp *supervisedPod) Name() types.NamespacedName {
	return getName(sp.req, "pod")
}

func (sp *supervisedPod) Logger() logr.Logger {
	return sp.log.WithValues("cijob", sp.req.NamespacedName, "pod", sp.Name())
}

type supervisedCIJob struct {
	request

	cijob *objectsv1alpha1.CIJob
}

func (sci *supervisedCIJob) Name() types.NamespacedName {
	return sci.req.NamespacedName
}

func (sci *supervisedCIJob) Logger() logr.Logger {
	return sci.log.WithValues("cijob", sci.req.NamespacedName)
}
