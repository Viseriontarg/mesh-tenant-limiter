package configapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/amin/mesh-tenant-limiter/pkg/config"
)

// Handler exposes the hot-reloadable configuration endpoints for operators.
type Handler struct {
	store *config.Store
}

// New creates a config API handler.
func New(store *config.Store) *Handler {
	return &Handler{store: store}
}

// Routes returns an HTTP handler with the configuration endpoints mounted.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/config", h.handleSnapshot)
	mux.HandleFunc("/v1/config/tiers/", h.handleTier)
	mux.HandleFunc("/v1/config/tenants/", h.handleTenant)
	mux.HandleFunc("/v1/config/default", h.handleDefaultTier)
	return mux
}

type upsertTierRequest struct {
	Requests int    `json:"requests"`
	Window   string `json:"window"`
}

type upsertTenantRequest struct {
	Tier string `json:"tier"`
}

type defaultTierRequest struct {
	Tier string `json:"tier"`
}

func (h *Handler) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, h.store.Snapshot())
}

func (h *Handler) handleTier(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tier := strings.TrimPrefix(r.URL.Path, "/v1/config/tiers/")
	if tier == "" {
		http.Error(w, "tier is required", http.StatusBadRequest)
		return
	}

	var req upsertTierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	window, err := time.ParseDuration(req.Window)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.store.UpsertTier(tier, config.RateLimit{Requests: req.Requests, Window: window}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"tier": tier})
}

func (h *Handler) handleTenant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tenantID := strings.TrimPrefix(r.URL.Path, "/v1/config/tenants/")
	if tenantID == "" {
		http.Error(w, "tenant id is required", http.StatusBadRequest)
		return
	}

	var req upsertTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.store.UpsertTenant(tenantID, req.Tier); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"tenant_id": tenantID, "tier": req.Tier})
}

func (h *Handler) handleDefaultTier(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req defaultTierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.store.SetDefaultTier(req.Tier); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"default_tier": req.Tier})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
