package whitelister

import (
	"sync"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/beyla/v2/pkg/clock"
	"github.com/grafana/beyla/v2/pkg/flaggy"
	"github.com/grafana/beyla/v2/pkg/internal/request"
)

var pidWhitelisterIgnorePIDDuration = flaggy.GetEnvDurationVar(
	"PID_WHITELISTER_IGNORE_PID_DURATION",
	"pid_whitelister_ignore_pid_duration",
	"PID Whitelister will ignore a PID for this duration after it is first seen",
	3*time.Minute,
)

var pidWhitelisterExpirePIDDuration = flaggy.GetEnvDurationVar(
	"PID_WHITELISTER_EXPIRE_PID_DURATION",
	"pid_whitelister_expire_pid_duration",
	"PID Whitelister will expire a PID if not seen for this duration",
	5*time.Minute,
)

var pidWhitelisterExpirePollingInterval = flaggy.GetEnvDurationVar(
	"PID_WHITELISTER_EXPIRE_POLLING_INTERVAL",
	"pid_whitelister_expire_polling_interval",
	"PID Whitelister will poll for expired PIDs every this duration",
	time.Minute,
)

type PIDWhitelisterStats struct {
	FirstSeen time.Time
	LastSeen  time.Time
}

type PIDWhitelister struct {
	ignoreDuration time.Duration
	expireDuration time.Duration
	pids           *xsync.MapOf[request.PidInfo, *PIDWhitelisterStats]
	eg             errgroup.Group
	cl             clock.Clock
}

func newPIDWhitelister(cl clock.Clock) *PIDWhitelister {
	return &PIDWhitelister{
		ignoreDuration: *pidWhitelisterIgnorePIDDuration,
		expireDuration: *pidWhitelisterExpirePIDDuration,
		pids:           xsync.NewMapOf[request.PidInfo, *PIDWhitelisterStats](),
		cl:             cl,
	}
}

func (pw *PIDWhitelister) Start() {
	pw.eg.Go(func() error {
		ticker := time.NewTicker(*pidWhitelisterExpirePollingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				pw.cleanupExpiredPIDs()
			}
		}
	})
}

func (pidw *PIDWhitelister) cleanupExpiredPIDs() {
	pidw.pids.Range(func(pid request.PidInfo, info *PIDWhitelisterStats) bool {
		if pidw.cl.Since(info.LastSeen) > pidw.expireDuration {
			pidw.pids.Delete(pid)
		}
		return true
	})
}

func (pw *PIDWhitelister) IsWhitelisted(pid request.PidInfo) bool {
	now := pw.cl.Now()

	info, ok := pw.pids.LoadOrCompute(pid, func() *PIDWhitelisterStats {
		return &PIDWhitelisterStats{
			FirstSeen: now,
			LastSeen:  now,
		}
	})

	if !ok {
		return false
	}

	info.LastSeen = now

	return pw.cl.Since(info.FirstSeen) > pw.ignoreDuration
}

var pidWhitelister *PIDWhitelister
var once sync.Once

func GetPIDWhitelister() *PIDWhitelister {
	once.Do(func() {
		pidWhitelister = newPIDWhitelister(clock.NewRealClock())
		pidWhitelister.Start()
	})

	return pidWhitelister
}
