package cache

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"time"
)

// TTL explicitly selects finite expiry or persistence. Its zero value is invalid.
// A TTL takes effect when the backend accepts a write, not when its value is encoded.
type TTL struct {
	duration time.Duration
	forever  bool
}

func For(duration time.Duration) TTL { return TTL{duration: duration} }
func Forever() TTL                   { return TTL{forever: true} }
func (t TTL) Validate() error {
	if t.forever && t.duration == 0 || !t.forever && t.duration > 0 {
		return nil
	}
	return fault.New(fault.Invalid, "cache TTL must be positive or explicitly persistent")
}
func (t TTL) IsForever() bool         { return t.forever }
func (t TTL) Duration() time.Duration { return t.duration }
