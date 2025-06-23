package clock

import (
	"sync"
	"time"
)

type FakeClock struct {
	sync.Mutex
	currentTime time.Time
}

var _ Clock = (*FakeClock)(nil)

func NewFakeClock() *FakeClock {
	return &FakeClock{
		currentTime: time.Now(),
	}
}

func (f *FakeClock) Now() time.Time {
	f.Lock()
	defer f.Unlock()
	return f.currentTime
}

func (f *FakeClock) SetTime(t time.Time) {
	f.Lock()
	defer f.Unlock()
	f.currentTime = t
}

func (f *FakeClock) Advance(d time.Duration) {
	f.Lock()
	defer f.Unlock()
	f.currentTime = f.currentTime.Add(d)
}

func (f *FakeClock) Since(t time.Time) time.Duration {
	f.Lock()
	defer f.Unlock()
	return f.currentTime.Sub(t)
}

func (f *FakeClock) Sleep(d time.Duration) {
	f.Lock()
	defer f.Unlock()
	f.currentTime = f.currentTime.Add(d)
}
