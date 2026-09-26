package observability

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics owns JustCD's Prometheus collectors. All label values are normalized
// to small fixed vocabularies before they reach a collector.
type Metrics struct {
	enabled  bool
	registry *prometheus.Registry

	httpRequests           *prometheus.CounterVec
	httpRequestDuration    *prometheus.HistogramVec
	reconciliations        *prometheus.CounterVec
	reconciliationDuration *prometheus.HistogramVec
	driftChanges           *prometheus.CounterVec
	operations             *prometheus.CounterVec
	operationDuration      *prometheus.HistogramVec
	operationQueueDepth    *prometheus.GaugeVec
	approvalActions        *prometheus.CounterVec
}

func NewMetrics(enabled bool) *Metrics {
	m := &Metrics{enabled: enabled}
	if !enabled {
		return m
	}
	m.registry = prometheus.NewRegistry()
	m.httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "justcd_http_requests_total", Help: "Completed HTTP requests handled by the JustCD backend.",
	}, []string{"method", "route", "status"})
	m.httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "justcd_http_request_duration_seconds", Help: "HTTP request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})
	m.reconciliations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "justcd_reconciliation_checks_total", Help: "Completed automatic reconciliation plan checks.",
	}, []string{"result"})
	m.reconciliationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "justcd_reconciliation_check_duration_seconds", Help: "Automatic reconciliation plan check duration in seconds.",
		Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
	}, []string{"result"})
	m.driftChanges = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "justcd_drift_changes_total", Help: "Resource changes found by automatic reconciliation checks.",
	}, []string{"kind"})
	m.operations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "justcd_operations_completed_total", Help: "Completed JustCD deployment operations.",
	}, []string{"type", "status"})
	m.operationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "justcd_operation_duration_seconds", Help: "Duration of JustCD deployment operations in seconds.",
		Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800},
	}, []string{"type", "status"})
	m.operationQueueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "justcd_operation_queue_depth", Help: "Current durable operation count by status.",
	}, []string{"status"})
	m.approvalActions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "justcd_plan_approvals_total", Help: "Plan approvals recorded, grouped by resulting approval state.",
	}, []string{"state"})
	m.registry.MustRegister(
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.httpRequests, m.httpRequestDuration, m.reconciliations, m.reconciliationDuration,
		m.driftChanges, m.operations, m.operationDuration, m.operationQueueDepth, m.approvalActions,
	)
	return m
}

func (m *Metrics) Enabled() bool { return m != nil && m.enabled }

func (m *Metrics) Handler() http.Handler {
	if !m.Enabled() || m.registry == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

func (m *Metrics) RecordHTTP(method, route string, status int, elapsed time.Duration) {
	if !m.Enabled() {
		return
	}
	method = boundedMethod(method)
	route = boundedRoute(route)
	statusClass := strconv.Itoa(status/100) + "xx"
	if status < 100 || status > 599 {
		statusClass = "other"
	}
	m.httpRequests.WithLabelValues(method, route, statusClass).Inc()
	m.httpRequestDuration.WithLabelValues(method, route).Observe(elapsed.Seconds())
}

func (m *Metrics) RecordReconciliation(result string, elapsed time.Duration) {
	if !m.Enabled() {
		return
	}
	result = oneOf(result, "in_sync", "drift", "failed", "approval_required", "cancelled")
	m.reconciliations.WithLabelValues(result).Inc()
	m.reconciliationDuration.WithLabelValues(result).Observe(elapsed.Seconds())
}

func (m *Metrics) RecordDriftChanges(changes []core.Change) {
	if !m.Enabled() {
		return
	}
	for _, change := range changes {
		m.driftChanges.WithLabelValues(oneOf(string(change.Kind), "create", "update", "delete")).Inc()
	}
}

func (m *Metrics) RecordOperation(operationType, status string, elapsed time.Duration) {
	if !m.Enabled() {
		return
	}
	operationType = oneOf(operationType, "sync", "rollback", "decommission")
	status = oneOf(status, "succeeded", "failed", "cancelled", "interrupted")
	m.operations.WithLabelValues(operationType, status).Inc()
	m.operationDuration.WithLabelValues(operationType, status).Observe(elapsed.Seconds())
}

func (m *Metrics) SetQueueDepth(queued, running int64) {
	if !m.Enabled() {
		return
	}
	m.operationQueueDepth.WithLabelValues("queued").Set(float64(queued))
	m.operationQueueDepth.WithLabelValues("running").Set(float64(running))
}

func (m *Metrics) RecordApproval(state string) {
	if !m.Enabled() {
		return
	}
	m.approvalActions.WithLabelValues(oneOf(state, "pending", "threshold_met")).Inc()
}

func boundedMethod(method string) string {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return strings.ToUpper(method)
	default:
		return "OTHER"
	}
}

func boundedRoute(route string) string {
	switch route {
	case "/healthz", "/api/v1/health", "/api/v1/openapi.yaml", "/metrics", "/api/v1/auth/*", "/api/v1/webhooks/*", "/api/v1/*", "other":
		return route
	default:
		return "other"
	}
}

func oneOf(value string, allowed ...string) string {
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return "other"
}
