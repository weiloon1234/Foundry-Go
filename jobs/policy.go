package jobs

import (
	"math/rand/v2"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

const MaxAttempts = 1000
const MaxDelay = 365 * 24 * time.Hour
const MaxTimeout = 24 * time.Hour
const MaxEnvelopeBytes = value.JSONMaxBytes
const MaxPayloadBytes = MaxEnvelopeBytes - (64 << 10)

// Policy owns delivery defaults for one declared job. Backoff contains retry
// delays after failures; attempts beyond the slice use its final delay. Jitter
// is the minimum retry spread: a retry adds a uniformly selected duration in
// [0, max(Jitter, delay/5)], bounded by MaxDelay, so long delays spread out
// proportionally. Zero Jitter disables jitter entirely.
// Each snapshot owns its Backoff slice. Attempts count started executions,
// including those whose worker subsequently disappears.
//
// MaxExceptions fails the job once that many attempts ended with a handler
// error, panic or timeout, even while Attempts remain; zero disables it. The
// built-in backends count exceptions per record; a custom backend that does not
// report Reservation.Exceptions makes only MaxExceptions 1 effective.
// RetryUntil is an absolute deadline: once it passes the job is not started
// again and a failed attempt is final. Declared policies normally leave it zero
// and dispatches set it with Options.RetryUntil. Either field requires
// ExtendedEnvelope; upgrade every worker and publisher before using them.
type Policy struct {
	Queue         Queue           `json:"queue"`
	Attempts      uint32          `json:"attempts"`
	Timeout       time.Duration   `json:"timeout"`
	Backoff       []time.Duration `json:"backoff"`
	Jitter        time.Duration   `json:"jitter"`
	MaxExceptions uint32          `json:"max_exceptions,omitzero"`
	RetryUntil    time.Time       `json:"retry_until,omitzero"`
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
	if p.MaxExceptions > p.Attempts || !p.RetryUntil.IsZero() && (p.RetryUntil.Year() < 1970 || p.RetryUntil.Year() > 9999) {
		return fault.New(fault.Invalid, "invalid job exception limit or retry deadline")
	}
	return nil
}

// extended reports fields that older envelope readers cannot decode.
func (p Policy) extended() bool   { return p.MaxExceptions != 0 || !p.RetryUntil.IsZero() }
func (p Policy) snapshot() Policy { p.Backoff = slices.Clone(p.Backoff); return p }
func (p Policy) same(other Policy) bool {
	return p.Queue == other.Queue && p.Attempts == other.Attempts && p.Timeout == other.Timeout && p.Jitter == other.Jitter && slices.Equal(p.Backoff, other.Backoff) &&
		p.MaxExceptions == other.MaxExceptions && p.RetryUntil.Equal(other.RetryUntil)
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

// jittered spreads a retry delay proportionally so a burst of failures does not
// retry in lockstep. Jitter is the minimum spread; zero disables jitter.
func (p Policy) jittered(delay time.Duration) time.Duration {
	if p.Jitter <= 0 {
		return delay
	}
	spread := max(p.Jitter, delay/5)
	return min(MaxDelay, delay+rand.N(spread+1))
}
