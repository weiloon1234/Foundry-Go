// Package session manages opaque, revocable credentials for typed model guards.
// Persistence adapters own atomic writes; application code supplies verified
// proofs and consumes models through the ordinary auth.Guard.
package session

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

const MaxLifetime = 365 * 24 * time.Hour
const MaxSessions = 1024
const MaxPageSize = 1024

// Lifetime fixes server-side expiry at creation. Sliding can extend idle expiry
// but never the absolute deadline. Rotation never restarts the absolute lifetime.
type Lifetime struct {
	Idle     time.Duration
	Absolute time.Duration
	Sliding  bool
}

func (l Lifetime) Validate() error {
	if l.Idle < time.Millisecond || l.Absolute < l.Idle || l.Absolute > MaxLifetime || l.Idle%time.Microsecond != 0 || l.Absolute%time.Microsecond != 0 {
		return fault.New(fault.Invalid, "session lifetime requires microsecond precision, positive idle expiry and a bounded absolute expiry")
	}
	return nil
}

// Config contains store-wide resource limits and explicit credential lifetimes.
// Changing policy affects new sessions; use revocation to invalidate old sessions.
type Config struct {
	Namespace     keyspace.Namespace
	Regular       Lifetime
	Remembered    Lifetime
	Pending       Lifetime
	MaxPerSubject int
	MaxConcurrent int
	Timeout       time.Duration
}

func DefaultConfig(namespace keyspace.Namespace) Config {
	return Config{Namespace: namespace, Regular: Lifetime{Idle: 2 * time.Hour, Absolute: 24 * time.Hour, Sliding: true}, Remembered: Lifetime{Idle: 7 * 24 * time.Hour, Absolute: 30 * 24 * time.Hour, Sliding: true}, Pending: Lifetime{Idle: 5 * time.Minute, Absolute: 5 * time.Minute}, MaxPerSubject: 32, MaxConcurrent: 128, Timeout: 5 * time.Second}
}
func (c Config) Validate() error {
	for _, err := range []error{c.Namespace.Validate(), c.Regular.Validate(), c.Remembered.Validate(), c.Pending.Validate()} {
		if err != nil {
			return err
		}
	}
	if c.MaxPerSubject < 1 || c.MaxPerSubject > MaxSessions || c.MaxConcurrent < 1 || c.MaxConcurrent > 65536 || c.Timeout <= 0 || c.Pending.Sliding {
		return fault.New(fault.Invalid, "invalid session capacity, timeout or pending-MFA lifetime")
	}
	return nil
}

// IssueOptions selects persistence policy after credential verification. Pending
// MFA credentials cannot request remember-me persistence.
type IssueOptions struct{ Remember bool }
