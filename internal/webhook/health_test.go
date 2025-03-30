package webhook

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jjshanks/add-pod-label/internal/config"
)

func TestHealthState(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		fn   func(*testing.T)
	}{
		{
			name: "new health state starts not ready",
			fn: func(t *testing.T) {
				clock := newMockClock(baseTime)
				hs := newHealthState(clock)
				assert.False(t, hs.isReady())
			},
		},
		{
			name: "mark ready changes state",
			fn: func(t *testing.T) {
				clock := newMockClock(baseTime)
				hs := newHealthState(clock)
				hs.markReady()
				assert.True(t, hs.isReady())
			},
		},
		{
			name: "last check time updates",
			fn: func(t *testing.T) {
				clock := newMockClock(baseTime)
				hs := newHealthState(clock)

				// Move clock forward 1 minute
				clock.Add(time.Minute)
				initialDuration := hs.timeSinceLastCheck()
				assert.Equal(t, time.Minute, initialDuration)

				// Update last check and verify duration is reset
				hs.updateLastChecked()
				updatedDuration := hs.timeSinceLastCheck()
				assert.Zero(t, updatedDuration)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.fn)
	}
}

func setupTestServer(t *testing.T, clock Clock) *TestServer {
	t.Helper()

	// Create a temporary directory for test certificates
	tempDir, err := os.MkdirTemp("", "webhook-test-")
	require.NoError(t, err)

	testCfg := defaultTestCertConfig()
	certFile, keyFile, cleanupCerts := generateTestCert(t, testCfg)
	defer cleanupCerts()

	addr, portCleanup := GetTestAddr(t)
	defer portCleanup()

	// Create test configuration with temp certificate paths and random port
	cfg := &config.Config{
		Address:  addr,
		CertFile: certFile,
		KeyFile:  keyFile,
		LogLevel: "debug",
		Console:  true,
	}

	// Create base logger
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Create a new Prometheus registry for this test
	reg := prometheus.NewRegistry()

	// Create a new test server
	srv, err := NewTestServer(cfg, reg)
	require.NoError(t, err)
	srv.logger = logger

	// Replace the health state with one using our test clock if provided
	if clock != nil {
		srv.health = newHealthState(clock)
	}

	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return &TestServer{
		Server:  srv,
		addr:    cfg.Address,
		cleanup: cleanup,
	}
}

func TestHealthEndpoints(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		endpoint        string
		method          string
		setupFn         func(*Server, *mockClock)
		expectedStatus  int
		expectedBody    string
		expectedHeaders map[string]string
	}{
		{
			name:     "liveness probe succeeds",
			endpoint: "/healthz",
			method:   http.MethodGet,
			setupFn: func(s *Server, clock *mockClock) {
				s.health.updateLastChecked()
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		// ... rest of the test cases remain the same ...
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := newMockClock(baseTime)
			ts := setupTestServer(t, clock)
			defer ts.cleanup()

			if tt.setupFn != nil {
				tt.setupFn(ts.Server, clock)
			}

			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tt.endpoint, nil)
			w := httptest.NewRecorder()

			switch tt.endpoint {
			case "/healthz":
				ts.handleLiveness(w, req)
			case "/readyz":
				ts.handleReadiness(w, req)
			}

			assert.Equal(t, tt.expectedStatus, w.Code)
			assert.Equal(t, tt.expectedBody, w.Body.String())

			if tt.expectedHeaders != nil {
				for k, v := range tt.expectedHeaders {
					assert.Equal(t, v, w.Header().Get(k))
				}
			}
		})
	}
}
