// Package webhook provides functionality for webhook operations.
// This file implements the main webhook server, handling initialization,
// TLS configuration, request routing, and graceful shutdown.
package webhook

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	"github.com/jjshanks/pod-label-webhook/internal/config"
)

const (
	// readHeaderTimeout defines the maximum time allowed to read request headers.
	// This helps prevent slow-loris attacks.
	readHeaderTimeout = 10 * time.Second

	// writeTimeout limits the time for writing the full response.
	// This prevents clients from maintaining connections indefinitely.
	writeTimeout = 10 * time.Second

	// readTimeout sets the maximum time for reading the entire request.
	// This includes the time to read headers and body.
	readTimeout = 10 * time.Second

	// idleTimeout specifies how long an idle connection is kept open.
	// This allows connection reuse while preventing resource exhaustion.
	idleTimeout = 120 * time.Second

	// defaultGracefulTimeout is the default timeout for graceful server shutdown.
	// The server will wait this long for existing requests to complete before
	// forcing a shutdown.
	defaultGracefulTimeout = 30 * time.Second
)

// Server represents the webhook server instance.
// It manages the HTTP server, metrics, logging, health state, and tracing.
type Server struct {
	logger          zerolog.Logger // Structured logger for server events
	config          *config.Config // Server configuration
	health          *healthState   // Server health tracking
	metrics         *metrics       // Prometheus metrics collection
	tracer          *tracer        // OpenTelemetry tracer
	server          *http.Server   // Underlying HTTP server
	gracefulTimeout time.Duration  // Maximum time to wait during shutdown
	serverMu        sync.RWMutex   // Protects server field during updates
}

// NewServer creates a new webhook server instance with the provided configuration.
// It initializes logging, metrics collection, tracing, and server settings with secure defaults.
func NewServer(cfg *config.Config) (*Server, error) {
	// Create base logger with common fields
	logger := zerolog.New(os.Stdout).With().
		Timestamp().
		Str("service", "pod-label-webhook").
		Logger()

	// Configure log level
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}
	logger = logger.Level(level)

	// Configure console output if needed
	if cfg.Console {
		logger = logger.Output(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "2006-01-02T15:04:05.000Z",
		})
	}

	// Initialize metrics with new registry
	reg := prometheus.NewRegistry()
	m, err := initMetrics(reg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}
	
	// Initialize OpenTelemetry tracer if enabled
	var tr *tracer
	if cfg.TracingEnabled {
		// We derive the endpoint from config
		endpoint := cfg.TracingEndpoint
		if endpoint == "" {
			logger.Info().Msg("Tracing enabled but no endpoint specified, using default localhost:4317")
			endpoint = "localhost:4317"
		}
		
		ctx := context.Background()
		tr, err = initTracer(ctx, 
			cfg.ServiceNamespace,
			cfg.ServiceName,
			cfg.ServiceVersion,
			endpoint,
			cfg.TracingInsecure)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize tracer: %w", err)
		}
		logger.Info().
			Str("endpoint", endpoint).
			Bool("insecure", cfg.TracingInsecure).
			Msg("OpenTelemetry tracing initialized")
	} else {
		// Create disabled tracer
		tr = &tracer{enabled: false}
		logger.Info().Msg("OpenTelemetry tracing is disabled")
	}

	return &Server{
		logger:          logger,
		config:          cfg,
		health:          newHealthState(realClock{}),
		metrics:         m,
		tracer:          tr,
		gracefulTimeout: defaultGracefulTimeout,
		serverMu:        sync.RWMutex{},
	}, nil
}

