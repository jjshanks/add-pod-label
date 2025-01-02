package webhook

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jjshanks/pod-label-webhook/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to safely extract float value from a metric
func extractMetricValue(metric interface{}) float64 {
	switch m := metric.(type) {
	case prometheus.Metric:
		pb := &dto.Metric{}
		err := m.Write(pb)
		if err != nil {
			return 0.0
		}
		switch {
		case pb.Gauge != nil:
			return pb.Gauge.GetValue()
		case pb.Counter != nil:
			return pb.Counter.GetValue()
		default:
			return 0.0
		}
	case *dto.Metric:
		switch {
		case m.Gauge != nil:
			return m.Gauge.GetValue()
		case m.Counter != nil:
			return m.Counter.GetValue()
		default:
			return 0.0
		}
	default:
		return 0.0
	}
}

func TestMetricsInitialization(t *testing.T) {
	tests := []struct {
		name     string
		registry *prometheus.Registry
		wantErr  bool
	}{
		{
			name:    "successful initialization with default registry",
			wantErr: false,
		},
		{
			name:     "initialization with custom registry",
			registry: prometheus.NewRegistry(),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := tt.registry
			if reg == nil {
				reg = prometheus.NewRegistry()
			}

			m, err := initMetrics(reg)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, m)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, m)

			// Verify metrics are registered
			metrics, err := reg.Gather()
			require.NoError(t, err)
			assert.NotEmpty(t, metrics, "Expected metrics to be registered")
		})
	}
}

func TestMetricsMiddlewareEdgeCases(t *testing.T) {
	// Helper function to create a large string of specified size in KB
	createLargeString := func(sizeKB int) string {
		chunk := strings.Repeat("x", 1024) // 1KB chunk
		return strings.Repeat(chunk, sizeKB)
	}

	tests := []struct {
		name           string
		path           string
		method         string
		statusCode     int
		expectedLabels map[string]string
		requestBody    string
		responseBody   string
		handler        http.HandlerFunc
		sleep          time.Duration
	}{
		{
			name:       "panicking handler",
			path:       "/panic",
			method:     "POST",
			statusCode: http.StatusInternalServerError,
			handler: func(w http.ResponseWriter, r *http.Request) {
				panic("intentional panic for testing")
			},
		},
		{
			name:       "slow handler",
			path:       "/slow",
			method:     "GET",
			statusCode: http.StatusOK,
			sleep:      2 * time.Second,
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(2 * time.Second)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:        "large request body",
			path:        "/large-request",
			method:      "POST",
			statusCode:  http.StatusOK,
			requestBody: createLargeString(500), // 500KB request
		},
		{
			name:       "large response body",
			path:       "/large-response",
			method:     "GET",
			statusCode: http.StatusOK,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(createLargeString(500))) // 500KB response
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			m, err := initMetrics(reg)
			require.NoError(t, err)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.handler != nil {
					tt.handler(w, r)
					return
				}
				w.WriteHeader(tt.statusCode)
				if tt.responseBody != "" {
					_, _ = w.Write([]byte(tt.responseBody))
				}
			})

			// Create test request
			var body io.Reader
			if tt.requestBody != "" {
				body = strings.NewReader(tt.requestBody)
			}
			req := httptest.NewRequest(tt.method, tt.path, body)
			w := httptest.NewRecorder()

			// Wrap handler with metrics middleware
			m.metricsMiddleware(handler).ServeHTTP(w, req)

			// Verify metrics were recorded
			metrics, err := reg.Gather()
			require.NoError(t, err)
			assert.NotEmpty(t, metrics)

			// For panic case, verify error metrics
			if tt.path == "/panic" {
				errorCounter, err := m.errorCounter.GetMetricWith(map[string]string{
					"path":   tt.path,
					"method": tt.method,
					"status": "500",
				})
				require.NoError(t, err)
				assert.Equal(t, float64(1), extractMetricValue(errorCounter))
			}

			// For slow handler, verify duration metric is recorded in appropriate bucket
			if tt.sleep > 0 {
				// Get all metric families
				metricFamilies, err := reg.Gather()
				require.NoError(t, err)

				var foundDurationMetric bool
				expectedSeconds := tt.sleep.Seconds()

				// Find the duration histogram metric
				for _, mf := range metricFamilies {
					if mf.GetName() == "pod_label_webhook_request_duration_seconds" {
						foundDurationMetric = true
						// Look through histogram buckets
						for _, m := range mf.GetMetric() {
							if m.GetHistogram() != nil {
								// Verify the request duration was recorded in appropriate bucket
								lastCount := uint64(0)
								for _, bucket := range m.GetHistogram().GetBucket() {
									if bucket.GetUpperBound() > expectedSeconds {
										// Verify this bucket has a higher count than the previous bucket
										assert.Greater(t, bucket.GetCumulativeCount(), lastCount,
											"Expected slow request to be recorded in bucket with upper bound %v",
											bucket.GetUpperBound())
										break
									}
									lastCount = bucket.GetCumulativeCount()
								}
							}
						}
					}
				}
				assert.True(t, foundDurationMetric, "Expected to find duration metric")
			}
		})
	}
}

func TestMetricsMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		method         string
		statusCode     int
		expectedLabels map[string]string
	}{
		{
			name:       "successful request",
			path:       "/test",
			method:     "GET",
			statusCode: http.StatusOK,
			expectedLabels: map[string]string{
				"path":   "/test",
				"method": "GET",
				"status": "200",
			},
		},
		{
			name:       "error request",
			path:       "/error",
			method:     "POST",
			statusCode: http.StatusBadRequest,
			expectedLabels: map[string]string{
				"path":   "/error",
				"method": "POST",
				"status": "400",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			m, err := initMetrics(reg)
			require.NoError(t, err)

			// Create test handler that returns the specified status code
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			// Create test request
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			// Wrap handler with metrics middleware
			m.metricsMiddleware(handler).ServeHTTP(w, req)

			// Verify request counter
			counter, err := m.requestCounter.GetMetricWith(tt.expectedLabels)
			require.NoError(t, err)
			assert.Equal(t, float64(1), extractMetricValue(counter))

			// Verify error counter for error cases
			if tt.statusCode >= 400 {
				errCounter, err := m.errorCounter.GetMetricWith(tt.expectedLabels)
				require.NoError(t, err)
				assert.Equal(t, float64(1), extractMetricValue(errCounter))
			}

			// Verify duration histogram exists
			duration, err := m.requestDuration.GetMetricWith(map[string]string{
				"path":   tt.path,
				"method": tt.method,
			})
			require.NoError(t, err)
			assert.NotNil(t, duration)
		})
	}
}

func TestUpdateHealthMetrics(t *testing.T) {
	tests := []struct {
		name          string
		ready         bool
		alive         bool
		wantReadiness float64
		wantLiveness  float64
	}{
		{
			name:          "both ready and alive",
			ready:         true,
			alive:         true,
			wantReadiness: 1,
			wantLiveness:  1,
		},
		{
			name:          "not ready but alive",
			ready:         false,
			alive:         true,
			wantReadiness: 0,
			wantLiveness:  1,
		},
		{
			name:          "ready but not alive",
			ready:         true,
			alive:         false,
			wantReadiness: 1,
			wantLiveness:  0,
		},
		{
			name:          "neither ready nor alive",
			ready:         false,
			alive:         false,
			wantReadiness: 0,
			wantLiveness:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			m, err := initMetrics(reg)
			require.NoError(t, err)

			m.updateHealthMetrics(tt.ready, tt.alive)

			assert.Equal(t, tt.wantReadiness, extractMetricValue(m.readinessGauge))
			assert.Equal(t, tt.wantLiveness, extractMetricValue(m.livenessGauge))
		})
	}
}

