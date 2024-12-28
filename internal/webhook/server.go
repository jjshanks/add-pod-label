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

	"github.com/jjshanks/pod-label-webhook/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

type Server struct {
	logger          zerolog.Logger
	config          *config.Config
	health          *healthState
	metrics         *metrics
	server          *http.Server
	gracefulTimeout time.Duration
	serverMu        sync.RWMutex // Protects server field
}

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

	// Initialize metrics with new registry if none provided
	reg := prometheus.NewRegistry()
	m, err := initMetrics(reg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	return &Server{
		logger:          logger,
		config:          cfg,
		health:          newHealthState(realClock{}),
		metrics:         m,
		gracefulTimeout: 30 * time.Second,
		serverMu:        sync.RWMutex{},
	}, nil
}

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

	mux := http.NewServeMux()

	// Wrap handlers with metrics middleware
	mux.Handle("/mutate", s.metrics.metricsMiddleware(http.HandlerFunc(s.handleMutate)))
	mux.Handle("/healthz", s.metrics.metricsMiddleware(http.HandlerFunc(s.handleLiveness)))
	mux.Handle("/readyz", s.metrics.metricsMiddleware(http.HandlerFunc(s.handleReadiness)))

	// Add metrics endpoint
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
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	s.serverMu.Unlock()

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

	// Set up signal handling
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

// GetAddr returns the server's current address in a thread-safe way
func (s *Server) GetAddr() string {
	s.serverMu.RLock()
	defer s.serverMu.RUnlock()
	if s.server == nil {
		return s.config.Address
	}
	return s.server.Addr
}

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

	// Shutdown server gracefully
	s.serverMu.RLock()
	if err := s.server.Shutdown(ctx); err != nil {
		s.serverMu.RUnlock()
		return fmt.Errorf("error during server shutdown: %v", err)
	}
	s.serverMu.RUnlock()

	s.logger.Info().Msg("Server shutdown completed")
	return nil
}
