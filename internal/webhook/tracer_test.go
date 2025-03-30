package webhook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracerInitialization(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		insecure   bool
		expectNoop bool
	}{
		{
			name:       "disabled tracing with empty endpoint",
			endpoint:   "",
			insecure:   false,
			expectNoop: true,
		},
		{
			name:       "enabled tracing with endpoint",
			endpoint:   "localhost:4317",
			insecure:   true,
			expectNoop: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset tracer provider after each test
			defer func() {
				otel.SetTracerProvider(trace.NewTracerProvider())
			}()

			ctx := context.Background()
			tracer, err := initTracer(ctx, "test-ns", "test-service", "v1.0.0", tt.endpoint, tt.insecure)
			require.NoError(t, err)
			assert.NotNil(t, tracer)

			// Check if tracing is correctly enabled or disabled
			assert.Equal(t, !tt.expectNoop, tracer.enabled)
			
			// For disabled tracing, provider should be nil
			if tt.expectNoop {
				assert.Nil(t, tracer.tracerProvider)
			} else {
				assert.NotNil(t, tracer.tracerProvider)
			}
			
			// Test shutdown works without errors
			err = tracer.shutdown(ctx)
			assert.NoError(t, err)
		})
	}
}

func TestTracerStartSpan(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		attributes []string
		expectAttrs int
	}{
		{
			name:       "span with enabled tracing",
			enabled:    true,
			attributes: []string{"key1", "value1", "key2", "value2"},
			expectAttrs: 2,
		},
		{
			name:       "span with disabled tracing",
			enabled:    false,
			attributes: []string{"key1", "value1"},
			expectAttrs: 0,
		},
		{
			name:       "span with odd number of attributes",
			enabled:    true,
			attributes: []string{"key1", "value1", "orphan"},
			expectAttrs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test tracer
			sr := tracetest.NewSpanRecorder()
			tp := trace.NewTracerProvider(trace.WithSpanProcessor(sr))
			
			tracer := &tracer{
				tracerProvider: tp,
				enabled:        tt.enabled,
			}
			
			// If tracer is disabled, we're testing no-op behavior
			if !tt.enabled {
				tracer.tracerProvider = nil
			} else {
				// Set global tracer provider
				otel.SetTracerProvider(tp)
			}
			
			// Reset tracer provider after each test
			defer func() {
				otel.SetTracerProvider(trace.NewTracerProvider())
			}()
			
			ctx := context.Background()
			spanCtx, span := tracer.startSpan(ctx, "test-operation", tt.attributes...)
			assert.NotNil(t, spanCtx)
			assert.NotNil(t, span)
			
			// End span to flush it to the recorder
			span.End()
			
			// Check attributes if tracing is enabled
			if tt.enabled {
				spans := sr.Ended()
				if assert.Equal(t, 1, len(spans), "Expected one span") {
					recordedSpan := spans[0]
					assert.Equal(t, "test-operation", recordedSpan.Name())
					
					// Check that all expected attributes are present
					attrs := recordedSpan.Attributes()
					actualAttrs := make(map[string]string)
					for _, attr := range attrs {
						if attr.Value.AsString() != "" {
							actualAttrs[string(attr.Key)] = attr.Value.AsString()
						}
					}
					
					assert.Equal(t, tt.expectAttrs, len(attrs), "Expected %d attributes but got %d", tt.expectAttrs, len(attrs))
					
					// Check each expected pair
					for i := 0; i < len(tt.attributes)-1 && i+1 < len(tt.attributes); i += 2 {
						key := tt.attributes[i]
						value := tt.attributes[i+1]
						actualValue, ok := actualAttrs[key]
						assert.True(t, ok, "Expected attribute %s to be present", key)
						if ok {
							assert.Equal(t, value, actualValue, "Expected attribute %s to have value %s but got %s", key, value, actualValue)
						}
					}
				}
			}
		})
	}
}