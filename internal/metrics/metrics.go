package metrics

import (
	"net/http"

	"github.com/Trysac/u135-exporter-kit"
)

// EngineMetrics holds counters and gauges for the LSM engine.
type EngineMetrics struct {
	reg *exporterkit.Registry
}

// NewEngineMetrics initializes Prometheus metrics via exporter-kit.
func NewEngineMetrics() *EngineMetrics {
	reg := exporterkit.NewRegistry(exporterkit.Options{})
	return &EngineMetrics{reg: reg}
}

// Handler returns http.Handler for the metrics endpoint.
func (m *EngineMetrics) Handler() http.Handler {
	return m.reg.Handler()
}
