// metrics.go
package webhook

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

const (
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
	// Buckets optimized for webhook latencies: 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s
	webhookDurationBuckets = []float64{0.005, 0.010, 0.025, 0.050, 0.100, 0.250, 0.500, 1.000, 2.500, 5.000}
)

// metrics holds our Prometheus metrics
type metrics struct {
	// requestCounter tracks the total number of requests processed
	// Labels: path, method, status
	requestCounter *prometheus.CounterVec

	// requestDuration tracks the duration of requests
	// Labels: path, method
	requestDuration *prometheus.HistogramVec

	// errorCounter tracks the total number of errors encountered
	// Labels: path, method, status
	errorCounter *prometheus.CounterVec

	// readinessGauge indicates the current readiness status (1 ready, 0 not ready)
	readinessGauge prometheus.Gauge

	// livenessGauge indicates the current liveness status (1 alive, 0 not alive)
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

// initMetrics initializes Prometheus metrics with an optional registry
func initMetrics(reg prometheus.Registerer) (*metrics, error) {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	m := &metrics{}

	// Request counter
	m.requestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "requests_total",
			Help:      "Total number of requests processed",
		},
		[]string{"path", "method", "status"},
	)
	if err := reg.Register(m.requestCounter); err != nil {
		return nil, fmt.Errorf("could not register request counter: %w", err)
	}

	// Request duration histogram
	m.requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "request_duration_seconds",
			Help:      "Duration of webhook request processing in seconds",
			Buckets:   webhookDurationBuckets,
		},
		[]string{"path", "method"},
	)
	if err := reg.Register(m.requestDuration); err != nil {
		return nil, fmt.Errorf("could not register request duration: %w", err)
	}

	// Error counter
	m.errorCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "errors_total",
			Help:      "Total number of errors encountered",
		},
		[]string{"path", "method", "status"},
	)
	if err := reg.Register(m.errorCounter); err != nil {
		return nil, fmt.Errorf("could not register error counter: %w", err)
	}

	// Readiness gauge
	m.readinessGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "readiness_status",
			Help:      "Current readiness status (1 for ready, 0 for not ready)",
		},
	)
	if err := reg.Register(m.readinessGauge); err != nil {
		return nil, fmt.Errorf("could not register readiness gauge: %w", err)
	}

	// Liveness gauge
	m.livenessGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "liveness_status",
			Help:      "Current liveness status (1 for alive, 0 for not alive)",
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

// metricsMiddleware wraps an http.Handler and records metrics
func (m *metrics) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := newStatusRecorder(w)

		defer func() {
			if err := recover(); err != nil {
				// Log the panic
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

		next.ServeHTTP(wrapped, r)

		// Record metrics after successful handling
		m.requestCounter.WithLabelValues(r.URL.Path, r.Method, fmt.Sprintf("%d", wrapped.status)).Inc()
		m.requestDuration.WithLabelValues(r.URL.Path, r.Method).Observe(time.Since(start).Seconds())

		// Record errors (status >= 400)
		if wrapped.status >= 400 {
			m.errorCounter.WithLabelValues(r.URL.Path, r.Method, fmt.Sprintf("%d", wrapped.status)).Inc()
		}
	})
}

// recordLabelOperation records the result of a label operation for a given namespace
func (m *metrics) recordLabelOperation(operation string, namespace string) {
	m.labelOperationsTotal.WithLabelValues(operation, namespace).Inc()
}

// recordAnnotationValidation records the result of annotation validation for a given namespace
func (m *metrics) recordAnnotationValidation(result string, namespace string) {
	m.annotationValidationTotal.WithLabelValues(result, namespace).Inc()
}

// updateHealthMetrics updates the health-related metrics
func (m *metrics) updateHealthMetrics(ready, alive bool) {
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

// handler returns a handler for /metrics endpoint
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
