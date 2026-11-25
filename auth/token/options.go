package token

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Option configures one token binding at construction.
type Option func(*options) error

type options struct {
	lifetimes    Lifetimes
	lifetimesSet bool
}

// Lifetimes are one guard's issuance lifetimes by mode.
type Lifetimes struct {
	// Personal is the lifetime of tokens issued without a refresh token. A zero value uses the store's lifetime; otherwise it must be valid for the mode and not exceed the store's.
	Personal Lifetime
	// Renewable is the lifetime of renewable access and refresh token pairs. A zero value uses the store's lifetime; otherwise it must be valid for the mode and not exceed the store's.
	Renewable Lifetime
	// Challenge is the lifetime of pending-MFA challenge tokens. A zero value uses the store's lifetime; otherwise it must be valid for the mode and not exceed the store's.
	Challenge Lifetime
}

// WithLifetimes shortens this guard's issuance lifetimes, for example admin
// tokens that expire sooner than user tokens issued from the same store. A zero
// Lifetime keeps the store's for that mode. The store's lifetimes are the
// ceiling: no duration may exceed the store's for its mode, so the configured
// store bounds every guard. Each lifetime must be valid for its mode, and the
// renewable access lifetime must still cover Config.AccessGrace. A family keeps
// the lifetime it was issued with across refreshes.
func WithLifetimes(lifetimes Lifetimes) Option {
	return func(o *options) error {
		if o.lifetimesSet {
			return safeerror.NewFault(fault.Duplicate, "token lifetimes are repeated")
		}
		o.lifetimes, o.lifetimesSet = lifetimes, true
		return nil
	}
}

// within resolves a guard's lifetimes below the store's.
func (l Lifetimes) within(c Config) (Lifetimes, error) {
	var result Lifetimes
	var err error
	if result.Personal, err = narrow(Personal, l.Personal, c.Personal); err != nil {
		return Lifetimes{}, err
	}
	if result.Renewable, err = narrow(Renewable, l.Renewable, c.Renewable); err != nil {
		return Lifetimes{}, err
	}
	if result.Challenge, err = narrow(Challenge, l.Challenge, c.Challenge); err != nil {
		return Lifetimes{}, err
	}
	if c.AccessGrace > result.Renewable.Access {
		return Lifetimes{}, safeerror.NewFault(fault.Invalid, "token access grace exceeds the guard's renewable access lifetime")
	}
	return result, nil
}

func narrow(mode Mode, guard, ceiling Lifetime) (Lifetime, error) {
	if guard == (Lifetime{}) {
		return ceiling, nil
	}
	if err := guard.Validate(mode); err != nil {
		return Lifetime{}, err
	}
	if guard.Access > ceiling.Access || guard.RefreshIdle > ceiling.RefreshIdle || guard.Absolute > ceiling.Absolute {
		return Lifetime{}, safeerror.NewFault(fault.Invalid, "guard token lifetime exceeds the store's")
	}
	return guard, nil
}
