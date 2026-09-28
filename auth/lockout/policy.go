// Package lockout provides typed failed-credential windows and temporary lockout.
// It is distinct from request-rate limiting: concurrent admitted attempts can
// still complete. Pair it with ratelimit before expensive credential work.
package lockout

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

const MaxFailures uint32 = 10000
const MaxDuration = ratelimit.MaxWindow

// Policy counts completed failures in a window starting at the first admitted
// attempt. Reaching MaxFailures locks until LockFor elapses. Denials never extend
// that expiry. A successful attempt clears only failures observed at its start;
// later failures are retained. Window expiry or an explicit Reset invalidates
// outstanding attempts, which must be retried rather than granting authority.
type Policy struct {
	MaxFailures     uint32
	Window, LockFor time.Duration
}

func DefaultPolicy() Policy {
	return Policy{MaxFailures: 5, Window: 15 * time.Minute, LockFor: 15 * time.Minute}
}
func (p Policy) Validate() error {
	if p.MaxFailures < 1 || p.MaxFailures > MaxFailures {
		return fault.New(fault.Invalid, "invalid login failure threshold")
	}
	for _, d := range []time.Duration{p.Window, p.LockFor} {
		if d < time.Millisecond || d > MaxDuration || d%time.Millisecond != 0 {
			return fault.New(fault.Invalid, "lockout durations require whole milliseconds between 1ms and 24h")
		}
	}
	return nil
}
