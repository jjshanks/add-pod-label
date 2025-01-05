// Package webhook provides functionality for webhook operations.
// This file defines the Clock interface and its implementations for time-based operations.
package webhook

import "time"

// Clock is an interface that wraps time-based methods.
// It enables testing of time-dependent code by allowing time manipulation
// in tests while using the system clock in production.
type Clock interface {
	// Now returns the current time. In production this is the system time,
	// but in tests it can be controlled for deterministic behavior.
	Now() time.Time
}

// realClock implements Clock using the actual system time.
// This is the production implementation used by default.
type realClock struct{}

// Now returns the current system time using time.Now()
func (realClock) Now() time.Time {
	return time.Now()
}

// mockClock implements Clock for testing purposes.
// It allows precise control over time in tests by manually
// setting and advancing the current time.
type mockClock struct {
	now time.Time // The current mocked time
}

// newMockClock creates a new mockClock instance initialized to the given time.
// This is used in tests to start with a known time state.
func newMockClock(t time.Time) *mockClock {
	return &mockClock{now: t}
}

// Now returns the current mocked time
func (m *mockClock) Now() time.Time {
	return m.now
}

// Add advances the mock clock by the specified duration.
// This allows tests to simulate the passage of time in a controlled way.
func (m *mockClock) Add(d time.Duration) {
	m.now = m.now.Add(d)
}

// Set directly sets the mock clock to a specific time.
// This allows tests to jump to exact points in time without
// calculating durations.
func (m *mockClock) Set(t time.Time) {
	m.now = t
}
