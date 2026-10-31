package cache

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// TTL explicitly selects finite expiry or persistence. Its zero value is invalid.
// A TTL takes effect when the backend accepts a write, not when its value is encoded.
type TTL struct {
	duration time.Duration
	forever  bool
}

// For constructs a finite TTL whose duration must be positive when validated.
func For(duration time.Duration) TTL { return TTL{duration: duration} }

// Forever returns an explicitly persistent TTL.
func Forever() TTL { return TTL{forever: true} }

// Validate returns fault.Invalid unless the TTL is positive or explicitly persistent.
func (t TTL) Validate() error {
	if t.forever && t.duration == 0 || !t.forever && t.duration > 0 {
		return nil
	}
	return safeerror.NewFault(fault.Invalid, "cache TTL must be positive or explicitly persistent")
}

// IsForever reports whether persistence was explicitly selected.
func (t TTL) IsForever() bool { return t.forever }

// Duration returns the finite duration, or zero for a persistent TTL.
func (t TTL) Duration() time.Duration { return t.duration }
