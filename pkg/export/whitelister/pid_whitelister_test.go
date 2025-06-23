package whitelister

import (
	"sync"
	"testing"
	"time"

	"github.com/grafana/beyla/v2/pkg/clock"
	"github.com/grafana/beyla/v2/pkg/internal/request"
	"github.com/stretchr/testify/assert"
)

func TestNewPIDWhitelister(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	assert.NotNil(t, whitelister)
	assert.Equal(t, *pidWhitelisterIgnorePIDDuration, whitelister.ignoreDuration)
	assert.Equal(t, *pidWhitelisterExpirePIDDuration, whitelister.expireDuration)
	assert.NotNil(t, whitelister.pids)
	assert.Equal(t, fakeClock, whitelister.cl)
}

func TestPIDWhitelister_IsWhitelisted_FirstTime(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	pid := request.PidInfo{HostPID: 12345}

	// First time seeing the PID should return false (still in ignore period)
	result := whitelister.IsWhitelisted(pid)
	assert.False(t, result)

	// Verify the PID was added to the map
	info, exists := whitelister.pids.Load(pid)
	assert.True(t, exists)
	assert.Equal(t, fakeClock.Now(), info.FirstSeen)
	assert.Equal(t, fakeClock.Now(), info.LastSeen)
}

func TestPIDWhitelister_IsWhitelisted_AfterIgnoreDuration(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	pid := request.PidInfo{HostPID: 12345}

	// First time seeing the PID
	result := whitelister.IsWhitelisted(pid)
	assert.False(t, result)

	// Advance time past the ignore duration
	fakeClock.Advance(*pidWhitelisterIgnorePIDDuration + time.Second)

	// Now it should be whitelisted
	result = whitelister.IsWhitelisted(pid)
	assert.True(t, result)

	// Verify LastSeen was updated
	info, exists := whitelister.pids.Load(pid)
	assert.True(t, exists)
	assert.Equal(t, fakeClock.Now(), info.LastSeen)
}

func TestPIDWhitelister_IsWhitelisted_JustBeforeIgnoreDuration(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	pid := request.PidInfo{HostPID: 12345}

	// First time seeing the PID
	result := whitelister.IsWhitelisted(pid)
	assert.False(t, result)

	// Advance time just before the ignore duration
	fakeClock.Advance(*pidWhitelisterIgnorePIDDuration - time.Second)

	// Should still not be whitelisted
	result = whitelister.IsWhitelisted(pid)
	assert.False(t, result)
}

func TestPIDWhitelister_IsWhitelisted_MultiplePIDs(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	pid1 := request.PidInfo{HostPID: 12345}
	pid2 := request.PidInfo{HostPID: 67890}

	// First time seeing both PIDs
	assert.False(t, whitelister.IsWhitelisted(pid1))
	assert.False(t, whitelister.IsWhitelisted(pid2))

	// Advance time past ignore duration
	fakeClock.Advance(*pidWhitelisterIgnorePIDDuration + time.Second)

	// Both should now be whitelisted
	assert.True(t, whitelister.IsWhitelisted(pid1))
	assert.True(t, whitelister.IsWhitelisted(pid2))

	// Verify both PIDs are in the map
	_, exists1 := whitelister.pids.Load(pid1)
	_, exists2 := whitelister.pids.Load(pid2)
	assert.True(t, exists1)
	assert.True(t, exists2)
}

func TestPIDWhitelister_IsWhitelisted_UpdateLastSeen(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	pid := request.PidInfo{HostPID: 12345}

	// First time seeing the PID
	assert.False(t, whitelister.IsWhitelisted(pid))

	// Advance time past ignore duration
	fakeClock.Advance(*pidWhitelisterIgnorePIDDuration + time.Second)

	// Now it should be whitelisted
	assert.True(t, whitelister.IsWhitelisted(pid))

	// Get the initial LastSeen
	info, _ := whitelister.pids.Load(pid)
	initialLastSeen := info.LastSeen

	// Advance time and check again
	fakeClock.Advance(time.Minute)
	assert.True(t, whitelister.IsWhitelisted(pid))

	// Verify LastSeen was updated
	info, _ = whitelister.pids.Load(pid)
	assert.True(t, info.LastSeen.After(initialLastSeen))
}

