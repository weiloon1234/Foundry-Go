package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/challengestore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func (b *Backend) Consume(ctx context.Context, address challenge.Address, hash challenge.Digest, apply func(context.Context, *database.Tx, challenge.Record) error) (value.Optional[challenge.Record], error) {
	if err := validateCredential(address, hash); err != nil {
		return value.Optional[challenge.Record]{}, err
	}
	if apply == nil {
		return value.Optional[challenge.Record]{}, fault.New(fault.Invalid, "challenge consumption requires an action")
	}
	scope, _ := address.Key()
	var result value.Optional[challenge.Record]
	err := b.within(ctx, func(tx *database.Tx) error {
		candidate, err := entries(scope, "").Where(challengestore.EntryFields().SecretHash.Eq(hash.Hex())).First(ctx, tx)
		if err != nil {
			return err
		}
		original, present := candidate.Get()
		if !present {
			return nil
		}
		subject, present, err := subjectByKey(ctx, tx, address, original.SubjectKey)
		if err != nil {
			return err
		}
		if !present {
			return fault.New(fault.Invalid, "stored challenge has no subject")
		}
		current, err := entries(scope, subject.Key).ForUpdate().Find(ctx, tx, original.ID)
		if err != nil {
			return err
		}
		row, present := current.Get()
		if !present {
			return nil
		}
		selected, err := record(address, subject, row)
		if err != nil {
			return err
		}
		if !selected.Hash.Equal(hash) {
			return nil
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		if !selected.Live(now.UTC()) {
			return nil
		}
		if err := apply(ctx, tx, selected); err != nil {
			return err
		}
		// The model lock/action can wait. An expired action must roll back its model
		// changes as well; merely returning an omitted result would commit them.
		now, err = b.now()
		if err != nil {
			return err
		}
		if !selected.Live(now.UTC()) {
			return auth.Unauthenticated
		}
		if _, err := entries(scope, subject.Key).Delete(ctx, tx, row.ID); err != nil {
			return err
		}
		result = value.Set(selected)
		return nil
	})
	if err != nil {
		return value.Optional[challenge.Record]{}, err
	}
	return result, nil
}
