package schedule

import (
	"reflect"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/lease"
)

// Group isolates independent scheduler elections in one namespace. All replicas
// of a group must deploy the same registry, timing and environment policy.
type Group string
type Config struct {
	Group         Group
	Clock         clock.Clock
	Concurrency   int
	MaxHistory    int
	MaxPerTick    int
	PollInterval  time.Duration
	LeadershipTTL time.Duration
	// Grace bounds lateness for schedules without catch-up. Older occurrences are
	// skipped. Process startup itself skips all missed occurrences by default.
	Grace time.Duration
	// Wake optionally replaces the polling ticker for controlled application time.
	// The caller owns the channel; closing it stops Run with an error. Leadership
	// and overlap heartbeats still use real safety time independent of this clock.
	Wake <-chan struct{}
}

func DefaultConfig(group Group) Config {
	return Config{Group: group, Clock: clock.System{}, Concurrency: 4, MaxHistory: 1024, MaxPerTick: 256, PollInterval: 100 * time.Millisecond, LeadershipTTL: 30 * time.Second, Grace: time.Second}
}
func nilValue(value any) bool {
	if value == nil {
		return true
	}
	r := reflect.ValueOf(value)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func (c Config) Validate() error {
	if !identifier.Semantic(string(c.Group)) || nilValue(c.Clock) || c.Concurrency < 1 || c.Concurrency > 4096 || c.MaxHistory < 1 || c.MaxHistory > 65536 || c.MaxPerTick < 1 || c.MaxPerTick > 4096 || c.PollInterval <= 0 || c.PollInterval > time.Minute || c.Grace < 0 || c.Grace > 24*time.Hour {
		return fault.New(fault.Invalid, "invalid scheduler configuration")
	}
	return lease.ValidateDuration(c.LeadershipTTL)
}
