// Package ratelimit supplies typed, atomic fixed-window admission decisions.
package ratelimit

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"time"
)

// MaxWindow bounds live policy retention and retry durations.
const MaxWindow = 24 * time.Hour

// Limit admits Requests units per fixed Window. Each key's windows are shifted by
// its Key.WindowOffset, so many keys do not reset at the same instant. A fixed
// window permits bursts on both sides of a boundary; it is not a sliding window.
type Limit struct {
	Requests uint32
	Window   time.Duration
}

func PerSecond(requests uint32) Limit { return Limit{requests, time.Second} }
func PerMinute(requests uint32) Limit { return Limit{requests, time.Minute} }
func PerHour(requests uint32) Limit   { return Limit{requests, time.Hour} }
func (l Limit) Validate() error {
	if l.Requests == 0 || l.Window < time.Millisecond || l.Window > MaxWindow || l.Window%time.Millisecond != 0 {
		return fault.New(fault.Invalid, "rate limit needs positive capacity and a whole-millisecond window between 1ms and 24h")
	}
	return nil
}
func (l Limit) ValidateCost(cost uint32) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if cost == 0 || cost > l.Requests {
		return fault.New(fault.Invalid, "rate limit cost must be positive and no greater than capacity")
	}
	return nil
}

// Decision reports the authority's decision at execution time. Denials consume
// no capacity. Durations are relative to that decision, not the client clock.
// An error always returns a zero Decision; never infer admission from an error.
type Decision struct {
	Allowed                bool
	Limit, Remaining       uint32
	ResetAfter, RetryAfter time.Duration
}

// Validate checks adapter output against the exact policy and request cost.
func (d Decision) Validate(limit Limit, cost uint32) error {
	if err := limit.ValidateCost(cost); err != nil {
		return err
	}
	if d.Limit != limit.Requests || d.Remaining > limit.Requests || d.ResetAfter <= 0 || d.ResetAfter > limit.Window || d.ResetAfter%time.Millisecond != 0 ||
		(d.Allowed && (d.RetryAfter != 0 || d.Remaining > limit.Requests-cost)) ||
		(!d.Allowed && (d.RetryAfter != d.ResetAfter || d.Remaining >= cost)) {
		return fault.New(fault.Internal, "invalid rate limit decision")
	}
	return nil
}

// ValidatePeek checks non-consuming adapter output: Remaining is the capacity
// left now, Allowed reports whether cost fits it, and RetryAfter is zero when
// allowed or equals ResetAfter when not.
func (d Decision) ValidatePeek(limit Limit, cost uint32) error {
	if err := limit.ValidateCost(cost); err != nil {
		return err
	}
	if d.Limit != limit.Requests || d.Remaining > limit.Requests || d.ResetAfter <= 0 || d.ResetAfter > limit.Window || d.ResetAfter%time.Millisecond != 0 ||
		d.Allowed != (cost <= d.Remaining) || (d.Allowed && d.RetryAfter != 0) || (!d.Allowed && d.RetryAfter != d.ResetAfter) {
		return fault.New(fault.Internal, "invalid rate limit inspection")
	}
	return nil
}
