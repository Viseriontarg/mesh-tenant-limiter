package httpmiddleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/amin/mesh-tenant-limiter/pkg/limiter"
)

// TenantExtractor reads a tenant ID from an HTTP request.
type TenantExtractor func(*http.Request) (string, error)

// Options configure the HTTP middleware behavior.
type Options struct {
	Limiter         *limiter.Limiter
	ExtractTenantID TenantExtractor
}

// New creates an HTTP middleware that enforces tenant rate limits.
func New(opts Options) (func(http.Handler) http.Handler, error) {
	if opts.Limiter == nil {
		return nil, errors.New("limiter is required")
	}
	if opts.ExtractTenantID == nil {
		opts.ExtractTenantID = func(r *http.Request) (string, error) {
			tenantID := r.Header.Get("X-Tenant-ID")
			if tenantID == "" {
				return "", errors.New("missing X-Tenant-ID header")
			}
			return tenantID, nil
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID, err := opts.ExtractTenantID(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			decision, err := opts.Limiter.Check(r.Context(), tenantID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			for key, value := range limiter.FormatHeaders(decision) {
				w.Header().Set(key, value)
			}

			if !decision.Allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"tenant_id": tenantID,
					"error":     "rate limit exceeded",
					"reset_at":  decision.ResetAt,
				})
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tenantIDKey{}, tenantID)))
		})
	}, nil
}

type tenantIDKey struct{}

// TenantIDFromContext returns the tenant ID captured by the middleware.
func TenantIDFromContext(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(tenantIDKey{}).(string)
	return tenantID, ok
}
