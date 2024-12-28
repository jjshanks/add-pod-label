package webhook

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// livenessTimeout is the duration after which the liveness probe will fail
	livenessTimeout = 60 * time.Second
)

// healthState maintains the server's health status
type healthState struct {
	ready       atomic.Bool  // Server is ready to handle requests
	lastChecked atomic.Int64 // Unix timestamp of last successful health check
	clock       Clock        // Clock interface for time operations
}

// newHealthState creates a new healthState instance
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

// markReady marks the server as ready to handle requests
func (h *healthState) markReady() {
	h.ready.Store(true)
}

// isReady returns true if the server is ready to handle requests
func (h *healthState) isReady() bool {
	return h.ready.Load()
}

// updateLastChecked updates the timestamp of the last successful health check
func (h *healthState) updateLastChecked() {
	h.lastChecked.Store(h.clock.Now().Unix())
}

// timeSinceLastCheck returns the duration since the last successful health check
func (h *healthState) timeSinceLastCheck() time.Duration {
	lastCheck := h.lastChecked.Load()
	return h.clock.Now().Sub(time.Unix(lastCheck, 0))
}

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	timeSinceLastCheck := s.health.timeSinceLastCheck()

	// Check if too much time has passed since last successful health check
	if timeSinceLastCheck > livenessTimeout {
		log.Error().
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

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if !s.health.isReady() {
		log.Warn().Msg("Readiness check failed: server not ready")
		http.Error(w, "Server not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
