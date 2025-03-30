package webhook

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTracingMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		requestHeaders map[string]string
		statusCode     int
		tracingEnabled bool
	}{
		{
			name:           "successful GET request",
			method:         "GET",
			path:           "/test",
			statusCode:     http.StatusOK,
			tracingEnabled: true,
		},
		{
			name:           "error response",
			method:         "POST",
			path:           "/error",
			statusCode:     http.StatusBadRequest,
			tracingEnabled: true,
		},
		{
			name:       "with request ID",
			method:     "GET",
			path:       "/test",
			statusCode: http.StatusOK,
			requestHeaders: map[string]string{
				"X-Request-ID": "test-request-id",
			},
			tracingEnabled: true,
		},
		{
			name:           "tracing disabled",
			method:         "GET",
			path:           "/test",
			statusCode:     http.StatusOK,
			tracingEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a span recorder to capture spans
			sr := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
			
			// Set the global trace provider
			otel.SetTracerProvider(tp)
			
			// Set the global propagator
			otel.SetTextMapPropagator(propagation.TraceContext{})
			
			// Create server with a test tracer
			server := &Server{
				logger: zerolog.Nop(),
				tracer: &tracer{
					tracerProvider: tp,
					enabled:        tt.tracingEnabled,
				},
			}
			
			// Create test handler
			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Check that context contains span if tracing is enabled
				if tt.tracingEnabled {
					span := trace.SpanFromContext(r.Context())
					assert.NotNil(t, span, "Expected span in context")
				}
				
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte("test"))
			})
			
			// Wrap with tracing middleware
			handler := server.tracingMiddleware(testHandler)
			
			// Create request with correct headers and context
			req := httptest.NewRequest(tt.method, tt.path, nil)
			for k, v := range tt.requestHeaders {
				req.Header.Set(k, v)
			}
			
			// Serve the request
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			
			// Verify response
			assert.Equal(t, tt.statusCode, w.Code)
			
			// Verify spans were created if tracing is enabled
			if tt.tracingEnabled {
				spans := sr.Ended()
				assert.Len(t, spans, 1, "Expected one span to be created")
				
				if len(spans) > 0 {
					span := spans[0]
					assert.Equal(t, "http_request", span.Name())
					
					// Check attributes
					attrs := span.Attributes()
					attrMap := make(map[string]string)
					for _, attr := range attrs {
						if attr.Value.AsString() != "" {
							attrMap[string(attr.Key)] = attr.Value.AsString()
						}
					}
					
					assert.Equal(t, tt.method, attrMap["http.method"])
					assert.Equal(t, tt.path, attrMap["http.path"])
					
					// Check request ID if provided
					if reqID, ok := tt.requestHeaders["X-Request-ID"]; ok {
						assert.Equal(t, reqID, attrMap["request.id"])
					}
				}
			} else {
				spans := sr.Ended()
				assert.Empty(t, spans, "No spans should be created when tracing is disabled")
			}
		})
	}
}

func TestMiddlewareChaining(t *testing.T) {
	// Create test spans recorder
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	
	// Create metrics registry
	reg := setupTestRegistry(t)
	metrics, err := initMetrics(reg)
	require.NoError(t, err)
	
	// Create a server with both tracing and metrics
	server := &Server{
		logger: zerolog.Nop(),
		metrics: metrics,
		tracer: &tracer{
			tracerProvider: tp,
			enabled: true,
		},
	}
	
	// Define handler function that will be wrapped
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	
	// Create middleware chain
	handler := server.tracingMiddleware(server.metrics.metricsMiddleware(testHandler))
	
	// Create and serve request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	
	// Verify the response
	assert.Equal(t, http.StatusOK, w.Code)
	
	// Verify trace was created
	spans := sr.Ended()
	assert.Len(t, spans, 1)
	
	// Verify metrics were recorded
	counter, err := metrics.requestCounter.GetMetricWith(map[string]string{
		"path":   "/test",
		"method": "GET",
		"status": "200",
	})
	require.NoError(t, err)
	assert.Equal(t, float64(1), extractMetricValue(counter))
}

// setupTestRegistry creates a new test registry for metrics testing
func setupTestRegistry(t *testing.T) *prometheus.Registry {
	t.Helper()
	return prometheus.NewRegistry()
}