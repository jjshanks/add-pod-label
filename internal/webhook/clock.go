package webhook

import "time"

// Clock is an interface that wraps the time-based methods we need
type Clock interface {
	Now() time.Time
}

// realClock is a Clock that uses the actual system time
type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

// mockClock is a Clock that can be manually controlled for testing
type mockClock struct {
	now time.Time
}

func newMockClock(t time.Time) *mockClock {
	return &mockClock{now: t}
}

func (m *mockClock) Now() time.Time {
	return m.now
}

func (m *mockClock) Add(d time.Duration) {
	m.now = m.now.Add(d)
}

func (m *mockClock) Set(t time.Time) {
	m.now = t
}
