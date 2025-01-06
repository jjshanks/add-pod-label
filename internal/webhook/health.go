// Package webhook provides functionality for webhook operations.
// This file implements health checking functionality including
// readiness and liveness probes for Kubernetes health monitoring.
package webhook

import (
	"net/http"
	"sync/atomic"
	"time"
)

const (
	// livenessTimeout defines the duration after which the liveness probe will fail
	// if no successful health check has occurred. After this duration, the server
	// is considered unresponsive and Kubernetes may restart the pod.
	livenessTimeout = 60 * time.Second
)

// healthState maintains the server's health status using atomic operations
// for thread-safe access. It tracks both readiness and liveness state.
type healthState struct {
	ready       atomic.Bool  // Indicates if server is ready to handle requests
	lastChecked atomic.Int64 // Unix timestamp of last successful health check
	clock       Clock        // Interface for time operations (enables testing)
}

// newHealthState creates a new healthState instance with the provided clock.
// If no clock is provided, it uses the real system clock.
// The initial state is not ready, but the last check time is set to creation time.
func newHealthState(clock Clock) *healthState {
	if clock == nil {
		clock = realClock{}
	}
	hs := &healthState{
		clock: clock,
	}
	hs.lastChecked.Store(clock.Now().Unix())
	return hs
}

// markReady marks the server as ready to handle requests.
// This is called once the server has completed initialization
// and is prepared to process webhook requests.
func (h *healthState) markReady() {
	h.ready.Store(true)
}

// isReady returns true if the server is ready to handle requests.
// This is used by the readiness probe to determine if the server
// should receive traffic.
func (h *healthState) isReady() bool {
	return h.ready.Load()
}

// updateLastChecked updates the timestamp of the last successful health check
// to the current time. This is called after successful health checks to
// indicate the server is still responsive.
func (h *healthState) updateLastChecked() {
	h.lastChecked.Store(h.clock.Now().Unix())
}

// timeSinceLastCheck returns the duration since the last successful health check.
// This is used to determine if the server has become unresponsive and
// should fail liveness checks.
func (h *healthState) timeSinceLastCheck() time.Duration {
	lastCheck := h.lastChecked.Load()
	return h.clock.Now().Sub(time.Unix(lastCheck, 0))
}

// handleLiveness is the HTTP handler for the /healthz endpoint.
// It verifies that the server is responsive by checking:
// - The time since the last successful health check is within the timeout
// - The server can successfully complete basic operations
//
// It returns:
// - 200 OK if the server is alive and responsive
// - 503 Service Unavailable if the server is unresponsive
func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	timeSinceLastCheck := s.health.timeSinceLastCheck()
	isAlive := timeSinceLastCheck <= livenessTimeout

	// Update metrics for monitoring
	s.metrics.updateHealthMetrics(s.health.isReady(), isAlive)

	// Check if too much time has passed since last successful health check
	if !isAlive {
		s.logger.Error().
			Dur("time_since_last_check", timeSinceLastCheck).
			Dur("timeout", livenessTimeout).
			Msg("Liveness check failed: server unresponsive")
		http.Error(w, "Server unresponsive", http.StatusServiceUnavailable)
		return
	}

	// Only update the last check time if we're responding successfully
	s.health.updateLastChecked()
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// handleReadiness is the HTTP handler for the /readyz endpoint.
// It verifies that the server is prepared to handle requests by checking:
// - The server has completed initialization
// - The server is marked as ready
//
// It returns:
// - 200 OK if the server is ready to handle requests
// - 503 Service Unavailable if the server is not ready
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	isReady := s.health.isReady()
	isAlive := s.health.timeSinceLastCheck() <= livenessTimeout

	// Update metrics for monitoring
	s.metrics.updateHealthMetrics(isReady, isAlive)

	if !isReady {
		s.logger.Warn().Msg("Readiness check failed: server not ready")
		http.Error(w, "Server not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
