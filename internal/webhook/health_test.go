package webhook

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jjshanks/pod-label-webhook/internal/config"
	"github.com/stretchr/testify/assert"
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

func setupTestServer(t *testing.T, clock Clock) *Server {
	cfg := &config.Config{
		Address:  "localhost:8443",
		CertFile: "/tmp/cert",
		KeyFile:  "/tmp/key",
		LogLevel: "debug",
	}

	srv, err := NewServer(cfg)
	assert.NoError(t, err)
	assert.NotNil(t, srv)

	// Replace the health state with one using our test clock
	srv.health = newHealthState(clock)
	return srv
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
		{
			name:           "post to liveness returns method not allowed",
			endpoint:       "/healthz",
			method:         http.MethodPost,
			setupFn:        nil,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method not allowed\n",
			expectedHeaders: map[string]string{
				"Allow": http.MethodGet,
			},
		},
		{
			name:           "put to liveness returns method not allowed",
			endpoint:       "/healthz",
			method:         http.MethodPut,
			setupFn:        nil,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method not allowed\n",
			expectedHeaders: map[string]string{
				"Allow": http.MethodGet,
			},
		},
		{
			name:           "delete to liveness returns method not allowed",
			endpoint:       "/healthz",
			method:         http.MethodDelete,
			setupFn:        nil,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method not allowed\n",
			expectedHeaders: map[string]string{
				"Allow": http.MethodGet,
			},
		},
		{
			name:     "liveness probe fails due to timeout",
			endpoint: "/healthz",
			setupFn: func(s *Server, clock *mockClock) {
				clock.Add(65 * time.Second)
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   "Server unresponsive\n",
		},
		{
			name:     "readiness probe succeeds",
			endpoint: "/readyz",
			method:   http.MethodGet,
			setupFn: func(s *Server, clock *mockClock) {
				s.health.markReady()
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "post to readiness returns method not allowed",
			endpoint:       "/readyz",
			method:         http.MethodPost,
			setupFn:        nil,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method not allowed\n",
			expectedHeaders: map[string]string{
				"Allow": http.MethodGet,
			},
		},
		{
			name:           "put to readiness returns method not allowed",
			endpoint:       "/readyz",
			method:         http.MethodPut,
			setupFn:        nil,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method not allowed\n",
			expectedHeaders: map[string]string{
				"Allow": http.MethodGet,
			},
		},
		{
			name:           "delete to readiness returns method not allowed",
			endpoint:       "/readyz",
			method:         http.MethodDelete,
			setupFn:        nil,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method not allowed\n",
			expectedHeaders: map[string]string{
				"Allow": http.MethodGet,
			},
		},
		{
			name:           "readiness probe fails when not ready",
			endpoint:       "/readyz",
			setupFn:        func(s *Server, clock *mockClock) {}, // Do nothing, server starts not ready
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   "Server not ready\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := newMockClock(baseTime)
			srv := setupTestServer(t, clock)

			if tt.setupFn != nil {
				tt.setupFn(srv, clock)
			}

			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tt.endpoint, nil)
			w := httptest.NewRecorder()

			switch tt.endpoint {
			case "/healthz":
				srv.handleLiveness(w, req)
			case "/readyz":
				srv.handleReadiness(w, req)
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
