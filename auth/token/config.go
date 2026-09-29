// Package token manages model-owned personal and renewable access credentials.
// Persistence adapters own atomic refresh/reuse behavior. Guards resolve current
// models and use auth.AccessScopes as a credential ceiling, never as permissions.
package token

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

const MaxLifetime = 365 * 24 * time.Hour
const MaxTokens = 1024
const MaxRotations = 4096
const MaxPruneFamilies = 1024

// MaxPrefixBytes bounds Config.Prefix.
const MaxPrefixBytes = 16

// Mode distinguishes nonrenewable personal credentials, renewable pairs and
// short-lived MFA challenges. A challenge never carries ordinary access scopes.
type Mode uint8

const (
	Personal Mode = iota + 1
	Renewable
	Challenge
)

// Lifetime preserves a family's absolute expiry across every refresh. Access
// activity never extends refresh idle expiry. Only successful refresh does so.
type Lifetime struct{ Access, RefreshIdle, Absolute time.Duration }

func durationValid(d time.Duration) bool {
	return d >= time.Millisecond && d <= MaxLifetime && d%time.Microsecond == 0
}
func (l Lifetime) Validate(mode Mode) error {
	if !durationValid(l.Access) || !durationValid(l.Absolute) || l.Access > l.Absolute {
		return fault.New(fault.Invalid, "invalid token access or absolute lifetime")
	}
	switch mode {
	case Personal:
		if l.RefreshIdle != 0 || l.Access != l.Absolute {
			return fault.New(fault.Invalid, "personal tokens require one fixed expiry")
		}
	case Renewable:
		if !durationValid(l.RefreshIdle) || l.RefreshIdle < l.Access || l.RefreshIdle > l.Absolute {
			return fault.New(fault.Invalid, "invalid token refresh idle lifetime")
		}
	case Challenge:
		if l.RefreshIdle != 0 || l.Access != l.Absolute || l.Absolute > 15*time.Minute {
			return fault.New(fault.Invalid, "MFA token requires a short fixed expiry")
		}
	default:
		return fault.New(fault.Invalid, "invalid token mode")
	}
	return nil
}

// Config bounds the store and fixes new tokens' lifetimes.
// MaxPerSubject caps a subject's live personal/renewable families per guard;
// Limit selects rejection with auth.CredentialLimit (default) or evicting the
// oldest. Pending-MFA challenge tokens only count toward MaxPendingPerSubject
// and always replace the oldest pending one. Expired families never count.
// AccessGrace keeps a refreshed family's previous access token valid for this
// long after the refresh (never beyond its own expiry), so requests already in
// flight with it do not fail; zero disables it. Prefix is prepended to newly
// issued access/refresh secrets for secret scanning (for example "myapp_");
// secrets with or without a prefix are accepted, since only the random part is
// hashed.
type Config struct {
	Namespace            keyspace.Namespace
	Personal             Lifetime
	Renewable            Lifetime
	Challenge            Lifetime
	MaxPerSubject        int
	MaxPendingPerSubject int
	Limit                auth.LimitPolicy
	MaxRotations         int
	AccessGrace          time.Duration
	Prefix               string
	MaxConcurrent        int
	Timeout              time.Duration
}

func DefaultConfig(namespace keyspace.Namespace) Config {
	return Config{Namespace: namespace,
		Personal:      Lifetime{Access: 30 * 24 * time.Hour, Absolute: 30 * 24 * time.Hour},
		Renewable:     Lifetime{Access: 15 * time.Minute, RefreshIdle: 7 * 24 * time.Hour, Absolute: 30 * 24 * time.Hour},
		Challenge:     Lifetime{Access: 5 * time.Minute, Absolute: 5 * time.Minute},
		MaxPerSubject: 32, MaxPendingPerSubject: 8, Limit: auth.RejectNew, MaxRotations: MaxRotations, AccessGrace: 30 * time.Second, MaxConcurrent: 128, Timeout: 5 * time.Second}
}
func (c Config) Validate() error {
	for _, err := range []error{c.Namespace.Validate(), c.Personal.Validate(Personal), c.Renewable.Validate(Renewable), c.Challenge.Validate(Challenge), c.Limit.Validate(), validatePrefix(c.Prefix)} {
		if err != nil {
			return err
		}
	}
	if c.MaxPerSubject < 1 || c.MaxPendingPerSubject < 1 || c.MaxPerSubject+c.MaxPendingPerSubject > MaxTokens || c.MaxRotations < 1 || c.MaxRotations > MaxRotations || c.MaxConcurrent < 1 || c.MaxConcurrent > 65536 || c.Timeout <= 0 {
		return fault.New(fault.Invalid, "invalid token capacity or timeout")
	}
	if c.AccessGrace < 0 || c.AccessGrace > c.Renewable.Access || c.AccessGrace%time.Microsecond != 0 {
		return fault.New(fault.Invalid, "token access grace must be between zero and the renewable access lifetime")
	}
	return nil
}

// validatePrefix allows lowercase letters, digits and underscores, which fit
// both bearer-token grammar and common secret-scanner patterns.
func validatePrefix(prefix string) error {
	if len(prefix) > MaxPrefixBytes {
		return fault.New(fault.Invalid, "token prefix is too long")
	}
	for i := range len(prefix) {
		c := prefix[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return fault.New(fault.Invalid, "token prefix allows only lowercase letters, digits and underscores")
		}
	}
	return nil
}

// IssueOptions is chosen by trusted credential verification, not mass-assigned
// from a login DTO. Requested scopes must fit this binding's declared ceiling.
// A scoped proof can only issue a subset of its own grants.
type IssueOptions[M any] struct {
	Name    string
	Scopes  auth.AccessScopes[M]
	Refresh bool
}
