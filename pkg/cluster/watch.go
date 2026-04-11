// Package cluster builds a controller-runtime cluster client for interacting with objects.
package cluster

import (
	"context"
	"fmt"
	"sync"

	rufiov1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"golang.org/x/sync/singleflight"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// NamespaceWatchManager dynamically creates per-namespace informer caches and
// registers watch sources on a controller. This is used when the external
// kubeconfig does not have cluster-wide list/watch access for Workflows and Jobs.
type NamespaceWatchManager struct {
	mu         sync.Mutex
	restConfig *rest.Config
	scheme     *runtime.Scheme
	ctrl       controller.Controller
	mgrCtx     context.Context
	namespaces map[string]context.CancelFunc
	sf         singleflight.Group

	// labelMachineName and labelMachineNamespace are the labels used to map
	// external resources back to TinkerbellMachine objects in the management cluster.
	labelMachineName      string
	labelMachineNamespace string

	// startAndRegisterFn overrides startAndRegister for testing. If nil, the
	// real implementation is used.
	startAndRegisterFn func(ctx, mgrCtx context.Context, ctrl controller.Controller, namespace string) (context.CancelFunc, error)
}

// NewNamespaceWatchManager creates a new NamespaceWatchManager.
// The controller and mgrCtx must be set before calling EnsureWatch (they are
// set after SetupWithManager returns the controller reference and the manager's
// context is available).
func NewNamespaceWatchManager(restConfig *rest.Config, scheme *runtime.Scheme, labelName, labelNamespace string) *NamespaceWatchManager {
	return &NamespaceWatchManager{
		restConfig:            restConfig,
		scheme:                scheme,
		namespaces:            make(map[string]context.CancelFunc),
		labelMachineName:      labelName,
		labelMachineNamespace: labelNamespace,
	}
}

// SetController sets the controller reference used for registering watches.
// Must be called before EnsureWatch.
func (m *NamespaceWatchManager) SetController(ctrl controller.Controller) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ctrl = ctrl
}

// SetContext sets the parent context used for starting namespace caches.
// Must be called before EnsureWatch.
func (m *NamespaceWatchManager) SetContext(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.mgrCtx = ctx
}

// EnsureWatch ensures that a namespace-scoped informer cache is running for the
// given namespace and that Workflow and Job watches are registered on the
// controller. Calls for already-watched namespaces are no-ops.
// Concurrent callers for the same namespace block until the first caller
// completes; all receive the same result.
func (m *NamespaceWatchManager) EnsureWatch(ctx context.Context, namespace string) error {
	// Fast path: namespace already fully established.
	m.mu.Lock()
	if _, ok := m.namespaces[namespace]; ok {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	// singleflight.Do deduplicates concurrent calls for the same namespace key.
	// All callers block until the first one finishes and all receive the same
	// error, avoiding the sentinel-nil race where a concurrent caller could
	// return success before the watch is actually established.
	_, err, _ := m.sf.Do(namespace, func() (any, error) {
		m.mu.Lock()
		// Re-check under singleflight in case a previous call completed
		// between the fast-path check and entering Do.
		if _, ok := m.namespaces[namespace]; ok {
			m.mu.Unlock()
			return nil, nil
		}

		if m.ctrl == nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("NamespaceWatchManager: controller not set")
		}

		if m.mgrCtx == nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("NamespaceWatchManager: context not set")
		}

		mgrCtx := m.mgrCtx
		ctrl := m.ctrl
		startFn := m.startAndRegister
		if m.startAndRegisterFn != nil {
			startFn = m.startAndRegisterFn
		}
		m.mu.Unlock()

		cacheCancel, err := startFn(ctx, mgrCtx, ctrl, namespace)
		if err != nil {
			return nil, err
		}

		m.mu.Lock()
		m.namespaces[namespace] = cacheCancel
		m.mu.Unlock()

		log.FromContext(ctx).Info("Started JIT namespace watch", "namespace", namespace)

		return nil, nil
	})

	if err != nil {
		return fmt.Errorf("failed to create namespace watch: error: %w, namespace: %s", err, namespace)
	}

	return nil
}

// startAndRegister creates a namespace-scoped cache, starts it, waits for sync,
// and registers Workflow/Job watches on the controller. It returns a CancelFunc
// to stop the cache. All operations are safe to call without holding m.mu.
func (m *NamespaceWatchManager) startAndRegister(
	ctx context.Context,
	mgrCtx context.Context,
	ctrl controller.Controller,
	namespace string,
) (context.CancelFunc, error) {
	nsCache, err := cache.New(m.restConfig, cache.Options{
		Scheme: m.scheme,
		ByObject: map[client.Object]cache.ByObject{
			&tinkv1.Workflow{}: {},
			&rufiov1.Job{}:     {},
		},
		DefaultNamespaces: map[string]cache.Config{
			namespace: {},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating namespace cache for %q: %w", namespace, err)
	}

	// Start the cache with a cancelable context so it can be stopped if sync
	// fails, preventing goroutine/cache leaks.
	cacheCtx, cacheCancel := context.WithCancel(mgrCtx)
	go func() {
		if err := nsCache.Start(cacheCtx); err != nil {
			log.FromContext(cacheCtx).Error(err, "namespace cache stopped", "namespace", namespace)
		}
	}()

	// Wait for the cache to sync before registering watches.
	if !nsCache.WaitForCacheSync(ctx) {
		cacheCancel()
		return nil, fmt.Errorf("namespace cache for %q failed to sync", namespace)
	}

	// Register watches on the controller for this namespace's cache.
	if err := ctrl.Watch(source.Kind(nsCache, &tinkv1.Workflow{},
		handler.TypedEnqueueRequestsFromMapFunc(labelMapper[*tinkv1.Workflow](m.labelMachineName, m.labelMachineNamespace)))); err != nil {
		cacheCancel()
		return nil, fmt.Errorf("watching Workflows in namespace %q: %w", namespace, err)
	}

	if err := ctrl.Watch(source.Kind(nsCache, &rufiov1.Job{},
		handler.TypedEnqueueRequestsFromMapFunc(labelMapper[*rufiov1.Job](m.labelMachineName, m.labelMachineNamespace)))); err != nil {
		cacheCancel()
		return nil, fmt.Errorf("watching Jobs in namespace %q: %w", namespace, err)
	}

	return cacheCancel, nil
}

// labelMapper returns a typed mapper function that maps external Tinkerbell
// resources back to TinkerbellMachine objects using label-based ownership.
func labelMapper[T client.Object](labelName, labelNamespace string) handler.TypedMapFunc[T, reconcile.Request] {
	return func(_ context.Context, o T) []reconcile.Request {
		labels := o.GetLabels()
		name := labels[labelName]
		namespace := labels[labelNamespace]

		if name == "" || namespace == "" {
			return nil
		}

		return []reconcile.Request{
			{NamespacedName: types.NamespacedName{Name: name, Namespace: namespace}},
		}
	}
}
