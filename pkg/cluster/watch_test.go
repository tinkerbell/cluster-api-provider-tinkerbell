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

	// Advance past the backoff window so the retry is allowed.
	m.nowFn = func() time.Time { return time.Now().Add(10 * time.Minute) }

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

func TestEnsureWatch_BackoffAfterFailure(t *testing.T) {
	t.Parallel()
	now := time.Now()
	m := newTestManager(func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		return nil, fmt.Errorf("RBAC denied")
	})
	m.nowFn = func() time.Time { return now }

	// First call fails and records the failure.
	if err := m.EnsureWatch(context.Background(), "backoff-ns"); err == nil {
		t.Fatal("expected error on first call")
	}

	// Immediate retry should be rejected by backoff (window is 2^1 * 5s = 10s).
	m.nowFn = func() time.Time { return now.Add(3 * time.Second) }
	if err := m.EnsureWatch(context.Background(), "backoff-ns"); err == nil {
		t.Fatal("expected backoff error on immediate retry")
	}

	// After the backoff window, the call should be attempted again.
	m.nowFn = func() time.Time { return now.Add(11 * time.Second) }
	m.startAndRegisterFn = func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		return func() {}, nil
	}
	if err := m.EnsureWatch(context.Background(), "backoff-ns"); err != nil {
		t.Fatalf("expected success after backoff window: %v", err)
	}
}

func TestEnsureWatch_BackoffResetsOnSuccess(t *testing.T) {
	t.Parallel()
	now := time.Now()
	failCount := 0
	m := newTestManager(func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		failCount++
		return nil, fmt.Errorf("fail %d", failCount)
	})
	m.nowFn = func() time.Time { return now }

	// Fail twice to accumulate backoff.
	_ = m.EnsureWatch(context.Background(), "reset-ns")
	m.nowFn = func() time.Time { return now.Add(11 * time.Second) } // past first backoff (2^1*5s=10s)
	_ = m.EnsureWatch(context.Background(), "reset-ns")

	// Now succeed. Second failure was recorded at now+11s with count=2, so
	// backoff is 2^2*5s=20s. Advance to now+32s to be past the window.
	m.nowFn = func() time.Time { return now.Add(32 * time.Second) }
	m.startAndRegisterFn = func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		return func() {}, nil
	}
	if err := m.EnsureWatch(context.Background(), "reset-ns"); err != nil {
		t.Fatalf("expected success: %v", err)
	}

	// Verify the failure record was cleared by checking that the namespace
	// is now tracked (idempotent call succeeds without hitting startFn).
	m.startAndRegisterFn = func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		t.Fatal("should not call startFn again for an established namespace")
		return nil, nil
	}
	if err := m.EnsureWatch(context.Background(), "reset-ns"); err != nil {
		t.Fatalf("idempotent call should succeed: %v", err)
	}
}

func TestStopWatch_StopsAndAllowsRewatch(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	m := newTestManager(func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		calls.Add(1)
		return func() {}, nil
	})

	// Establish the watch.
	if err := m.EnsureWatch(context.Background(), "stop-ns"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}

	// Stop the watch.
	m.StopWatch("stop-ns")

	// Re-establish the watch — should call startFn again.
	if err := m.EnsureWatch(context.Background(), "stop-ns"); err != nil {
		t.Fatalf("unexpected error on rewatch: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 calls after StopWatch + re-EnsureWatch, got %d", got)
	}
}

func TestStopWatch_NoOp(t *testing.T) {
	t.Parallel()
	m := newTestManager(nil)

	// Stopping a namespace that was never watched should not panic.
	m.StopWatch("nonexistent-ns")
}

func TestStopAll(t *testing.T) {
	t.Parallel()
	canceled := make(map[string]bool)
	m := newTestManager(func(_, _ context.Context, _ controller.Controller, ns string) (context.CancelFunc, error) {
		return func() { canceled[ns] = true }, nil
	})

	for _, ns := range []string{"ns-a", "ns-b", "ns-c"} {
		if err := m.EnsureWatch(context.Background(), ns); err != nil {
			t.Fatalf("unexpected error for %s: %v", ns, err)
		}
	}

	m.StopAll()

	for _, ns := range []string{"ns-a", "ns-b", "ns-c"} {
		if !canceled[ns] {
			t.Errorf("expected cancel for %s", ns)
		}
	}

	// After StopAll, EnsureWatch should create new watches.
	var calls atomic.Int32
	m.startAndRegisterFn = func(_, _ context.Context, _ controller.Controller, _ string) (context.CancelFunc, error) {
		calls.Add(1)
		return func() {}, nil
	}
	if err := m.EnsureWatch(context.Background(), "ns-a"); err != nil {
		t.Fatalf("unexpected error after StopAll: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 call after StopAll, got %d", got)
	}
}

func TestBackoffDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		count int
		want  time.Duration
	}{
		{0, 5 * time.Second},   // 2^0 * 5s
		{1, 10 * time.Second},  // 2^1 * 5s
		{2, 20 * time.Second},  // 2^2 * 5s
		{3, 40 * time.Second},  // 2^3 * 5s
		{4, 80 * time.Second},  // 2^4 * 5s
		{5, 160 * time.Second}, // 2^5 * 5s
		{6, 5 * time.Minute},   // 2^6 * 5s = 320s > 300s → capped
		{10, 5 * time.Minute},  // capped
	}

	for _, tc := range tests {
		got := backoffDuration(tc.count)
		if got != tc.want {
			t.Errorf("backoffDuration(%d) = %v, want %v", tc.count, got, tc.want)
		}
	}
}
