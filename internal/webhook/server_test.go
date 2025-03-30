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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jjshanks/add-pod-label/internal/config"
)

// portAllocator manages test port allocation to prevent conflicts
type portAllocator struct {
	mu         sync.Mutex
	usedPorts  map[int]struct{}
	basePort   int
	maxRetries int
}

// newPortAllocator creates a new portAllocator instance
func newPortAllocator() *portAllocator {
	return &portAllocator{
		usedPorts:  make(map[int]struct{}),
		basePort:   10000, // Start at a high port number
		maxRetries: 50,    // Maximum number of retries
	}
}

// getPort allocates an available port for testing
func (pa *portAllocator) getPort(t *testing.T) (int, func()) {
	t.Helper()

	pa.mu.Lock()
	defer pa.mu.Unlock()

	for retry := 0; retry < pa.maxRetries; retry++ {
		// Try to find an available port
		port := pa.findAvailablePort(t)
		if port == 0 {
			continue
		}

		// Verify the port is truly available by attempting to listen
		if listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port)); err == nil {
			listener.Close()
			pa.usedPorts[port] = struct{}{}

			// Return the port and a cleanup function
			cleanup := func() {
				pa.mu.Lock()
				delete(pa.usedPorts, port)
				pa.mu.Unlock()
			}

			return port, cleanup
		}
	}

	t.Fatal("Failed to allocate available port after maximum retries")
	return 0, nil
}

// findAvailablePort attempts to find an unused port
func (pa *portAllocator) findAvailablePort(t *testing.T) int {
	t.Helper()

	// Try to get a system-assigned port first
	if listener, err := net.Listen("tcp", "localhost:0"); err == nil {
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()

		// Verify port isn't in our used ports map
		if _, used := pa.usedPorts[port]; !used {
			// Wait a brief moment to ensure the port is truly released
			time.Sleep(10 * time.Millisecond)
			return port
		}
	}

	// Fall back to searching from base port
	for port := pa.basePort; port < 65535; port++ {
		if _, used := pa.usedPorts[port]; !used {
			return port
		}
	}

	return 0
}

// getAddr returns a formatted address string with the allocated port
func (pa *portAllocator) getAddr(t *testing.T) (string, func()) {
	t.Helper()
	port, cleanup := pa.getPort(t)
	return fmt.Sprintf("localhost:%d", port), cleanup
}

// Global port allocator instance for tests
var testPortAllocator = newPortAllocator()

// GetTestAddr returns an available address for testing
func GetTestAddr(t *testing.T) (string, func()) {
	return testPortAllocator.getAddr(t)
}

// AssertPortAvailable verifies that a port is actually available
func AssertPortAvailable(t *testing.T, port int) {
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	require.NoError(t, err, "Port %d should be available", port)
	listener.Close()
}

// testCertConfig holds basic configuration for test certificates
type testCertConfig struct {
	validFor time.Duration
	hosts    []string
}

// defaultTestCertConfig returns a basic cert config suitable for most tests
func defaultTestCertConfig() *testCertConfig {
	return &testCertConfig{
		validFor: 1 * time.Hour,
		hosts:    []string{"localhost", "127.0.0.1"},
	}
}

// generateTestCert creates a temporary certificate for testing
// It returns paths to the cert and key files, and a cleanup function
func generateTestCert(t *testing.T, cfg *testCertConfig) (certFile, keyFile string, cleanup func()) {
	t.Helper()

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "webhook-test-certs-")
	require.NoError(t, err, "failed to create temp directory")

	certFile = filepath.Join(tempDir, "tls.crt")
	keyFile = filepath.Join(tempDir, "tls.key")

	// Generate private key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "failed to generate private key")

	// Create certificate template with minimal required fields
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "webhook-test",
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(cfg.validFor),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add DNS names and IP addresses
	for _, host := range cfg.hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}

	// Create certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err, "failed to create certificate")

	// Write certificate file
	certOut, err := os.Create(certFile)
	require.NoError(t, err, "failed to create cert file")
	defer certOut.Close()

	err = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	require.NoError(t, err, "failed to encode certificate")

	// Write key file
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	require.NoError(t, err, "failed to create key file")
	defer keyOut.Close()

	keyBytes, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err, "failed to marshal private key")

	err = pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	require.NoError(t, err, "failed to encode private key")

	cleanup = func() {
		os.RemoveAll(tempDir)
	}

	return certFile, keyFile, cleanup
}

// setupTestServer creates a test server with appropriate configuration
// It can be used with either real TLS certificates or mocked TLS
func setupWebhookTestServer(t *testing.T, useMockTLS bool) (*Server, func()) {
	t.Helper()

	var certFile, keyFile string
	var cleanup func()

	if useMockTLS {
		// Use dummy values for cert paths when mocking TLS
		certFile = "mock-cert"
		keyFile = "mock-key"
		cleanup = func() {}
	} else {
		// Generate real certificates
		certFile, keyFile, cleanup = generateTestCert(t, defaultTestCertConfig())
	}

	addr, portCleanup := GetTestAddr(t)
	defer portCleanup()

	// Create test configuration
	cfg := &config.Config{
		Address:  addr,
		CertFile: certFile,
		KeyFile:  keyFile,
		LogLevel: "debug",
		Console:  true,
	}

	// Create new registry for isolated tests
	reg := prometheus.NewRegistry()

	// Create server
	srv, err := NewTestServer(cfg, reg)
	require.NoError(t, err, "failed to create test server")

	return srv, cleanup
}

