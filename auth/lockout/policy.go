// Package lockout provides typed failed-credential windows and temporary lockout.
// It is distinct from request-rate limiting: concurrent admitted attempts can
// still complete. Pair it with ratelimit before expensive credential work.
package lockout

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/value"
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

// Limits is the client-aware login throttle. PerClient counts failures for one
// account from one client address, so a single source cannot keep a victim
// locked out for everyone. Account is a higher per-account ceiling that still
// bounds distributed guessing; its tradeoff is that an attacker spreading
// failures across many addresses can lock one targeted account for LockFor.
//
// Address is an optional per-client-address ceiling across all accounts that
// bounds credential spraying from one source. It is off by default: every user
// behind a shared address (carrier-grade NAT, a corporate proxy or VPN) shares
// it, so it can lock all of them out, including correct passwords, and with
// misconfigured trusted proxies every request appears to come from the proxy
// and one ceiling covers the whole site. Enable it (for example with
// DefaultAddressPolicy) only when client addresses are trusted and rarely
// shared, and prefer HTTP request rate limits for spraying. Each enabled
// ceiling must be at least the PerClient threshold.
type Limits struct {
	PerClient Policy
	Account   Policy
	Address   value.Optional[Policy]
}

// DefaultLimits enables the pair and account windows; Address is off.
func DefaultLimits() Limits {
	return Limits{
		PerClient: DefaultPolicy(),
		Account:   Policy{MaxFailures: 50, Window: 15 * time.Minute, LockFor: 15 * time.Minute},
	}
}

// DefaultAddressPolicy is the documented opt-in per-address ceiling: 100
// failures across all accounts in 15 minutes lock that address for 15 minutes.
func DefaultAddressPolicy() Policy {
	return Policy{MaxFailures: 100, Window: 15 * time.Minute, LockFor: 15 * time.Minute}
}

func (l Limits) Validate() error {
	for _, p := range []Policy{l.PerClient, l.Account} {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	if l.Account.MaxFailures < l.PerClient.MaxFailures {
		return fault.New(fault.Invalid, "lockout ceilings must not be lower than the per-client threshold")
	}
	if address, enabled := l.Address.Get(); enabled {
		if err := address.Validate(); err != nil {
			return err
		}
		if address.MaxFailures < l.PerClient.MaxFailures {
			return fault.New(fault.Invalid, "lockout ceilings must not be lower than the per-client threshold")
		}
	}
	return nil
}
