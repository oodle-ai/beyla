package clock

import (
	"time"
)

type realClock struct {
}

var _ Clock = (*realClock)(nil)

func NewRealClock() Clock {
	return &realClock{}
}

func (r *realClock) Now() time.Time {
	return time.Now()
}

func (r *realClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

func (r *realClock) Sleep(d time.Duration) {
	time.Sleep(d)
}