// TestServerInitialization tests the basic server initialization
func TestServerInitialization(t *testing.T) {
	srv, cleanup := setupWebhookTestServer(t, false)
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
			srv, cleanup := setupWebhookTestServer(t, false)
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

	// Create disabled tracer for tests
	tr := &tracer{
		enabled: false,
	}

	// Create server instance
	srv := &Server{
		logger:          logger,
		config:          cfg,
		health:          newHealthState(realClock{}),
		metrics:         m,
		tracer:          tr,
		gracefulTimeout: 5 * time.Second,
		serverMu:        sync.RWMutex{},
	}

	// Set up the server manually for testing
	mux := http.NewServeMux()

	// Create middleware chain - matching real server (tracing first, then metrics)
	handleWithMiddleware := func(handler http.HandlerFunc) http.Handler {
		return srv.tracingMiddleware(srv.metrics.metricsMiddleware(handler))
	}

	// Apply middleware chain to handlers
	mux.Handle("/mutate", handleWithMiddleware(srv.handleMutate))
	mux.Handle("/healthz", handleWithMiddleware(srv.handleLiveness))
	mux.Handle("/readyz", handleWithMiddleware(srv.handleReadiness))

	// Add metrics endpoint with only metrics middleware (no tracing)
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
	srv, cleanup := setupWebhookTestServer(t, false)
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
				srv, cleanup := setupWebhookTestServer(t, false)
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

// Update in server_test.go
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
			// Use the new webhook test server setup
			srv, cleanup := setupWebhookTestServer(t, false)
			defer cleanup()

			// Start server
			errCh := make(chan error, 1)
			var wg sync.WaitGroup
			wg.Add(1)

			go func() {
				defer wg.Done()
				t.Log("Starting server...")
				errCh <- srv.Run()
			}()

			// Wait for server to be ready by checking health endpoint
			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
					},
				},
				Timeout: 5 * time.Second,
			}

			var addr string
			var err error
			// More robust server startup check
			startTime := time.Now()
			for time.Since(startTime) < 5*time.Second {
				addr, err = srv.GetAddr()
				if err != nil {
					time.Sleep(100 * time.Millisecond)
					continue
				}

				resp, healthErr := client.Get(fmt.Sprintf("https://%s/healthz", addr))
				if healthErr == nil {
					resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						break
					}
				}
				time.Sleep(100 * time.Millisecond)
			}
			require.NotEmpty(t, addr, "Failed to get server address")

			// Send shutdown signal
			t.Logf("Sending %s signal...", tc.name)
			p, processErr := os.FindProcess(os.Getpid())
			require.NoError(t, processErr)
			signalErr := p.Signal(tc.signal)
			require.NoError(t, signalErr)

			// Wait for shutdown
			select {
			case shutdownErr := <-errCh:
				assert.NoError(t, shutdownErr)
			case <-time.After(5 * time.Second):
				t.Fatal("Server shutdown timed out")
			}

			wg.Wait()

			// Verify server is no longer accepting connections
			_, healthErr := client.Get(fmt.Sprintf("https://%s/healthz", addr))
			assert.Error(t, healthErr)
		})
	}
}

func TestServerShutdownTimeout(t *testing.T) {
	// Create a temporary directory for test certificates
	tempDir, err := os.MkdirTemp("", "webhook-timeout-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	testCfg := defaultTestCertConfig()
	certFile, keyFile, cleanupCerts := generateTestCert(t, testCfg)
	defer cleanupCerts()

	// Create a server configuration with temp certificate paths
	cfg := &config.Config{
		Address:  "localhost:0", // Use port 0 to let the OS assign a free port
		CertFile: certFile,
		KeyFile:  keyFile,
		LogLevel: "debug",
		Console:  true,
	}

	// Create a new Prometheus registry for this test
	reg := prometheus.NewRegistry()

	// Create server instance
	srv, err := NewTestServer(cfg, reg)
	require.NoError(t, err)

	// Reduce the graceful timeout to test short timeout scenario
	srv.gracefulTimeout = 10 * time.Millisecond

	// Channels for synchronization
	serverStarted := make(chan struct{})
	serverStopped := make(chan error, 1)
	healthCheckDone := make(chan struct{})

	// Start server listener in a goroutine
	go func() {
		close(serverStarted)
		listenErr := srv.server.ListenAndServeTLS(srv.config.CertFile, srv.config.KeyFile)
		if listenErr != nil && listenErr != http.ErrServerClosed {
			serverStopped <- listenErr
		}
		close(serverStopped)
		close(healthCheckDone) // Signal that server has fully stopped
	}()

	// Wait for server to start
	<-serverStarted
	time.Sleep(50 * time.Millisecond)

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Measure shutdown time
	startShutdown := time.Now()
	shutdownErr := srv.server.Shutdown(ctx)
	shutdownDuration := time.Since(startShutdown)

	// Check shutdown results
	if shutdownErr != nil && shutdownErr != context.DeadlineExceeded {
		t.Errorf("Unexpected shutdown error: %v", shutdownErr)
	}

	t.Logf("Shutdown completed in %v", shutdownDuration)

	// Wait for server to stop or timeout
	select {
	case srvErr := <-serverStopped:
		// We expect either nil or ErrServerClosed
		if srvErr != nil && srvErr != http.ErrServerClosed {
			t.Errorf("Unexpected server error: %v", srvErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not shut down in time")
	}

	// Wait for health check to be safe
	<-healthCheckDone

	// Now that the server has fully stopped, safely check the health state
	assert.False(t, srv.health.isReady(), "Server should not be ready after shutdown")
}