func TestPIDWhitelister_CleanupExpiredPIDs(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	pid1 := request.PidInfo{HostPID: 12345}
	pid2 := request.PidInfo{HostPID: 67890}

	// Add both PIDs
	whitelister.IsWhitelisted(pid1)
	whitelister.IsWhitelisted(pid2)

	// Verify both are in the map
	assert.Equal(t, 2, whitelister.pids.Size())

	// Advance time past expire duration for pid1
	fakeClock.Advance(*pidWhitelisterExpirePIDDuration + time.Second)

	// Update pid2 to be recent
	whitelister.IsWhitelisted(pid2)

	// Clean up expired PIDs
	whitelister.cleanupExpiredPIDs()

	// pid1 should be removed, pid2 should remain
	assert.Equal(t, 1, whitelister.pids.Size())

	_, exists1 := whitelister.pids.Load(pid1)
	_, exists2 := whitelister.pids.Load(pid2)
	assert.False(t, exists1)
	assert.True(t, exists2)
}

func TestPIDWhitelister_CleanupExpiredPIDs_NoExpired(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	pid := request.PidInfo{HostPID: 12345}

	// Add PID
	whitelister.IsWhitelisted(pid)

	// Verify it's in the map
	assert.Equal(t, 1, whitelister.pids.Size())

	// Advance time but not past expire duration
	fakeClock.Advance(*pidWhitelisterExpirePIDDuration - time.Second)

	// Clean up expired PIDs
	whitelister.cleanupExpiredPIDs()

	// PID should still be in the map
	assert.Equal(t, 1, whitelister.pids.Size())

	_, exists := whitelister.pids.Load(pid)
	assert.True(t, exists)
}

func TestPIDWhitelister_Start(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	// Start the cleanup goroutine
	whitelister.Start()

	// Add a PID
	pid := request.PidInfo{HostPID: 12345}
	whitelister.IsWhitelisted(pid)

	// Verify it's in the map
	assert.Equal(t, 1, whitelister.pids.Size())

	// Test that the cleanup goroutine is running by checking that
	// the whitelister is properly initialized
	assert.NotNil(t, whitelister.pids)
	assert.NotNil(t, whitelister.cl)

	// The actual cleanup functionality is tested in TestPIDWhitelister_CleanupExpiredPIDs
	// This test just verifies that Start() doesn't panic and initializes properly
}

func TestGetPIDWhitelister_Singleton(t *testing.T) {
	// Reset the singleton for testing
	pidWhitelister = nil
	once = sync.Once{}

	// Get the whitelister twice
	whitelister1 := GetPIDWhitelister()
	whitelister2 := GetPIDWhitelister()

	// Should be the same instance
	assert.Equal(t, whitelister1, whitelister2)
	assert.NotNil(t, whitelister1)
}

func TestPIDWhitelister_ConcurrentAccess(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	// Test concurrent access to IsWhitelisted
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(pid int) {
			pidInfo := request.PidInfo{HostPID: uint32(pid)}
			whitelister.IsWhitelisted(pidInfo)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have 10 PIDs in the map
	assert.Equal(t, 10, whitelister.pids.Size())
}

func TestPIDWhitelister_EdgeCase_ZeroPID(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	pid := request.PidInfo{HostPID: 0}

	// Should handle zero PID
	result := whitelister.IsWhitelisted(pid)
	assert.False(t, result)

	// Advance time past ignore duration
	fakeClock.Advance(*pidWhitelisterIgnorePIDDuration + time.Second)

	// Should now be whitelisted
	result = whitelister.IsWhitelisted(pid)
	assert.True(t, result)
}

func TestPIDWhitelister_EdgeCase_MaxPID(t *testing.T) {
	fakeClock := clock.NewFakeClock()
	whitelister := newPIDWhitelister(fakeClock)

	pid := request.PidInfo{HostPID: ^uint32(0)} // Max uint32

	// Should handle max PID
	result := whitelister.IsWhitelisted(pid)
	assert.False(t, result)

	// Advance time past ignore duration
	fakeClock.Advance(*pidWhitelisterIgnorePIDDuration + time.Second)

	// Should now be whitelisted
	result = whitelister.IsWhitelisted(pid)
	assert.True(t, result)
}

func TestPIDWhitelister_Stats_Fields(t *testing.T) {
	stats := &PIDWhitelisterStats{
		FirstSeen: time.Now(),
		LastSeen:  time.Now().Add(time.Minute),
	}

	assert.False(t, stats.FirstSeen.IsZero())
	assert.False(t, stats.LastSeen.IsZero())
	assert.True(t, stats.LastSeen.After(stats.FirstSeen))
}