// Run starts the webhook server and blocks until shutdown is triggered.
// It handles:
// - Certificate validation
// - Route setup
// - TLS configuration
// - Signal handling
// - Graceful shutdown
func (s *Server) Run() error {
	s.logger.Info().
		Str("address", s.config.Address).
		Str("cert_file", s.config.CertFile).
		Str("key_file", s.config.KeyFile).
		Msg("Starting webhook server")

	// Validate certificate paths
	if err := s.config.ValidateCertPaths(); err != nil {
		return fmt.Errorf("certificate validation failed: %v", err)
	}

	// Set up HTTP routes
	mux := http.NewServeMux()

	// Create middleware chain - tracing first, then metrics
	// This ensures spans are created before metrics are collected
	handleWithMiddleware := func(handler http.HandlerFunc) http.Handler {
		// First apply tracing, then metrics
		return s.tracingMiddleware(s.metrics.metricsMiddleware(handler))
	}

	// Apply middleware chain to handlers
	mux.Handle("/mutate", handleWithMiddleware(s.handleMutate))
	mux.Handle("/healthz", handleWithMiddleware(s.handleLiveness))
	mux.Handle("/readyz", handleWithMiddleware(s.handleReadiness))

	// Add metrics endpoint with only metrics middleware (no tracing)
	mux.Handle("/metrics", s.metrics.handler())

	// Initialize HTTP server with secure defaults
	s.serverMu.Lock()
	s.server = &http.Server{
		Addr:    s.config.Address,
		Handler: mux,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			CipherSuites: []uint16{
				tls.TLS_AES_128_GCM_SHA256,
				tls.TLS_AES_256_GCM_SHA384,
				tls.TLS_CHACHA20_POLY1305_SHA256,
			},
			CurvePreferences: []tls.CurveID{
				tls.X25519,
				tls.CurveP384,
			},
			SessionTicketsDisabled: true,
			Renegotiation:          tls.RenegotiateNever,
			InsecureSkipVerify:     false,
			ClientAuth:             tls.VerifyClientCertIfGiven,
		},
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}
	s.serverMu.Unlock()

	// Mark server as ready to receive requests
	s.health.markReady()
	s.metrics.updateHealthMetrics(true, true)

	// Create error channel for server errors
	serverError := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		if err := s.server.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile); err != http.ErrServerClosed {
			serverError <- err
		}
	}()

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Wait for shutdown signal or server error
	select {
	case err := <-serverError:
		return fmt.Errorf("server error: %v", err)
	case sig := <-stop:
		s.logger.Info().
			Str("signal", sig.String()).
			Msg("Received shutdown signal")
		return s.shutdown()
	}
}

// shutdown performs a graceful server shutdown.
// It:
// - Marks the server as not ready to prevent new requests
// - Updates health metrics
// - Waits for in-flight requests to complete
// - Shuts down the tracer provider
// - Enforces a timeout for shutdown completion
func (s *Server) shutdown() error {
	// Mark server as not ready
	s.health.ready.Store(false)
	s.metrics.updateHealthMetrics(false, true)

	s.logger.Info().
		Dur("timeout", s.gracefulTimeout).
		Msg("Shutting down server")

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), s.gracefulTimeout)
	defer cancel()

	// Get server reference under lock
	s.serverMu.RLock()
	server := s.server
	s.serverMu.RUnlock()

	// Shutdown server gracefully
	var shutdownErr error
	if err := server.Shutdown(ctx); err != nil {
		shutdownErr = fmt.Errorf("error during server shutdown: %v", err)
	}

	// Shutdown tracer if it's enabled
	if s.tracer != nil && s.tracer.enabled {
		s.logger.Debug().Msg("Shutting down tracer")
		if err := s.tracer.shutdown(ctx); err != nil {
			// Log error but continue shutdown
			s.logger.Error().Err(err).Msg("Error shutting down tracer")
			if shutdownErr == nil {
				shutdownErr = fmt.Errorf("error during tracer shutdown: %v", err)
			}
		}
	}

	s.logger.Info().Msg("Server shutdown completed")
	return shutdownErr
}

// GetAddr returns the server's current address in a thread-safe way.
// This is useful for testing and dynamic port assignment.
func (s *Server) GetAddr() (string, error) {
	s.serverMu.RLock()
	defer s.serverMu.RUnlock()
	if s.server == nil {
		return "", fmt.Errorf("server is not initialized")
	}
	return s.server.Addr, nil
}
