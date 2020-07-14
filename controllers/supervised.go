package controllers

import (
	"context"

	sourcev1alpha1 "github.com/fluxcd/source-controller/api/v1alpha1"
	"github.com/go-logr/logr"
	objectsv1alpha1 "github.com/tinyci/k8s-api/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type request struct {
	log    logr.Logger
	req    ctrl.Request
	client client.Client
}

type supervised interface {
	Logger() logr.Logger
	Name() types.NamespacedName
	Get(context.Context, bool) (runtime.Object, error)
	Create(context.Context, runtime.Object, bool) error
	Delete(context.Context) error
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

func (sg *supervisedGit) Get(ctx context.Context, cache bool) (runtime.Object, error) {
	if cache && sg.git != nil {
		return sg.git, nil
	}

	return sg.git, sg.client.Get(ctx, sg.Name(), sg.git)
}

func (sg *supervisedGit) Delete(ctx context.Context) error {
	if sg.git != nil {
		return sg.client.Delete(ctx, sg.git)
	}

	return nil
}

func (sg *supervisedGit) Create(ctx context.Context, obj runtime.Object, cache bool) error {
	gr := obj.(*sourcev1alpha1.GitRepository)
	nsName := sg.Name()
	gr.Name = nsName.Name
	gr.Namespace = nsName.Namespace
	cp := gr.DeepCopy()
	if err := sg.client.Create(ctx, cp); err != nil {
		return err
	}

	if cache {
		sg.git = cp
	}

	return nil
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

func (sp *supervisedPod) Get(ctx context.Context, cache bool) (runtime.Object, error) {
	if cache && sp.pod != nil {
		return sp.pod, nil
	}

	sp.Logger().Info("retrieving pod information for CI job", "pod", sp.Name())
	return sp.pod, sp.client.Get(ctx, sp.Name(), sp.pod)
}

func (sp *supervisedPod) Delete(ctx context.Context) error {
	if sp.pod != nil {
		return sp.client.Delete(ctx, sp.pod)
	}

	return nil
}

func (sp *supervisedPod) Create(ctx context.Context, obj runtime.Object, cache bool) error {
	pod := obj.(*corev1.Pod)
	nsName := sp.Name()
	pod.Name = nsName.Name
	pod.Namespace = nsName.Namespace
	cp := pod.DeepCopy()
	if err := sp.client.Create(ctx, cp); err != nil {
		return err
	}

	if cache {
		sp.pod = cp
	}

	return nil
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

func (sci *supervisedCIJob) Get(ctx context.Context, cache bool) (runtime.Object, error) {
	if cache && sci.cijob != nil {
		return sci.cijob, nil
	}
	return sci.cijob, sci.client.Get(ctx, sci.Name(), sci.cijob)
}

func (sci *supervisedCIJob) Delete(ctx context.Context) error {
	if sci.cijob != nil {
		return sci.client.Delete(ctx, sci.cijob)
	}

	return nil
}

func (sci *supervisedCIJob) Create(ctx context.Context, obj runtime.Object, cache bool) error {
	cijob := obj.(*objectsv1alpha1.CIJob)
	nsName := sci.Name()
	cijob.Name = nsName.Name
	cijob.Namespace = nsName.Namespace

	cp := cijob.DeepCopy()
	if err := sci.client.Create(ctx, cp); err != nil {
		return err
	}

	if cache {
		sci.cijob = cp
	}

	return nil
}
