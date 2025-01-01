package webhook

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

const (
	// metricsNamespace defines the base namespace for all webhook metrics
	// This ensures unique and consistent metric naming across the application
	metricsNamespace = "pod_label_webhook"

	// Label operation results
	labelOperationSuccess = "success"
	labelOperationSkipped = "skipped"
	labelOperationError   = "error"

	// Annotation states
	annotationValid   = "valid"
	annotationInvalid = "invalid"
	annotationMissing = "missing"
)

var (
	// webhookDurationBuckets are custom histogram buckets optimized for webhook latencies
	// These buckets are carefully chosen to capture meaningful performance characteristics:
	// - 5ms, 10ms: Captures very fast responses
	// - 25ms, 50ms, 100ms: Covers typical low-latency operations
	// - 250ms, 500ms, 1s: Captures slightly slower operations
	// - 2.5s, 5s: Allows tracking of long-running or potentially problematic requests
	webhookDurationBuckets = []float64{0.005, 0.010, 0.025, 0.050, 0.100, 0.250, 0.500, 1.000, 2.500, 5.000}
)

// metrics holds Prometheus metrics for the webhook
// Each field represents a different type of metric to track various aspects of webhook performance
type metrics struct {
	// requestCounter tracks the total number of requests processed
	// Labels allow for granular tracking by path, method, and status
	requestCounter *prometheus.CounterVec

	// requestDuration measures the time taken to process requests
	// Uses a histogram to track request processing time distribution
	requestDuration *prometheus.HistogramVec

	// errorCounter tracks the total number of errors encountered
	// Provides visibility into error rates and types
	errorCounter *prometheus.CounterVec

	// readinessGauge indicates the current readiness status of the webhook
	// 1 means ready, 0 means not ready
	readinessGauge prometheus.Gauge

	// livenessGauge indicates the current liveness status of the webhook
	// 1 means alive, 0 means not alive
	livenessGauge prometheus.Gauge

	// labelOperationsTotal tracks the number of label operations
	// Labels: operation (success/skipped/error), namespace
	labelOperationsTotal *prometheus.CounterVec

	// annotationValidationTotal tracks annotation validation results
	// Labels: result (valid/invalid/missing), namespace
	annotationValidationTotal *prometheus.CounterVec

	// registry is the Prometheus registry used to manage these metrics
	registry *prometheus.Registry
}

// initMetrics initializes and registers Prometheus metrics for the webhook
//
// This method:
// - Creates various metrics with appropriate namespaces and labels
// - Registers metrics with a provided or default registry
// - Handles potential registration errors
//
// Parameters:
//   - reg: An optional Prometheus Registerer (uses default if nil)
//
// Returns:
//   - Configured metrics instance
//   - Error if metric registration fails
func initMetrics(reg prometheus.Registerer) (*metrics, error) {
	// Use default registerer if none provided
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	m := &metrics{}

	// Initialize request counter metric
	m.requestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "requests_total",
			Help:      "Total number of requests processed by the webhook",
		},
		// Labels allow for detailed request tracking
		[]string{"path", "method", "status"},
	)
	if err := reg.Register(m.requestCounter); err != nil {
		return nil, fmt.Errorf("could not register request counter: %w", err)
	}

	// Initialize request duration histogram
	m.requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "request_duration_seconds",
			Help:      "Duration of webhook request processing in seconds",
			Buckets:   webhookDurationBuckets, // Use custom latency-optimized buckets
		},
		// Labels track duration by path and method
		[]string{"path", "method"},
	)
	if err := reg.Register(m.requestDuration); err != nil {
		return nil, fmt.Errorf("could not register request duration: %w", err)
	}

	// Initialize error counter metric
	m.errorCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "errors_total",
			Help:      "Total number of errors encountered during webhook processing",
		},
		// Labels provide context about where and why errors occurred
		[]string{"path", "method", "status"},
	)
	if err := reg.Register(m.errorCounter); err != nil {
		return nil, fmt.Errorf("could not register error counter: %w", err)
	}

	// Initialize readiness status gauge
	m.readinessGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "readiness_status",
			Help:      "Webhook readiness status (1 = ready, 0 = not ready)",
		},
	)
	if err := reg.Register(m.readinessGauge); err != nil {
		return nil, fmt.Errorf("could not register readiness gauge: %w", err)
	}

	// Initialize liveness status gauge
	m.livenessGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "liveness_status",
			Help:      "Webhook liveness status (1 = alive, 0 = not alive)",
		},
	)
	if err := reg.Register(m.livenessGauge); err != nil {
		return nil, fmt.Errorf("could not register liveness gauge: %w", err)
	}

	// Initialize label operations counter
	m.labelOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "label_operations_total",
			Help:      "Total number of label operations by result and namespace",
		},
		[]string{"operation", "namespace"},
	)
	if err := reg.Register(m.labelOperationsTotal); err != nil {
		return nil, fmt.Errorf("could not register label operations counter: %w", err)
	}

	// Initialize annotation validation counter
	m.annotationValidationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "annotation_validation_total",
			Help:      "Total number of annotation validation results by outcome and namespace",
		},
		[]string{"result", "namespace"},
	)
	if err := reg.Register(m.annotationValidationTotal); err != nil {
		return nil, fmt.Errorf("could not register annotation validation counter: %w", err)
	}

	// Store registry if a custom one was used
	if r, ok := reg.(*prometheus.Registry); ok {
		m.registry = r
	}

	return m, nil
}

