package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Trysac/u401-lsm-kv-engine/internal/metrics"
)

func TestEngineMetrics(t *testing.T) {
	m := metrics.NewEngineMetrics()
	h := m.Handler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
}