func TestMetricsEndpoint(t *testing.T) {
	tests := []struct {
		name           string
		setupMetrics   func(*metrics)
		expectedMetric string
	}{
		{
			name: "request counter metric",
			setupMetrics: func(m *metrics) {
				m.requestCounter.WithLabelValues("/test", "GET", "200").Inc()
			},
			expectedMetric: `pod_label_webhook_requests_total{method="GET",path="/test",status="200"} 1`,
		},
		{
			name: "health metrics",
			setupMetrics: func(m *metrics) {
				m.updateHealthMetrics(true, true)
			},
			expectedMetric: `pod_label_webhook_readiness_status 1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			m, err := initMetrics(reg)
			require.NoError(t, err)

			// Setup test metrics
			if tt.setupMetrics != nil {
				tt.setupMetrics(m)
			}

			// Create test request for metrics endpoint
			req := httptest.NewRequest("GET", "/metrics", nil)
			w := httptest.NewRecorder()

			// Serve metrics
			m.handler().ServeHTTP(w, req)

			// Read response
			resp := w.Result()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Verify response
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Contains(t, string(body), tt.expectedMetric)
		})
	}
}

func TestStatusRecorder(t *testing.T) {
	tests := []struct {
		name       string
		writeCode  int
		wantStatus int
	}{
		{
			name:       "default status",
			writeCode:  0,
			wantStatus: http.StatusOK,
		},
		{
			name:       "custom status",
			writeCode:  http.StatusNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "server error status",
			writeCode:  http.StatusInternalServerError,
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			recorder := newStatusRecorder(w)

			if tt.writeCode != 0 {
				recorder.WriteHeader(tt.writeCode)
			}

			assert.Equal(t, tt.wantStatus, recorder.status)
		})
	}
}

func TestIntegrationWithServer(t *testing.T) {
	// Create a new server with metrics
	cfg := &config.Config{
		Address:  "localhost:8443",
		CertFile: "/tmp/cert",
		KeyFile:  "/tmp/key",
		LogLevel: "debug",
	}

	tests := []struct {
		name       string
		endpoint   string
		method     string
		body       string
		wantStatus int
		wantMetric string
		checkError bool
	}{
		{
			name:       "successful mutate request",
			endpoint:   "/mutate",
			method:     "POST",
			body:       "{}",
			wantStatus: http.StatusBadRequest, // Because the body isn't valid admission review
			wantMetric: `pod_label_webhook_requests_total{method="POST",path="/mutate",status="400"} 1`,
			checkError: true,
		},
		{
			name:       "health check",
			endpoint:   "/healthz",
			method:     "GET",
			wantStatus: http.StatusOK,
			wantMetric: `pod_label_webhook_requests_total{method="GET",path="/healthz",status="200"} 1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			srv, err := NewTestServer(cfg, reg)
			require.NoError(t, err)

			// Create test request
			req := httptest.NewRequest(tt.method, tt.
				endpoint, strings.NewReader(tt.body))
			if tt.endpoint == "/mutate" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()

			// Create handler based on endpoint
			var handler http.Handler
			switch tt.endpoint {
			case "/mutate":
				handler = srv.metrics.metricsMiddleware(http.HandlerFunc(srv.handleMutate))
			case "/healthz":
				handler = srv.metrics.metricsMiddleware(http.HandlerFunc(srv.handleLiveness))
			default:
				t.Fatalf("unknown endpoint: %s", tt.endpoint)
			}

			// Serve request through the metrics middleware
			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			// Verify metrics
			metricsReq := httptest.NewRequest("GET", "/metrics", nil)
			metricsW := httptest.NewRecorder()
			srv.metrics.handler().ServeHTTP(metricsW, metricsReq)

			metricsBody, err := io.ReadAll(metricsW.Body)
			require.NoError(t, err)
			assert.Contains(t, string(metricsBody), tt.wantMetric)

			if tt.checkError && w.Code >= 400 {
				assert.Contains(t, string(metricsBody), `pod_label_webhook_errors_total`)
			}
		})
	}
}

func TestLabelOperationMetrics(t *testing.T) {
	tests := []struct {
		name           string
		operation      string
		namespace      string
		recorded       bool // Indicates whether the metric should be recorded
		expectedLabels map[string]string
	}{
		{
			name:      "successful label operation",
			operation: labelOperationSuccess,
			namespace: "default",
			recorded:  true,
			expectedLabels: map[string]string{
				"operation": "success",
				"namespace": "default",
			},
		},
		{
			name:      "skipped label operation",
			operation: labelOperationSkipped,
			namespace: "test-ns",
			recorded:  true,
			expectedLabels: map[string]string{
				"operation": "skipped",
				"namespace": "test-ns",
			},
		},
		{
			name:      "error label operation",
			operation: labelOperationError,
			namespace: "error-ns",
			recorded:  true,
			expectedLabels: map[string]string{
				"operation": "error",
				"namespace": "error-ns",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new registry for each test to ensure clean state
			reg := prometheus.NewRegistry()
			m, err := initMetrics(reg)
			require.NoError(t, err)

			// Explicitly record the metric if recorded is true
			if tt.recorded {
				m.recordLabelOperation(tt.operation, tt.namespace)
			}

			// Gather metrics and print for debugging
			metrics, err := reg.Gather()
			require.NoError(t, err)

			// Debug: print all gathered metrics
			t.Logf("Total metric families gathered: %d", len(metrics))
			for _, mf := range metrics {
				t.Logf("Metric family name: %s", mf.GetName())
				for _, m := range mf.GetMetric() {
					labels := m.GetLabel()
					labelStr := ""
					for _, label := range labels {
						labelStr += fmt.Sprintf("%s=%s ", label.GetName(), label.GetValue())
					}
					t.Logf("  Metric labels: %s", labelStr)
				}
			}

			var found bool
			for _, mf := range metrics {
				if mf.GetName() == "pod_label_webhook_label_operations_total" {
					for _, m := range mf.GetMetric() {
						labels := m.GetLabel()

						// Check if the labels match exactly
						if len(labels) == 2 {
							labelMap := make(map[string]string)
							for _, label := range labels {
								labelMap[label.GetName()] = label.GetValue()
							}

							// Compare labels exactly
							if labelMap["operation"] == tt.operation &&
								labelMap["namespace"] == tt.namespace {

								found = true

								// Check the value
								expectedValue := float64(1)
								if !tt.recorded {
									expectedValue = float64(0)
								}
								metricValue := extractMetricValue(m)
								assert.Equal(t, expectedValue, metricValue,
									"Unexpected metric value for %s operation in %s namespace. Got %v, want %v",
									tt.operation, tt.namespace, metricValue, expectedValue)
								break
							}
						}
					}
				}
			}

			// If recording was expected, assert that the metric was found
			if tt.recorded {
				assert.True(t, found,
					"Expected metric for %s operation in %s namespace was not found. "+
						"Searched for labels: %v",
					tt.operation, tt.namespace, tt.expectedLabels)
			}
		})
	}
}

