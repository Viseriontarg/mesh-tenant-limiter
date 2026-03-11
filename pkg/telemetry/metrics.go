package telemetry

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics provides Prometheus counters for the limiter critical path and async Redis sync.
type Metrics struct {
	decisions     *prometheus.CounterVec
	syncLatency   *prometheus.HistogramVec
	syncDropped   prometheus.Counter
	circuitOpen   prometheus.Gauge
	backendErrors prometheus.Counter
}

// NewMetrics creates and registers the Prometheus metrics used by the sidecar.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	m := &Metrics{
		decisions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mesh_tenant_limiter_decisions_total",
				Help: "Count of limiter decisions partitioned by outcome and mode.",
			},
			[]string{"tenant", "outcome", "mode"},
		),
		syncLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mesh_tenant_limiter_sync_latency_seconds",
				Help:    "Latency of asynchronous backend synchronization calls.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"status"},
		),
		syncDropped: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "mesh_tenant_limiter_sync_dropped_total",
				Help: "Count of sync events dropped because the queue was full.",
			},
		),
		circuitOpen: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "mesh_tenant_limiter_backend_circuit_open",
				Help: "Whether the Redis fail-open circuit breaker is currently open.",
			},
		),
		backendErrors: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "mesh_tenant_limiter_backend_errors_total",
				Help: "Count of asynchronous backend synchronization errors.",
			},
		),
	}

	reg.MustRegister(m.decisions, m.syncLatency, m.syncDropped, m.circuitOpen, m.backendErrors)
	return m
}

// RecordDecision tracks each request outcome.
func (m *Metrics) RecordDecision(tenant string, allowed, failOpen bool) {
	outcome := "deny"
	if allowed {
		outcome = "allow"
	}
	mode := "enforced"
	if failOpen {
		mode = "fail_open"
	}
	m.decisions.WithLabelValues(tenant, outcome, mode).Inc()
}

// ObserveSyncLatency tracks backend call duration and completion status.
func (m *Metrics) ObserveSyncLatency(status string, duration time.Duration) {
	m.syncLatency.WithLabelValues(status).Observe(duration.Seconds())
}

// RecordSyncDropped tracks backpressure in the async queue.
func (m *Metrics) RecordSyncDropped() {
	m.syncDropped.Inc()
}

// RecordBackendError marks a backend synchronization failure.
func (m *Metrics) RecordBackendError() {
	m.backendErrors.Inc()
}

// SetCircuitOpen updates the Prometheus gauge representing breaker state.
func (m *Metrics) SetCircuitOpen(open bool) {
	if open {
		m.circuitOpen.Set(1)
		return
	}
	m.circuitOpen.Set(0)
}
