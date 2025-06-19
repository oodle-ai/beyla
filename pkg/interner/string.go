package intern

import (
	"github.com/grafana/beyla/v2/pkg/flaggy"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
	"golang.org/x/sync/errgroup"
)

var defaultStringInternerResetInterval = flaggy.GetEnvDurationVar(
	"DEFAULT_STRING_INTERNER_RESET_INTERVAL",
	"default_string_interner_reset_interval",
	"Interval after which string interner is reset",
	2*time.Hour,
)

var (
	defaultStringInterner *StringInterner
	once                  sync.Once
)

type StringInterner struct {
	cache       atomic.Pointer[xsync.MapOf[string, string]]
	eg          errgroup.Group
	resetTicker *time.Ticker
	closed      chan struct{}
}

func NewStringInterner(
	resetInterval time.Duration,
) *StringInterner {
	interner := &StringInterner{
		closed: make(chan struct{}),
	}

	interner.cache.Store(xsync.NewMapOf[string, string]())

	if resetInterval > 0 {
		interner.resetTicker = time.NewTicker(*defaultStringInternerResetInterval)
		interner.eg.Go(func() error {
			defer interner.resetTicker.Stop()
			for {
				select {
				case <-interner.closed:
					return nil
				case <-interner.resetTicker.C:
					interner.cache.Store(xsync.NewMapOf[string, string]())
				}
			}
		})
	}

	return interner
}

func GetDefaultStringInterner() *StringInterner {
	once.Do(func() {
		defaultStringInterner = NewStringInterner(*defaultStringInternerResetInterval)
	})

	return defaultStringInterner
}

func (s *StringInterner) Stop() error {
	close(s.closed)
	return nil
}

func (s *StringInterner) Intern(str string) string {
	// Fast path.
	v, exists := s.cache.Load().Load(str)
	if exists {
		return v
	}

	// Create a deep copy since original buffer may be overwritten.
	str = strings.Clone(str)
	v, _ = s.cache.Load().LoadOrStore(str, str)
	return v
}
