package session

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
func (s *Sessions[M, K]) RevokeAllIn(ctx context.Context, tx *database.Tx, reference model.Reference[M, K]) (uint64, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if tx == nil {
		return 0, fault.New(fault.Invalid, "credential revocation requires a transaction")
	}
	backend, ok := s.store.backend.(TransactionalBackend)
	if !ok {
		return 0, fault.New(fault.Invalid, "credential backend cannot join revocation transaction")
	}
	var count uint64
	err := s.store.execute(ctx, func(op context.Context) error {
		identity, err := reference.Identity()
		if err != nil {
			return err
		}
		if _, err := s.provider.Parse(identity); err != nil {
			return err
		}
		// A count never fails the joined transaction: rejecting it would roll back
		// the caller's credential change (for example a password reset).
		count, err = backend.RevokeAllIn(op, tx, s.address, identity)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Revocation contributes this bound guard to auth.NewRevocations. Construct the
// group once and pass its Invalidate method to password-reset/domain callbacks.
func (s *Sessions[M, K]) Revocation() (auth.Revocation[M, K], error) {
	if err := s.Validate(); err != nil {
		return auth.Revocation[M, K]{}, err
	}
	if _, ok := s.store.backend.(TransactionalBackend); !ok {
		return auth.Revocation[M, K]{}, fault.New(fault.Invalid, "credential backend cannot join revocation transaction")
	}
	key, err := s.address.Key()
	if err != nil {
		return auth.Revocation[M, K]{}, err
	}
	return auth.DefineRevocation(auth.RevocationName("session."+key), s.provider, s.RevokeAllIn), nil
}
