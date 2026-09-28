package mfa

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Model maps stored fields and normal generated mutations to MFA. Lock uses a
// generated ForUpdate query on the supplied transaction and preserves K.
// SetEnabled returns the complete updated model and must preserve identity.
// Invalidate joins tx (for example auth.Revocations.Invalidate) and revokes every
// registered credential guard. CanDisable enforces required-factor domain policy.
// PasswordLogin.RequiresMFA must include this Enabled state as well as policy.
// Callbacks must not commit, retry, send external messages or acquire other pools.
type Model[M model.Identifiable, K any] struct {
	Lock         func(context.Context, *database.Tx, K) (value.Optional[M], error)
	Enabled      func(M) bool
	SetEnabled   func(context.Context, *database.Tx, M, bool) (M, error)
	AccountLabel func(M) (string, error)
	CanDisable   func(context.Context, M) (bool, error)
	Invalidate   func(context.Context, *database.Tx, M) error
}

// Factors owns one model/provider's TOTP factor across guards. A separate typed
// MFA throttle is mandatory; never reuse the password-attempt declaration.
type Factors[M model.Identifiable, K any] struct {
	store    *Store
	provider auth.Provider[M, K]
	model    Model[M, K]
	attempts lockout.Throttle[K]
	address  Address
	observer *Observer[M, K]
}

func New[M model.Identifiable, K any](store *Store, provider auth.Provider[M, K], binding Model[M, K], attempts lockout.Throttle[K]) (*Factors[M, K], error) {
	for _, err := range []error{store.Validate(), provider.Validate(), attempts.Validate()} {
		if err != nil {
			return nil, err
		}
	}
	if binding.Lock == nil || binding.Enabled == nil || binding.SetEnabled == nil || binding.AccountLabel == nil || binding.CanDisable == nil || binding.Invalidate == nil {
		return nil, fault.New(fault.Invalid, "MFA requires stored model state, mutation, policy and invalidation callbacks")
	}
	address := Address{Namespace: store.config.Namespace, Provider: provider.Name(), Model: provider.ModelName()}
	if err := address.Validate(); err != nil {
		return nil, err
	}
	return &Factors[M, K]{store: store, provider: provider, model: binding, attempts: attempts, address: address}, nil
}
func (f *Factors[M, K]) Validate() error {
	if f == nil {
		return fault.New(fault.Invalid, "MFA factors are not configured")
	}
	return f.store.Validate()
}
func (f *Factors[M, K]) setEnabled(ctx context.Context, tx *database.Tx, current M, enabled bool) (M, error) {
	updated, err := f.model.SetEnabled(ctx, tx, current, enabled)
	if err != nil {
		return *new(M), err
	}
	before, err := current.FoundryIdentity()
	if err != nil {
		return *new(M), err
	}
	after, err := updated.FoundryIdentity()
	if err != nil {
		return *new(M), err
	}
	if before != after || f.model.Enabled(updated) != enabled {
		return *new(M), fault.New(fault.Invalid, "MFA mutation changed identity or returned inconsistent state")
	}
	if err := f.model.Invalidate(ctx, tx, updated); err != nil {
		return *new(M), err
	}
	return updated, nil
}
