package clock

import "time"

// Clock is an interface for getting the current time.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// Since returns the time elapsed since the given time.
	Since(t time.Time) time.Duration
	// Sleep pauses the current goroutine for at least the duration d.
	Sleep(d time.Duration)
}