func TestAnnotationValidationMetrics(t *testing.T) {
	tests := []struct {
		name           string
		result         string
		namespace      string
		recorded       bool // Indicates whether the metric should be recorded
		expectedLabels map[string]string
	}{
		{
			name:      "valid annotation",
			result:    annotationValid,
			namespace: "default",
			recorded:  true,
			expectedLabels: map[string]string{
				"result":    "valid",
				"namespace": "default",
			},
		},
		{
			name:      "invalid annotation",
			result:    annotationInvalid,
			namespace: "test-ns",
			recorded:  true,
			expectedLabels: map[string]string{
				"result":    "invalid",
				"namespace": "test-ns",
			},
		},
		{
			name:      "missing annotation",
			result:    annotationMissing,
			namespace: "missing-ns",
			recorded:  true,
			expectedLabels: map[string]string{
				"result":    "missing",
				"namespace": "missing-ns",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new registry for each test to ensure clean state
			reg := prometheus.NewRegistry()
			m, err := initMetrics(reg)
			require.NoError(t, err)

			// Explicitly record the metric if recorded is true
			if tt.recorded {
				m.recordAnnotationValidation(tt.result, tt.namespace)
			}

			// Gather metrics and print for debugging
			metrics, err := reg.Gather()
			require.NoError(t, err)

			// Debug: print all gathered metrics
			t.Logf("Total metric families gathered: %d", len(metrics))
			for _, mf := range metrics {
				t.Logf("Metric family name: %s", mf.GetName())
				for _, m := range mf.GetMetric() {
					labels := m.GetLabel()
					labelStr := ""
					for _, label := range labels {
						labelStr += fmt.Sprintf("%s=%s ", label.GetName(), label.GetValue())
					}
					t.Logf("  Metric labels: %s", labelStr)
				}
			}

			var found bool
			for _, mf := range metrics {
				if mf.GetName() == "pod_label_webhook_annotation_validation_total" {
					for _, m := range mf.GetMetric() {
						labels := m.GetLabel()

						// Check if the labels match exactly
						if len(labels) == 2 {
							labelMap := make(map[string]string)
							for _, label := range labels {
								labelMap[label.GetName()] = label.GetValue()
							}

							// Compare labels exactly
							if labelMap["result"] == tt.result &&
								labelMap["namespace"] == tt.namespace {

								found = true

								// Check the value
								expectedValue := float64(1)
								if !tt.recorded {
									expectedValue = float64(0)
								}
								metricValue := extractMetricValue(m)
								assert.Equal(t, expectedValue, metricValue,
									"Unexpected metric value for %s result in %s namespace. Got %v, want %v",
									tt.result, tt.namespace, metricValue, expectedValue)
								break
							}
						}
					}
				}
			}

			// If recording was expected, assert that the metric was found
			if tt.recorded {
				assert.True(t, found,
					"Expected metric for %s result in %s namespace was not found. "+
						"Searched for labels: %v",
					tt.result, tt.namespace, tt.expectedLabels)
			}
		})
	}
}

func TestSanitizeLabel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"valid_label", "valid_label"},
		{"valid-label", "valid-label"},
		{"123numeric", "_123numeric"},
		{"namespace/test", "namespace_test"},
		{"special!@#$%^&*()chars", "special_chars"},
		{"", "_empty_"},
		{"Γ£û∩╕Åinvalid_unicode", "_invalid_unicode"},
		{"a" + strings.Repeat("x", 100), "a" + strings.Repeat("x", 62)},
		{"a" + strings.Repeat("x", 200), "a" + strings.Repeat("x", 62)},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeLabel(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
