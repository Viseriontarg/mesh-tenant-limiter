package configapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amin/mesh-tenant-limiter/pkg/config"
	"github.com/amin/mesh-tenant-limiter/pkg/configapi"
)

func TestConfigAPIUpdatesTierAndTenant(t *testing.T) {
	store, err := config.NewStore(config.Snapshot{
		DefaultTier: "free",
		Tiers: map[string]config.RateLimit{
			"free": {Requests: 100, Window: time.Minute},
		},
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	handler := configapi.New(store).Routes()

	tierBody := bytes.NewBufferString(`{"requests":5000,"window":"1s"}`)
	tierReq := httptest.NewRequest(http.MethodPut, "/v1/config/tiers/enterprise", tierBody)
	tierReq.Header.Set("Content-Type", "application/json")
	tierRec := httptest.NewRecorder()
	handler.ServeHTTP(tierRec, tierReq)
	if tierRec.Code != http.StatusOK {
		t.Fatalf("expected tier update to succeed, got %d", tierRec.Code)
	}

	tenantBody := bytes.NewBufferString(`{"tier":"enterprise"}`)
	tenantReq := httptest.NewRequest(http.MethodPut, "/v1/config/tenants/acme", tenantBody)
	tenantReq.Header.Set("Content-Type", "application/json")
	tenantRec := httptest.NewRecorder()
	handler.ServeHTTP(tenantRec, tenantReq)
	if tenantRec.Code != http.StatusOK {
		t.Fatalf("expected tenant update to succeed, got %d", tenantRec.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected snapshot lookup to succeed, got %d", getRec.Code)
	}

	var snap config.Snapshot
	if err := json.NewDecoder(bytes.NewReader(getRec.Body.Bytes())).Decode(&snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}

	if snap.Tenants["acme"].Tier != "enterprise" {
		t.Fatalf("expected tenant to be assigned to enterprise tier")
	}
}
