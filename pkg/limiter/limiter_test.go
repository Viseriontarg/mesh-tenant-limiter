package limiter_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin/mesh-tenant-limiter/pkg/config"
	"github.com/amin/mesh-tenant-limiter/pkg/limiter"
)

func TestLimiterEnforcesSlidingWindow(t *testing.T) {
	store := newTestStore(t, config.Snapshot{
		DefaultTier: "free",
		Tiers: map[string]config.RateLimit{
			"free": {Requests: 2, Window: 100 * time.Millisecond},
		},
	})

	l, err := limiter.New(limiter.Options{Config: store})
	if err != nil {
		t.Fatalf("new limiter: %v", err)
	}
	defer l.Close()

	first, _ := l.Check(context.Background(), "tenant-a")
	second, _ := l.Check(context.Background(), "tenant-a")
	third, _ := l.Check(context.Background(), "tenant-a")

	if !first.Allowed || !second.Allowed {
		t.Fatalf("expected first two requests to be allowed")
	}
	if third.Allowed {
		t.Fatalf("expected third request to be blocked")
	}

	time.Sleep(110 * time.Millisecond)
	fourth, _ := l.Check(context.Background(), "tenant-a")
	if !fourth.Allowed {
		t.Fatalf("expected request after window reset to be allowed")
	}
}

func TestLimiterHandlesConcurrentAccess(t *testing.T) {
	store := newTestStore(t, config.Snapshot{
		DefaultTier: "shared",
		Tiers: map[string]config.RateLimit{
			"shared": {Requests: 10, Window: time.Second},
		},
	})

	l, err := limiter.New(limiter.Options{Config: store})
	if err != nil {
		t.Fatalf("new limiter: %v", err)
	}
	defer l.Close()

	var allowed int32
	start := make(chan struct{})
	var wg sync.WaitGroup

	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			decision, err := l.Check(context.Background(), "tenant-b")
			if err != nil {
				t.Errorf("check failed: %v", err)
				return
			}
			if decision.Allowed {
				atomic.AddInt32(&allowed, 1)
			}
		}()
	}

	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&allowed); got != 10 {
		t.Fatalf("expected 10 allowed requests, got %d", got)
	}
}

func TestLimiterFailsOpenWhenBackendTripsCircuit(t *testing.T) {
	store := newTestStore(t, config.Snapshot{
		DefaultTier: "free",
		Tiers: map[string]config.RateLimit{
			"free": {Requests: 1, Window: time.Minute},
		},
	})

	backend := &failingBackend{called: make(chan struct{}, 1)}
	l, err := limiter.New(limiter.Options{
		Config:               store,
		Backend:              backend,
		FlushInterval:        10 * time.Millisecond,
		CircuitBreakerWindow: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new limiter: %v", err)
	}
	defer l.Close()

	first, err := l.Check(context.Background(), "tenant-c")
	if err != nil {
		t.Fatalf("first check: %v", err)
	}
	if !first.Allowed {
		t.Fatalf("expected first request to be allowed")
	}

	select {
	case <-backend.called:
	case <-time.After(time.Second):
		t.Fatalf("backend was never called")
	}

	second, err := l.Check(context.Background(), "tenant-c")
	if err != nil {
		t.Fatalf("second check: %v", err)
	}
	if !second.Allowed || !second.FailOpen {
		t.Fatalf("expected second request to be allowed in fail-open mode")
	}
}

type stubBackend struct {
	calls atomic.Int32
}

func (b *stubBackend) Sync(context.Context, string, config.RateLimit, int) error {
	b.calls.Add(1)
	return nil
}

type failingBackend struct {
	called chan struct{}
}

func (b *failingBackend) Sync(context.Context, string, config.RateLimit, int) error {
	select {
	case b.called <- struct{}{}:
	default:
	}
	return context.DeadlineExceeded
}

func newTestStore(t *testing.T, snap config.Snapshot) *config.Store {
	t.Helper()

	store, err := config.NewStore(snap)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}
