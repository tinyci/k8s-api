package controllers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var errCreated = errors.New("already created")

type (
	supervisedMap map[types.NamespacedName]supervised
	resourceMap   map[types.NamespacedName]supervisedMap
)

type tracker struct {
	sync.Mutex
	resources resourceMap
}

func newTracker() *tracker {
	return &tracker{resources: resourceMap{}}
}

func (t *tracker) add(req ctrl.Request, s supervised) {
tableRetry:
	table, ok := t.resources[req.NamespacedName]
	if !ok {
		t.resources[req.NamespacedName] = supervisedMap{}
		goto tableRetry
	}

	table[s.Name()] = s
}

func (t *tracker) exists(req ctrl.Request, s supervised) bool {
	table, ok := t.resources[req.NamespacedName]
	if !ok {
		return false
	}

	_, ok = table[s.Name()]
	return ok
}

func (t *tracker) remove(req ctrl.Request, s supervised) {
	table, ok := t.resources[req.NamespacedName]
	if !ok {
		return
	}

	delete(table, s.Name())
	if len(table) == 0 { // clean up orphaned table entries
		delete(t.resources, req.NamespacedName)
	}
}

func (t *tracker) Create(ctx context.Context, obj runtime.Object, req ctrl.Request, s supervised) error {
	t.Lock()
	defer t.Unlock()

	if t.exists(req, s) {
		return fmt.Errorf("could not create %q: %w", s.Name(), errCreated)
	}

	if err := s.Create(ctx, obj, true); err != nil {
		return err
	}

	t.add(req, s)
	return nil
}

func (t *tracker) Delete(ctx context.Context, req ctrl.Request, s supervised) error {
	t.Lock()
	defer t.Unlock()

	return t.delete(ctx, req, s)
}

func (t *tracker) delete(ctx context.Context, req ctrl.Request, s supervised) error {
	if !t.exists(req, s) {
		return nil
	}

	if err := s.Delete(ctx); client.IgnoreNotFound(err) != nil {
		return err
	}

	t.remove(req, s)

	return nil
}

func (t *tracker) Reap(ctx context.Context, req ctrl.Request) error {
	t.Lock()
	defer t.Unlock()

	for _, s := range t.resources[req.NamespacedName] {
		if err := t.delete(ctx, req, s); err != nil {
			return err
		}
	}

	delete(t.resources, req.NamespacedName)
	return nil
}
