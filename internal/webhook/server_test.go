package webhook

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jjshanks/pod-label-webhook/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTest creates a test server with temporary certificates and a custom registry
func setupTest(t *testing.T) (*Server, func()) {
	t.Helper()

	// Create a temporary directory for test certificates
	tempDir, err := os.MkdirTemp("", "webhook-test-")
	require.NoError(t, err)

	// Create test certificate files
	certFile := filepath.Join(tempDir, "tls.crt")
	keyFile := filepath.Join(tempDir, "tls.key")

	// Generate self-signed certificate
	err = generateSelfSignedCert(certFile, keyFile)
	require.NoError(t, err)

	// Create a test configuration with temp certificate paths
	cfg := &config.Config{
		Address:  "localhost:0", // Use port 0 to let the OS assign a free port
		CertFile: certFile,
		KeyFile:  keyFile,
		LogLevel: "debug",
		Console:  true,
	}

	// Capture logs in a buffer for testing
	var logBuffer strings.Builder
	logger := zerolog.New(&logBuffer).With().Timestamp().Logger()

	// Create a new Prometheus registry for this test
	reg := prometheus.NewRegistry()

	// Create a new test server with custom logger
	srv, err := NewTestServer(cfg, reg)
	require.NoError(t, err)
	srv.logger = logger

	// Create a cleanup function
	cleanup := func() {
		// Remove temporary certificate files and directory
		os.RemoveAll(tempDir)
	}

	return srv, cleanup
}

// generateSelfSignedCert creates a self-signed certificate for testing
func generateSelfSignedCert(certFile, keyFile string) error {
	// Create a private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create a self-signed certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	// Create the self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write private key
	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	err = os.WriteFile(keyFile, keyPEM, 0600)
	if err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	// Write certificate
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	err = os.WriteFile(certFile, certPEM, 0644)
	if err != nil {
		return fmt.Errorf("failed to write cert file: %w", err)
	}

	return nil
}

// TestServerInitialization tests the basic server initialization
func TestServerInitialization(t *testing.T) {
	srv, cleanup := setupTest(t)
	defer cleanup()

	// Check that server is not nil
	assert.NotNil(t, srv)

	// Verify logger is set
	assert.NotNil(t, srv.logger)

	// Verify configuration is set
	assert.NotNil(t, srv.config)

	// Verify metrics are initialized
	assert.NotNil(t, srv.metrics)

	// Verify health state is initialized
	assert.NotNil(t, srv.health)
}

// TestServerHealthEndpoints tests the health check endpoints
func TestServerHealthEndpoints(t *testing.T) {
	testCases := []struct {
		name           string
		endpoint       string
		setupFunc      func(*Server)
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Liveness Probe Success",
			endpoint:       "/healthz",
			setupFunc:      func(srv *Server) { srv.health.updateLastChecked() },
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "Readiness Probe Success",
			endpoint:       "/readyz",
			setupFunc:      func(srv *Server) { srv.health.markReady() },
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "Readiness Probe Not Ready",
			endpoint:       "/readyz",
			setupFunc:      func(srv *Server) {}, // Do nothing, server starts not ready
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   "Server not ready\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			srv, cleanup := setupTest(t)
			defer cleanup()

			// Setup specific test conditions
			if tc.setupFunc != nil {
				tc.setupFunc(srv)
			}

			// Create a request to the health endpoint
			req := httptest.NewRequest(http.MethodGet, tc.endpoint, nil)
			w := httptest.NewRecorder()

			// Handle the request based on the endpoint
			switch tc.endpoint {
			case "/healthz":
				srv.handleLiveness(w, req)
			case "/readyz":
				srv.handleReadiness(w, req)
			}

			// Check response
			resp := w.Result()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.expectedStatus, resp.StatusCode)
			assert.Equal(t, tc.expectedBody, string(body))
		})
	}
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

	// Create server instance
	srv := &Server{
		logger:          logger,
		config:          cfg,
		health:          newHealthState(realClock{}),
		metrics:         m,
		gracefulTimeout: 5 * time.Second,
		serverMu:        sync.RWMutex{},
	}

	// Set up the server manually for testing
	mux := http.NewServeMux()

	// Wrap handlers with metrics middleware
	mux.Handle("/mutate", srv.metrics.metricsMiddleware(http.HandlerFunc(srv.handleMutate)))
	mux.Handle("/healthz", srv.metrics.metricsMiddleware(http.HandlerFunc(srv.handleLiveness)))
	mux.Handle("/readyz", srv.metrics.metricsMiddleware(http.HandlerFunc(srv.handleReadiness)))

	// Add metrics endpoint
	mux.Handle("/metrics", srv.metrics.handler())

	// Initialize HTTP server with secure defaults
	srv.server = &http.Server{
		Addr:    cfg.Address,
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

	return srv, nil
}

