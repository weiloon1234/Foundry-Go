package token

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

// RevokeAllIn joins a caller's transaction. Its count is provisional and the
// caller must propagate errors. It never acquires a model lock; credential-change
// orchestration must lock that model first. Unsupported adapters fail closed.
func (t *Tokens[M, K]) RevokeAllIn(ctx context.Context, tx *database.Tx, reference model.Reference[M, K]) (uint64, error) {
	if err := t.Validate(); err != nil {
		return 0, err
	}
	if tx == nil {
		return 0, fault.New(fault.Invalid, "credential revocation requires a transaction")
	}
	backend, ok := t.store.backend.(TransactionalBackend)
	if !ok {
		return 0, fault.New(fault.Invalid, "credential backend cannot join revocation transaction")
	}
	var count uint64
	err := t.store.execute(ctx, func(op context.Context) error {
		identity, err := reference.Identity()
		if err != nil {
			return err
		}
		if _, err := t.provider.Parse(identity); err != nil {
			return err
		}
		count, err = backend.RevokeAllIn(op, tx, t.address, identity)
		if err == nil && count > MaxTokens {
			return fault.New(fault.Invalid, "credential backend exceeded revocation capacity")
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Revocation contributes this bound guard to auth.NewRevocations. Construct the
// group once and pass its Invalidate method to password-reset/domain callbacks.
func (t *Tokens[M, K]) Revocation() (auth.Revocation[M, K], error) {
	if err := t.Validate(); err != nil {
		return auth.Revocation[M, K]{}, err
	}
	if _, ok := t.store.backend.(TransactionalBackend); !ok {
		return auth.Revocation[M, K]{}, fault.New(fault.Invalid, "credential backend cannot join revocation transaction")
	}
	key, err := t.address.Key()
	if err != nil {
		return auth.Revocation[M, K]{}, err
	}
	return auth.DefineRevocation(auth.RevocationName("token."+key), t.provider, t.RevokeAllIn), nil
}
