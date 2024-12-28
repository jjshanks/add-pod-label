package webhook

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jjshanks/pod-label-webhook/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

type Server struct {
	logger  zerolog.Logger
	config  *config.Config
	health  *healthState
	metrics *metrics
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

	// Initialize metrics with default registry
	m, err := initMetrics(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	return &Server{
		logger:  logger,
		config:  cfg,
		health:  newHealthState(realClock{}),
		metrics: m,
	}, nil
}

func NewTestServer(cfg *config.Config, reg prometheus.Registerer) (*Server, error) {
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

	// Initialize metrics with provided registry
	m, err := initMetrics(reg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	return &Server{
		logger:  logger,
		config:  cfg,
		health:  newHealthState(realClock{}),
		metrics: m,
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

	// Mark the server as ready after all handlers are set up
	s.health.markReady()
	s.metrics.updateHealthMetrics(true, true)

	server := &http.Server{
		Addr:              s.config.Address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       120 * time.Second,
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
	}

	return server.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
}