// TestServerShutdown tests the server shutdown process
func TestServerShutdown(t *testing.T) {
	// Create the server
	srv, cleanup := setupTest(t)
	defer cleanup()

	// Channels for synchronization
	serverStopped := make(chan error, 1)

	// Start server listener in a goroutine
	go func() {
		t.Logf("Starting server listener")
		err := srv.server.ListenAndServeTLS(srv.config.CertFile, srv.config.KeyFile)
		if err != nil && err != http.ErrServerClosed {
			serverStopped <- err
		}
		close(serverStopped)
	}()

	// Wait a moment for the server to start listening
	time.Sleep(500 * time.Millisecond)

	// Perform shutdown
	t.Logf("Initiating shutdown")
	startShutdown := time.Now()

	// Create context for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Shutdown the server
	err := srv.server.Shutdown(ctx)
	shutdownDuration := time.Since(startShutdown)

	t.Logf("Shutdown completed in %v", shutdownDuration)

	// Assert no shutdown errors
	assert.NoError(t, err)

	// Check that the server is no longer ready
	assert.False(t, srv.health.isReady())

	// Wait for server to stop or timeout
	select {
	case srvErr := <-serverStopped:
		// We expect either nil or ErrServerClosed
		if srvErr != nil && srvErr != http.ErrServerClosed {
			t.Errorf("Unexpected server error: %v", srvErr)
		}
	case <-ctx.Done():
		t.Fatal("Server did not shut down in time")
	}
}

// TestGetAddr tests the GetAddr method
func TestGetAddr(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() *Server
		wantErr bool
	}{
		{
			name: "server not initialized",
			setup: func() *Server {
				return &Server{
					logger:   zerolog.New(io.Discard),
					config:   &config.Config{},
					health:   newHealthState(realClock{}),
					serverMu: sync.RWMutex{},
				}
			},
			wantErr: true,
		},
		{
			name: "server initialized",
			setup: func() *Server {
				srv, cleanup := setupTest(t)
				t.Cleanup(cleanup)
				return srv
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setup()

			addr, err := srv.GetAddr()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, addr)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, addr)
				assert.True(t,
					strings.HasPrefix(addr, "localhost:") ||
						strings.HasPrefix(addr, "0.0.0.0:"),
					"Unexpected address format",
				)
			}
		})
	}
}

func TestServerShutdownSignals(t *testing.T) {
	testCases := []struct {
		name   string
		signal os.Signal
	}{
		{"SIGTERM", syscall.SIGTERM},
		{"SIGINT", syscall.SIGINT},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			srv := setupTestServer(t, nil) // Pass nil for clock since this test doesn't need time control
			defer srv.cleanup()

			// Start server
			errCh := make(chan error, 1)
			var wg sync.WaitGroup
			wg.Add(1)

			go func() {
				defer wg.Done()
				t.Log("Starting server...")
				errCh <- srv.Run()
			}()

			// Give the server a moment to start
			time.Sleep(250 * time.Millisecond)

			// Test health endpoint with retries
			t.Log("Testing health endpoint...")
			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
					},
				},
				Timeout: 5 * time.Second,
			}

			var addr string
			var resp *http.Response
			var err error
			for i := 0; i < 10; i++ {
				addr, err = srv.GetAddr()
				if err != nil {
					t.Logf("Failed to get server address on attempt %d: %v", i+1, err)
					time.Sleep(200 * time.Millisecond)
					continue
				}

				resp, err = client.Get(fmt.Sprintf("https://%s/healthz", addr))
				if err != nil {
					t.Logf("Health check attempt %d failed: %v", i+1, err)
					time.Sleep(200 * time.Millisecond)
					continue
				}

				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					break
				}

				t.Logf("Health check returned status %d on attempt %d", resp.StatusCode, i+1)
				time.Sleep(200 * time.Millisecond)
			}
			require.NoError(t, err, "Failed to get successful health check response")
			require.Equal(t, http.StatusOK, resp.StatusCode)

			t.Logf("Server successfully started and verified at %s", addr)

			// Send shutdown signal
			t.Logf("Sending %s signal...", tc.name)
			p, err := os.FindProcess(os.Getpid())
			require.NoError(t, err)
			err = p.Signal(tc.signal)
			require.NoError(t, err)

			// Wait for shutdown
			select {
			case shutdownErr := <-errCh:
				assert.NoError(t, shutdownErr)
				t.Log("Server shutdown completed")
			case <-time.After(5 * time.Second):
				t.Fatal("Server shutdown timed out")
			}

			// Allow time for server to fully stop
			time.Sleep(200 * time.Millisecond)

			// Verify server is no longer accepting connections
			t.Log("Verifying server is no longer accepting connections...")
			_, err = client.Get(fmt.Sprintf("https://%s/healthz", addr))
			assert.Error(t, err)

			wg.Wait()
		})
	}
}

func waitForServer(t *testing.T, srv *Server) string {
	t.Helper()

	ready := make(chan string)
	timeout := time.After(10 * time.Second)

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				addr, err := srv.GetAddr()
				if err == nil && addr != "" && addr != "localhost:0" && srv.health.isReady() {
					ready <- addr
					return
				}
			case <-timeout:
				return
			}
		}
	}()

	select {
	case addr := <-ready:
		t.Logf("Server ready at address: %s", addr)
		return addr
	case <-timeout:
		addr, err := srv.GetAddr()
		t.Fatalf("Timeout waiting for server to be ready. Address: %v (err: %v), Ready state: %v",
			addr, err, srv.health.isReady())
		return ""
	}
}
