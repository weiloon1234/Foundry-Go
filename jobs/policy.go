package jobs

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
	"slices"
	"time"
)

const MaxAttempts = 1000
const MaxDelay = 365 * 24 * time.Hour
const MaxTimeout = 24 * time.Hour
const MaxEnvelopeBytes = value.JSONMaxBytes
const MaxPayloadBytes = MaxEnvelopeBytes - (64 << 10)

// Policy owns delivery defaults for one declared job. Backoff contains retry
// delays after failures; attempts beyond the slice use its final delay. Jitter
// adds a uniformly selected duration in [0, Jitter], bounded by MaxDelay.
// Each snapshot owns its Backoff slice. Attempts count started executions,
// including those whose worker subsequently disappears.
type Policy struct {
	Queue    Queue           `json:"queue"`
	Attempts uint32          `json:"attempts"`
	Timeout  time.Duration   `json:"timeout"`
	Backoff  []time.Duration `json:"backoff"`
	Jitter   time.Duration   `json:"jitter"`
}

func DefaultPolicy(queue Queue) Policy {
	return Policy{Queue: queue, Attempts: 5, Timeout: 5 * time.Minute,
		Backoff: []time.Duration{5 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}, Jitter: time.Second}
}
func (p Policy) Validate() error {
	if err := p.Queue.Validate(); err != nil {
		return err
	}
	if p.Attempts == 0 || p.Attempts > MaxAttempts || p.Timeout <= 0 || p.Timeout > MaxTimeout || len(p.Backoff) == 0 || len(p.Backoff) > MaxAttempts || p.Jitter < 0 || p.Jitter > MaxDelay {
		return fault.New(fault.Invalid, "invalid job attempt, timeout or retry bounds")
	}
	for _, delay := range p.Backoff {
		if delay < 0 || delay > MaxDelay-p.Jitter {
			return fault.New(fault.Invalid, "invalid job retry delay")
		}
	}
	return nil
}
func (p Policy) snapshot() Policy { p.Backoff = slices.Clone(p.Backoff); return p }
func (p Policy) same(other Policy) bool {
	return p.Queue == other.Queue && p.Attempts == other.Attempts && p.Timeout == other.Timeout && p.Jitter == other.Jitter && slices.Equal(p.Backoff, other.Backoff)
}

// RetryDelay excludes jitter so tests and operators can inspect the schedule.
func (p Policy) RetryDelay(attempt uint32) (time.Duration, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	if attempt == 0 {
		return 0, fault.New(fault.Invalid, "retry requires a started attempt")
	}
	return p.Backoff[min(int(attempt)-1, len(p.Backoff)-1)], nil
}
