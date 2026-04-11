package cluster

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// fakeController satisfies the controller.Controller interface for testing.
type fakeController struct{}

func (fakeController) Reconcile(_ context.Context, _ reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func (fakeController) Watch(_ source.Source) error { return nil }

func (fakeController) Start(_ context.Context) error { return nil }

func (fakeController) GetLogger() logr.Logger { return logr.Discard() }

func newTestManager(startFn func(ctx, mgrCtx context.Context, ctrl controller.Controller, ns string) (context.CancelFunc, error)) *NamespaceWatchManager {
	m := NewNamespaceWatchManager(nil, runtime.NewScheme(), "name-label", "ns-label")
	m.SetController(fakeController{})
	m.SetContext(context.Background())
	m.startAndRegisterFn = startFn
	return m
}

func TestEnsureWatch_ControllerNotSet(t *testing.T) {
	t.Parallel()
	m := NewNamespaceWatchManager(nil, runtime.NewScheme(), "n", "ns")
	m.SetContext(context.Background())

	if err := m.EnsureWatch(context.Background(), "default"); err == nil {
		t.Fatal("expected error when controller not set")
	}
}

func TestEnsureWatch_ContextNotSet(t *testing.T) {
	t.Parallel()
	m := NewNamespaceWatchManager(nil, runtime.NewScheme(), "n", "ns")
	m.SetController(fakeController{})

	if err := m.EnsureWatch(context.Background(), "default"); err == nil {
		t.Fatal("expected error when context not set")
	}
}

func TestEnsureWatch_Idempotent(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	m := newTestManager(func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		calls.Add(1)
		return func() {}, nil
	})

	for i := 0; i < 5; i++ {
		if err := m.EnsureWatch(context.Background(), "ns-a"); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected startAndRegister called once, got %d", got)
	}
}

func TestEnsureWatch_DifferentNamespaces(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	m := newTestManager(func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		calls.Add(1)
		return func() {}, nil
	})

	for _, ns := range []string{"ns-1", "ns-2", "ns-3"} {
		if err := m.EnsureWatch(context.Background(), ns); err != nil {
			t.Fatalf("namespace %s: unexpected error: %v", ns, err)
		}
	}

	if got := calls.Load(); got != 3 {
		t.Fatalf("expected 3 startAndRegister calls, got %d", got)
	}
}

func TestEnsureWatch_ErrorAllowsRetry(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	m := newTestManager(func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		calls.Add(1)
		return nil, fmt.Errorf("simulated RBAC failure")
	})

	// First call fails.
	if err := m.EnsureWatch(context.Background(), "ns-fail"); err == nil {
		t.Fatal("expected error on first call")
	}

	// Namespace must NOT be recorded, so a retry should invoke startAndRegister again.
	m.startAndRegisterFn = func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		calls.Add(1)
		return func() {}, nil
	}

	if err := m.EnsureWatch(context.Background(), "ns-fail"); err != nil {
		t.Fatalf("retry should succeed: %v", err)
	}

	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 calls (fail + retry), got %d", got)
	}
}

func TestEnsureWatch_ConcurrentSameNamespace(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	gate := make(chan struct{}) // keeps startFn blocked until all goroutines are waiting
	m := newTestManager(func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		calls.Add(1)
		<-gate // block so concurrent callers pile up on singleflight
		return func() {}, nil
	})

	const goroutines = 10
	var ready sync.WaitGroup
	ready.Add(goroutines)
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ready.Done()
			ready.Wait() // all goroutines start together
			errs[idx] = m.EnsureWatch(context.Background(), "contested-ns")
		}(i)
	}

	ready.Wait() // all goroutines are at the gate
	close(gate)  // let the single startFn proceed
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 startAndRegister call via singleflight, got %d", got)
	}
}

func TestEnsureWatch_ConcurrentSameNamespaceError(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	// Use time.Sleep so the singleflight callback stays active long enough
	// for all goroutines to queue up. Unlike the success case there is no
	// fast-path for late arrivals, so we need real overlap.
	m := newTestManager(func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		return nil, fmt.Errorf("cache sync failed")
	})

	const goroutines = 10
	var ready sync.WaitGroup
	ready.Add(goroutines)
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ready.Done()
			ready.Wait() // all goroutines start together
			errs[idx] = m.EnsureWatch(context.Background(), "fail-ns")
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err == nil {
			t.Errorf("goroutine %d: expected error, got nil", i)
		}
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 startAndRegister call via singleflight, got %d", got)
	}
}
