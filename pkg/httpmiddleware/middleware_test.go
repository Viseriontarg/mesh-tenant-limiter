package httpmiddleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amin/mesh-tenant-limiter/pkg/config"
	"github.com/amin/mesh-tenant-limiter/pkg/httpmiddleware"
	"github.com/amin/mesh-tenant-limiter/pkg/limiter"
)

func TestMiddlewareInjectsRateLimitHeaders(t *testing.T) {
	store := newTestStore(t)
	lim, err := limiter.New(limiter.Options{Config: store})
	if err != nil {
		t.Fatalf("new limiter: %v", err)
	}
	defer lim.Close()

	mw, err := httpmiddleware.New(httpmiddleware.Options{Limiter: lim})
	if err != nil {
		t.Fatalf("new middleware: %v", err)
	}

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodGet, "/demo", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, rec.Code)
	}
	if rec.Header().Get("X-RateLimit-Limit") != "2" {
		t.Fatalf("missing X-RateLimit-Limit header")
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "1" {
		t.Fatalf("expected remaining header to be 1")
	}
	if rec.Header().Get("X-RateLimit-Reset") == "" {
		t.Fatalf("missing X-RateLimit-Reset header")
	}
}

func TestMiddlewareBlocksWhenLimitExceeded(t *testing.T) {
	store := newTestStore(t)
	lim, err := limiter.New(limiter.Options{Config: store})
	if err != nil {
		t.Fatalf("new limiter: %v", err)
	}
	defer lim.Close()

	mw, err := httpmiddleware.New(httpmiddleware.Options{Limiter: lim})
	if err != nil {
		t.Fatalf("new middleware: %v", err)
	}

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/demo", nil)
		req.Header.Set("X-Tenant-ID", "tenant-a")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/demo", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, rec.Code)
	}
}

func newTestStore(t *testing.T) *config.Store {
	t.Helper()

	store, err := config.NewStore(config.Snapshot{
		DefaultTier: "free",
		Tiers: map[string]config.RateLimit{
			"free": {Requests: 2, Window: time.Minute},
		},
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}
