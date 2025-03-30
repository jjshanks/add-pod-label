package webhook

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

// tracingMiddleware wraps an HTTP handler to add tracing.
// It creates spans for incoming requests and propagates context.
//
// This middleware:
// - Extracts trace context from the request headers
// - Creates a new span for the request
// - Adds HTTP attributes like method, path, and status code
// - Adds request ID to span attributes
// - Passes the span context to the next handler
// - Ends the span when the request is complete
func (s *Server) tracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.tracer.enabled {
			// If tracing is disabled, just pass through
			next.ServeHTTP(w, r)
			return
		}

		// Extract trace context from request headers
		ctx := r.Context()
		propagator := propagation.TraceContext{}
		ctx = propagator.Extract(ctx, propagation.HeaderCarrier(r.Header))

		// Start a new span for this request
		ctx, span := s.tracer.startSpan(ctx, "http_request",
			"http.method", r.Method,
			"http.url", r.URL.String(),
			"http.path", r.URL.Path,
		)
		defer span.End()

		// Add request ID to span if available
		reqID := r.Header.Get("X-Request-ID")
		if reqID != "" {
			span.SetAttributes(attribute.String("request.id", reqID))
		}

		// Wrap response writer to capture status code
		wrapped := newStatusRecorder(w)

		// Call the next handler with the span context
		next.ServeHTTP(wrapped, r.WithContext(ctx))

		// Add status code attribute after the request is complete
		span.SetAttributes(attribute.Int("http.status_code", wrapped.status))
	})
}