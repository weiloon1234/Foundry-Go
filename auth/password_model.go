package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// PasswordModel supplies domain behavior for one concrete model and login key.
// All callbacks are required, concurrency-safe and context-aware. Returned
// models own their data; treat reference-valued fields as read-only. Foundry
// handles verification, rehash decisions, eligibility and assurance. No Actor
// or second provider lookup is involved.
type PasswordModel[M model.Identifiable, I any] struct {
	// Lookup uses the application's normalized login key and tenant scope. Missing
	// models return an omitted Optional. Enforce uniqueness in the database.
	Lookup func(context.Context, I) (value.Optional[M], error)
	// Lock uses the supplied issuance transaction and the prior model's typed
	// primary key to return a current full model with a generated ForUpdate query.
	// It is invoked when a credential is issued, after password verification. This
	// closes the gap where a reset could otherwise precede issuance of an old proof.
	Lock func(context.Context, *database.Tx, M) (value.Optional[M], error)
	// Hash returns the stored value, never a presentation getter's replacement.
	// Zero means an account has no usable password; login rejects it uniformly.
	Hash func(M) password.Hash
	// Rehash conditionally replaces old with next in the authoritative store.
	// Compare old in the same transaction as the write and return its complete
	// resulting model. Omit the result when a concurrent password change wins.
	// Preserve ordinary model lifecycle behavior. Never retry uncertain writes.
	Rehash func(ctx context.Context, subject M, old, next password.Hash) (value.Optional[M], error)
	// RequiresMFA checks current factor policy. It is explicit even when false;
	// a required second factor produces only a PendingMFA proof. Link enrolled
	// factors with PasswordLogin.WithSecondFactor so enrollment always applies.
	RequiresMFA func(context.Context, M) (bool, error)
}

func (m PasswordModel[M, I]) Validate() error {
	if m.Lookup == nil || m.Lock == nil || m.Hash == nil || m.Rehash == nil || m.RequiresMFA == nil {
		return fault.New(fault.Invalid, "password model requires lookup, transactional lock, stored hash, conditional rehash and MFA policy")
	}
	return nil
}
