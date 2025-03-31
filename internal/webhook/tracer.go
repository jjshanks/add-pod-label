package webhook

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// tracerName is the name of the tracer used by the webhook
	tracerName = "github.com/jjshanks/add-pod-label"
)

// tracer is responsible for managing OpenTelemetry tracing functionality.
// It handles trace provider setup, shutdown, and provides access to the global tracer.
type tracer struct {
	// tracerProvider is the OpenTelemetry trace provider
	tracerProvider *sdktrace.TracerProvider
	// enabled indicates whether tracing is enabled
	enabled bool
}

// initTracer initializes OpenTelemetry tracing.
// It sets up a trace provider with OTLP exporter configured for gRPC protocol.
//
// Parameters:
//   - ctx: Context for cancellation and deadlines
//   - serviceNamespace: Namespace of the service for resource attribution
//   - serviceName: Name of the service for resource attribution
//   - serviceVersion: Version of the service for resource attribution
//   - endpoint: OTLP exporter endpoint (e.g., "otel-collector:4317")
//   - insecure: Whether to use insecure connection to the collector
//
// Returns:
//   - A new initialized tracer instance
//   - Error if initialization fails
func initTracer(ctx context.Context, serviceNamespace, serviceName, serviceVersion, endpoint string, insecure bool) (*tracer, error) {
	// If endpoint is empty, tracing is disabled
	if endpoint == "" {
		log.Info().Msg("Tracing is disabled (no endpoint configured)")
		return &tracer{enabled: false}, nil
	}

	log.Info().
		Str("service", serviceName).
		Str("namespace", serviceNamespace).
		Str("version", serviceVersion).
		Str("endpoint", endpoint).
		Bool("insecure", insecure).
		Msg("Initializing OpenTelemetry tracing")

	// Create secure or insecure client options
	var clientOpts []otlptracegrpc.Option
	clientOpts = append(clientOpts, otlptracegrpc.WithEndpoint(endpoint))
	if insecure {
		clientOpts = append(clientOpts, otlptracegrpc.WithInsecure())
	}

	// Create exporter with more detailed logging
	log.Info().
		Str("endpoint", endpoint).
		Bool("insecure", insecure).
		Msg("Creating OTLP trace exporter")
	
	client := otlptracegrpc.NewClient(clientOpts...)
	exporter, err := otlptrace.New(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}
	
	log.Info().Msg("OTLP trace exporter created successfully")

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNamespace(serviceNamespace),
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create trace provider with batch span processor
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Set global trace provider and propagator
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &tracer{
		tracerProvider: tp,
		enabled:        true,
	}, nil
}

// shutdown gracefully shuts down the tracer's provider.
// It ensures all spans are flushed and resources are released.
//
// Parameters:
//   - ctx: Context for cancellation and deadlines
//
// Returns:
//   - Error if shutdown fails
func (t *tracer) shutdown(ctx context.Context) error {
	if !t.enabled || t.tracerProvider == nil {
		return nil
	}

	// Set timeout for shutdown
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	log.Debug().Msg("Shutting down tracer provider")
	if err := t.tracerProvider.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown tracer provider: %w", err)
	}
	return nil
}

// startSpan starts a new span with the given parent context and operation name.
// It creates attributes from the provided key-value pairs.
//
// Parameters:
//   - ctx: Parent context
//   - operationName: Name of the operation (e.g., "handleMutate")
//   - keyValues: Attributes to attach to the span (must be even number of arguments)
//
// Returns:
//   - New context containing the span
//   - The created span
//   - error if any issues occurred (e.g., odd number of key-value pairs)
func (t *tracer) startSpan(ctx context.Context, operationName string, keyValues ...string) (context.Context, trace.Span, error) {
	if !t.enabled {
		// Return a no-op span when tracing is disabled
		return ctx, trace.SpanFromContext(ctx), nil
	}

	// Check for even number of key-value pairs
	if len(keyValues)%2 != 0 {
		err := fmt.Errorf("odd number of key-value pairs provided for span attributes in operation '%s'", operationName)
		log.Warn().
			Str("operation", operationName).
			Int("attributes_count", len(keyValues)).
			Err(err).
			Msg("Invalid span attributes")
		return ctx, nil, err
	}

	// Create attributes from key-value pairs
	var attrs []attribute.KeyValue
	for i := 0; i < len(keyValues); i += 2 {
		key := keyValues[i]
		value := keyValues[i+1]
		attrs = append(attrs, attribute.String(key, value))
	}

	// Start span with attributes
	tr := otel.Tracer(tracerName)
	ctx, span := tr.Start(ctx, operationName, trace.WithAttributes(attrs...))
	return ctx, span, nil
}