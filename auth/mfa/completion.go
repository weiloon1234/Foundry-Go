package mfa

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Verifier binds one typed TOTP/recovery response for Sessions.CompleteMFA or
// Tokens.CompleteMFA. It performs no I/O and grants no authority. Completion
// locks/resolves the current model once, checks its eligibility and enabled
// factor, consumes the pending credential and verifies the second factor in the
// credential creation transaction. A failure rolls back all provisional writes.
func (f *Factors[M, K]) Verifier(response Response) (auth.SecondFactor[M, K], error) {
	if err := f.Validate(); err != nil {
		return auth.SecondFactor[M, K]{}, err
	}
	if err := response.Validate(); err != nil {
		return auth.SecondFactor[M, K]{}, err
	}
	if _, ok := f.store.backend.(TransactionalBackend); !ok {
		return auth.SecondFactor[M, K]{}, fault.New(fault.Invalid, "MFA backend cannot join credential creation")
	}
	verifier := auth.DefineSecondFactor(f.provider, func(ctx context.Context, parent *database.Tx, reference model.Reference[M, K], consume func(context.Context, *database.Tx) error) error {
		identity, err := reference.Identity()
		if err != nil {
			return err
		}
		within, err := f.withinTransaction(parent)
		if err != nil {
			return err
		}
		load := func(op context.Context, tx *database.Tx) (M, error) {
			if !parent.SharesTransaction(tx) {
				return *new(M), fault.New(fault.Invalid, "MFA backend substituted its creation transaction")
			}
			found, err := f.model.Lock(op, tx, reference.Key())
			if err != nil {
				return *new(M), err
			}
			current, present := found.Get()
			if !present {
				return *new(M), auth.Unauthenticated
			}
			actual, err := f.provider.CheckModel(op, current)
			if err != nil {
				return *new(M), err
			}
			got, err := actual.Identity()
			if err != nil {
				return *new(M), err
			}
			if got != identity {
				return *new(M), fault.New(fault.Invalid, "MFA model lock returned a different identity")
			}
			return current, nil
		}
		return f.mutate(ctx, identity, within, load, func(op context.Context, tx *database.Tx, current M, stored value.Optional[Record], now temporal.DateTime) (Change, error) {
			record, err := f.active(current, stored, now)
			if err != nil {
				return Change{}, err
			}
			// Consume provisionally before checking the factor: revoked/reused pending
			// credentials cannot clear factor lockout state. Any later error rolls back
			// this deletion, including late lockout, expiry or final credential creation.
			if err := consume(op, tx); err != nil {
				return Change{}, err
			}
			record, err = f.verify(op, record, response, now)
			if err != nil {
				return Change{}, err
			}
			if err := f.changed(op, tx, record.Subject, Verified); err != nil {
				return Change{}, err
			}
			return Replace(record), nil
		})
	})
	return verifier, nil
}
