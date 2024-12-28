package webhook

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	metricsNamespace = "pod_label_webhook"
)

// metrics holds our Prometheus metrics
type metrics struct {
	requestCounter  *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	errorCounter    *prometheus.CounterVec
	readinessGauge  prometheus.Gauge
	livenessGauge   prometheus.Gauge
	registry        *prometheus.Registry
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
			Help:      "Request duration in seconds",
			Buckets:   prometheus.DefBuckets,
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

	if r, ok := reg.(*prometheus.Registry); ok {
		m.registry = r
	}

	return m, nil
}

// metricsMiddleware wraps an http.Handler and records metrics
func (m *metrics) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap ResponseWriter to capture status code
		wrapped := newStatusRecorder(w)
		next.ServeHTTP(wrapped, r)

		// Record metrics
		m.requestCounter.WithLabelValues(r.URL.Path, r.Method, fmt.Sprintf("%d", wrapped.status)).Inc()
		m.requestDuration.WithLabelValues(r.URL.Path, r.Method).Observe(time.Since(start).Seconds())

		// Record errors (status >= 400)
		if wrapped.status >= 400 {
			m.errorCounter.WithLabelValues(r.URL.Path, r.Method, fmt.Sprintf("%d", wrapped.status)).Inc()
		}
	})
}

// statusRecorder wraps http.ResponseWriter to capture status code
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// updateHealthMetrics updates the health-related metrics
func (m *metrics) updateHealthMetrics(ready, alive bool) {
	// Convert bool to float64 (1 for true, 0 for false)
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