// metricsMiddleware wraps an HTTP handler to collect performance metrics
//
// This middleware:
// - Tracks request duration
// - Counts total requests and errors
// - Recovers from panics
// - Provides detailed error tracking
//
// Metrics collected include:
// - Total requests by path, method, and status
// - Request processing duration
// - Error counts
func (m *metrics) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record start time for duration calculation
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := newStatusRecorder(w)

		// Recover from any panics in the handler
		defer func() {
			if err := recover(); err != nil {
				// Log the panic with stack trace
				log.Error().
					Interface("panic", err).
					Str("stack", string(debug.Stack())).
					Msg("Handler panic recovered")

				// Set 500 status
				wrapped.WriteHeader(http.StatusInternalServerError)

				// Record error metrics
				m.requestCounter.WithLabelValues(r.URL.Path, r.Method, "500").Inc()
				m.errorCounter.WithLabelValues(r.URL.Path, r.Method, "500").Inc()
				m.requestDuration.WithLabelValues(r.URL.Path, r.Method).Observe(time.Since(start).Seconds())
			}
		}()

		// Process the actual request
		next.ServeHTTP(wrapped, r)

		// Record metrics after request processing
		m.requestCounter.WithLabelValues(r.URL.Path, r.Method, fmt.Sprintf("%d", wrapped.status)).Inc()
		m.requestDuration.WithLabelValues(r.URL.Path, r.Method).Observe(time.Since(start).Seconds())

		// Track errors (status >= 400)
		if wrapped.status >= 400 {
			m.errorCounter.WithLabelValues(r.URL.Path, r.Method, fmt.Sprintf("%d", wrapped.status)).Inc()
		}
	})
}

// recordLabelOperation records the result of a label operation for a given namespace
func (m *metrics) recordLabelOperation(operation string, namespace string) {
	m.labelOperationsTotal.WithLabelValues(operation, sanitizeLabel(namespace)).Inc()
}

// sanitizeLabel ensures a string is safe to use as a metric label
func sanitizeLabel(s string) string {
	if !utf8.ValidString(s) {
		return "_invalid_utf8_"
	}
	if s == "" {
		return "_empty_"
	}
	return s
}

// recordAnnotationValidation records the result of annotation validation for a given namespace
func (m *metrics) recordAnnotationValidation(result string, namespace string) {
	m.annotationValidationTotal.WithLabelValues(result, sanitizeLabel(namespace)).Inc()
}

// updateHealthMetrics updates the health-related metrics
//
// This method:
// - Converts boolean readiness and liveness states to metric values
// - Sets gauge metrics to 0 or 1 based on current system state
//
// Parameters:
//   - ready: Indicates if the webhook is ready to handle requests
//   - alive: Indicates if the webhook is responsive
func (m *metrics) updateHealthMetrics(ready, alive bool) {
	// Convert boolean to float64 (1 for true, 0 for false)
	if ready {
		m.readinessGauge.Set(1)
	} else {
		m.readinessGauge.Set(0)
	}

	if alive {
		m.livenessGauge.Set(1)
	} else {
		m.livenessGauge.Set(0)
	}
}

// handler returns an HTTP handler for the Prometheus metrics endpoint
//
// If a custom registry was used during initialization, it uses that registry.
// Otherwise, it falls back to the default Prometheus handler.
//
// Returns an HTTP handler that can be used to expose metrics
func (m *metrics) handler() http.Handler {
	if m.registry != nil {
		return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
	}
	return promhttp.Handler()
}

// statusRecorder wraps http.ResponseWriter to capture the HTTP status code
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// newStatusRecorder creates a new statusRecorder
func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader captures the written status code
func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
