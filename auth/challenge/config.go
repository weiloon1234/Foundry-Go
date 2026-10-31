// Package challenge owns single-use, model-bound recovery credentials. Typed
// password-reset and email-verification flows share one atomic persistence
// contract. No challenge produces an authentication proof.
package challenge

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

const MaxLifetime = 7 * 24 * time.Hour
const MaxPrune = 128

// Config declares the challenge namespace and operation admission bounds.
type Config struct {
	Namespace     keyspace.Namespace
	MaxConcurrent int
	Timeout       time.Duration
}

// DefaultConfig returns default challenge concurrency and timeout bounds for namespace.
func DefaultConfig(namespace keyspace.Namespace) Config {
	return Config{Namespace: namespace, MaxConcurrent: 128, Timeout: 15 * time.Second}
}

// Validate checks the namespace, bounded positive concurrency and positive timeout.
func (c Config) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxConcurrent < 1 || c.MaxConcurrent > 65536 || c.Timeout <= 0 {
		return safeerror.NewFault(fault.Invalid, "invalid challenge capacity or timeout")
	}
	return nil
}

// ValidateLifetime requires a whole-microsecond lifetime between one millisecond and
// MaxLifetime.
func ValidateLifetime(lifetime time.Duration) error {
	if lifetime < time.Millisecond || lifetime > MaxLifetime || lifetime%time.Microsecond != 0 {
		return safeerror.NewFault(fault.Invalid, "invalid challenge lifetime")
	}
	return nil
}

// Purpose is closed to the supported recovery operations. It is preserved in
// token and issuer types; a verification token cannot reset a password.
type Purpose interface {
	PasswordReset | EmailVerification
}

// PasswordReset marks tokens and flows for password reset.
type PasswordReset struct{}

// EmailVerification marks tokens and flows for email verification.
type EmailVerification struct{}

// Kind identifies a persisted recovery purpose.
type Kind string

const (
	ResetPassword Kind = "password-reset"
	VerifyEmail   Kind = "email-verification"
)

func purpose[P Purpose]() Kind {
	switch any(*new(P)).(type) {
	case PasswordReset:
		return ResetPassword
	default:
		return VerifyEmail
	}
}
