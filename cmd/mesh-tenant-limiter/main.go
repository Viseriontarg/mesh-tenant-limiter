package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amin/mesh-tenant-limiter/pkg/config"
	"github.com/amin/mesh-tenant-limiter/pkg/configapi"
	"github.com/amin/mesh-tenant-limiter/pkg/httpmiddleware"
	"github.com/amin/mesh-tenant-limiter/pkg/limiter"
	"github.com/amin/mesh-tenant-limiter/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	addr := envOrDefault("MTL_ADDR", ":8080")

	store, err := config.NewStore(config.Snapshot{
		DefaultTier: "free",
		Tiers: map[string]config.RateLimit{
			"free":       {Requests: 100, Window: time.Minute},
			"enterprise": {Requests: 5000, Window: time.Second},
		},
		Tenants: map[string]config.TenantEntry{
			"demo-free":       {TenantID: "demo-free", Tier: "free"},
			"demo-enterprise": {TenantID: "demo-enterprise", Tier: "enterprise"},
		},
	})
	if err != nil {
		log.Fatalf("init config: %v", err)
	}

	reg := prometheus.NewRegistry()
	metrics := telemetry.NewMetrics(reg)
	lim, err := limiter.New(limiter.Options{
		Config:        store,
		Metrics:       metrics,
		FlushInterval: 100 * time.Millisecond,
	})
	if err != nil {
		log.Fatalf("init limiter: %v", err)
	}
	defer lim.Close()

	protected, err := httpmiddleware.New(httpmiddleware.Options{Limiter: lim})
	if err != nil {
		log.Fatalf("init middleware: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.Handle("/v1/config", configapi.New(store).Routes())
	mux.Handle("/v1/config/", configapi.New(store).Routes())
	mux.Handle("/demo", protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID, _ := httpmiddleware.TenantIDFromContext(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","tenant_id":"` + tenantID + `"}`))
	})))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("mesh-tenant-limiter listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
