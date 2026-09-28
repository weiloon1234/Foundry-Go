// Package challenge owns single-use, model-bound recovery credentials. Typed
// password-reset and email-verification flows share one atomic persistence
// contract. No challenge produces an authentication proof.
package challenge

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

const MaxLifetime = 7 * 24 * time.Hour
const MaxPrune = 128

type Config struct {
	Namespace     keyspace.Namespace
	MaxConcurrent int
	Timeout       time.Duration
}

func DefaultConfig(namespace keyspace.Namespace) Config {
	return Config{Namespace: namespace, MaxConcurrent: 128, Timeout: 15 * time.Second}
}
func (c Config) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxConcurrent < 1 || c.MaxConcurrent > 65536 || c.Timeout <= 0 {
		return fault.New(fault.Invalid, "invalid challenge capacity or timeout")
	}
	return nil
}
func ValidateLifetime(lifetime time.Duration) error {
	if lifetime < time.Millisecond || lifetime > MaxLifetime || lifetime%time.Microsecond != 0 {
		return fault.New(fault.Invalid, "invalid challenge lifetime")
	}
	return nil
}

// Purpose is closed to the supported recovery operations. It is preserved in
// token and issuer types; a verification token cannot reset a password.
type Purpose interface {
	PasswordReset | EmailVerification
}
type PasswordReset struct{}
type EmailVerification struct{}

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
