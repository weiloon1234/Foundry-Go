package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func (l *PasswordLogin[M, K, I]) checkedResult(subject M, proof Proof[M, K]) (PasswordResult[M, K], error) {
	expectedHash := l.model.Hash(subject)
	check := &passwordCheck[M]{provider: l.provider.definition.id, verify: func(ctx context.Context, tx *database.Tx) (M, error) {
		found, err := l.model.Lock(ctx, tx, subject)
		if err != nil {
			return *new(M), err
		}
		current, present := found.Get()
		if !present {
			return *new(M), Unauthenticated
		}
		identity, err := current.FoundryIdentity()
		if err != nil {
			return *new(M), err
		}
		if identity != proof.Identity() {
			return *new(M), fault.New(fault.Invalid, "password issuance lock returned a different model")
		}
		if l.model.Hash(current) != expectedHash {
			return *new(M), Unauthenticated
		}
		if err := l.provider.checkEligibility(ctx, current); err != nil {
			return *new(M), err
		}
		required, err := l.model.RequiresMFA(ctx, current)
		if err != nil {
			return *new(M), err
		}
		if required != (proof.Assurance() == PendingMFA) {
			return *new(M), Unauthenticated
		}
		if err := ctx.Err(); err != nil {
			return *new(M), err
		}
		return current, nil
	}}
	checked, err := proof.WithIssuanceCheck(func(ctx context.Context, tx *database.Tx) error {
		_, err := check.verify(ctx, tx)
		return err
	})
	if err != nil {
		return PasswordResult[M, K]{}, err
	}
	return PasswordResult[M, K]{subject: subject, proof: checked, recheck: check}, nil
}
