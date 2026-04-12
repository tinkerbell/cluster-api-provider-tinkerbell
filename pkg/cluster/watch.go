// Package cluster builds a controller-runtime cluster client for interacting with objects.
package cluster

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

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

const (
	// backoffBase is the initial backoff interval for failed EnsureWatch calls.
	backoffBase = 5 * time.Second
	// backoffMax is the maximum backoff interval.
	backoffMax = 5 * time.Minute
)

// failureRecord tracks consecutive failures for a namespace.
type failureRecord struct {
	count       int
	lastAttempt time.Time
}

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
	failures   map[string]failureRecord
	sf         singleflight.Group

	// labelMachineName and labelMachineNamespace are the labels used to map
	// external resources back to TinkerbellMachine objects in the management cluster.
	labelMachineName      string
	labelMachineNamespace string

	// startAndRegisterFn overrides startAndRegister for testing. If nil, the
	// real implementation is used.
	startAndRegisterFn func(ctx, mgrCtx context.Context, ctrl controller.Controller, namespace string) (context.CancelFunc, error)

	// nowFn returns the current time. Override in tests for determinism.
	nowFn func() time.Time
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
		failures:              make(map[string]failureRecord),
		labelMachineName:      labelName,
		labelMachineNamespace: labelNamespace,
		nowFn:                 time.Now,
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
// If a previous attempt for the namespace failed recently, the call is rejected
// with a backoff error to avoid flooding the external API server.
func (m *NamespaceWatchManager) EnsureWatch(ctx context.Context, namespace string) error { //nolint:cyclop
	if namespace == "" {
		return fmt.Errorf("NamespaceWatchManager: namespace must not be empty")
	}

	// Fast path: namespace already fully established.
	m.mu.Lock()
	if _, ok := m.namespaces[namespace]; ok {
		m.mu.Unlock()
		return nil
	}

	// Backoff: if the namespace has a recent failure, reject the call early.
	if fr, ok := m.failures[namespace]; ok {
		wait := backoffDuration(fr.count)
		now := m.nowFn()
		if elapsed := now.Sub(fr.lastAttempt); elapsed < wait {
			m.mu.Unlock()
			return fmt.Errorf("namespace %q watch in backoff (%s remaining)", namespace, wait-elapsed)
		}
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
			m.mu.Lock()
			prev := m.failures[namespace]
			m.failures[namespace] = failureRecord{count: prev.count + 1, lastAttempt: m.nowFn()}
			m.mu.Unlock()

			return nil, err
		}

		m.mu.Lock()
		m.namespaces[namespace] = cacheCancel
		delete(m.failures, namespace)
		m.mu.Unlock()

		log.FromContext(ctx).Info("Started JIT namespace watch", "namespace", namespace)

		return nil, nil
	})

	if err != nil {
		return fmt.Errorf("failed to create namespace watch: error: %w, namespace: %s", err, namespace)
	}

	return nil
}

// backoffDuration computes an exponential backoff capped at backoffMax.
func backoffDuration(failureCount int) time.Duration {
	// 2^6 * backoffBase already exceeds backoffMax, so short-circuit for any
	// count >= 6 to avoid float64 → int64 overflow at very high counts.
	if failureCount >= 6 { //nolint:mnd
		return backoffMax
	}

	d := time.Duration(math.Pow(2, float64(failureCount))) * backoffBase //nolint:mnd
	if d > backoffMax {
		return backoffMax
	}

	return d
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
		// If the cache stopped unexpectedly (not via StopWatch/StopAll),
		// remove the namespace so the next EnsureWatch call re-creates it.
		if cacheCtx.Err() == nil {
			m.mu.Lock()
			delete(m.namespaces, namespace)
			m.mu.Unlock()
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

// StopWatch stops the namespace-scoped informer cache for the given namespace
// and removes it from tracking. Subsequent calls to EnsureWatch for the same
// namespace will create a new cache. No-op if the namespace is not watched.
func (m *NamespaceWatchManager) StopWatch(namespace string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cancel, ok := m.namespaces[namespace]; ok {
		cancel()
		delete(m.namespaces, namespace)
	}

	delete(m.failures, namespace)
}

// StopAll stops all namespace-scoped informer caches and clears all tracking
// state. Intended for graceful shutdown.
func (m *NamespaceWatchManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for ns, cancel := range m.namespaces {
		cancel()
		delete(m.namespaces, ns)
	}

	clear(m.failures)
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
