package webhook

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jjshanks/pod-label-webhook/internal/config"
	"github.com/rs/zerolog"
)

type Server struct {
	logger zerolog.Logger
	config *config.Config
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

	return &Server{
		logger: logger,
		config: cfg,
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
	mux.HandleFunc("/mutate", s.handleMutate)

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

	s.logger.Info().
		Str("address", s.config.Address).
		Str("cert_file", s.config.CertFile).
		Str("key_file", s.config.KeyFile).
		Msg("Starting webhook server")

	return server.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
}

func (s *Server) validateCertPaths(certFile, keyFile string) error {
	s.logger.Debug().Msg("Validating certificate paths")

	// Validate certificate file
	certInfo, err := os.Stat(certFile)
	if err != nil {
		s.logger.Error().Err(err).Msg("Certificate validation failed")
		return fmt.Errorf("certificate file error: %v", err)
	}
	if !certInfo.Mode().IsRegular() {
		s.logger.Error().Msg("Certificate validation failed")
		return fmt.Errorf("certificate path is not a regular file")
	}

	// Validate key file
	keyInfo, err := os.Stat(keyFile)
	if err != nil {
		s.logger.Error().Err(err).Msg("Certificate validation failed")
		return fmt.Errorf("key file error: %v", err)
	}
	if !keyInfo.Mode().IsRegular() {
		s.logger.Error().Msg("Certificate validation failed")
		return fmt.Errorf("key path is not a regular file")
	}

	// Check key file permissions
	keyMode := keyInfo.Mode()
	if keyMode.Perm()&0o077 != 0 {
		s.logger.Error().
			Str("key_file", keyFile).
			Str("mode", keyMode.String()).
			Msg("Key file has excessive permissions")
		s.logger.Error().Msg("Certificate validation failed")
		return fmt.Errorf("key file %s has excessive permissions %v, expected 0600 or more restrictive",
			keyFile, keyMode.Perm())
	}
	if keyMode.Perm() > 0o600 {
		s.logger.Warn().
			Str("key_file", keyFile).
			Str("mode", keyMode.String()).
			Msg("key file has permissive mode %v, recommend 0600")
	}

	// Validate parent directories
	certDir := filepath.Dir(certFile)
	keyDir := filepath.Dir(keyFile)

	certDirInfo, err := os.Stat(certDir)
	if err != nil {
		s.logger.Error().Err(err).Msg("Certificate validation failed")
		return fmt.Errorf("certificate directory error: %v", err)
	}
	if !certDirInfo.IsDir() {
		s.logger.Error().Msg("Certificate validation failed")
		return fmt.Errorf("certificate parent path is not a directory")
	}

	keyDirInfo, err := os.Stat(keyDir)
	if err != nil {
		s.logger.Error().Err(err).Msg("Certificate validation failed")
		return fmt.Errorf("key directory error: %v", err)
	}
	if !keyDirInfo.IsDir() {
		s.logger.Error().Msg("Certificate validation failed")
		return fmt.Errorf("key parent path is not a directory")
	}

	s.logger.Debug().Msg("Certificate paths validated successfully")
	return nil
}
