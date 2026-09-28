package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/tokenstore"
)

func (b *Backend) Create(ctx context.Context, address token.Address, creation token.Creation) (token.Record, error) {
	return b.create(ctx, address, creation, nil, nil)
}
func (b *Backend) CreateChecked(ctx context.Context, address token.Address, creation token.Creation, check func(context.Context, *database.Tx) error) (token.Record, error) {
	if check == nil {
		return token.Record{}, fault.New(fault.Invalid, "credential creation requires a proof check")
	}
	return b.create(ctx, address, creation, check, nil)
}
func (b *Backend) create(ctx context.Context, address token.Address, creation token.Creation, check func(context.Context, *database.Tx) error, beforeCommit func() error) (token.Record, error) {
	if err := creation.Validate(address); err != nil {
		return token.Record{}, err
	}
	var result token.Record
	err := b.within(ctx, func(tx *database.Tx) error {
		// Lock the application model before credential subjects. Password reset uses
		// the same order, so old password proofs cannot issue after invalidation.
		if check != nil {
			if err := check(ctx, tx); err != nil {
				return err
			}
		}
		subject, present, err := lockSubject(ctx, tx, address, creation.Subject, true)
		if err != nil {
			return err
		}
		if !present {
			return fault.New(fault.Internal, "token subject was not created")
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		rows, err := subjectFamilies(ctx, tx, subject)
		if err != nil {
			return err
		}
		active := 0
		for _, family := range rows {
			current, err := currentRecord(ctx, tx, address, subject, family)
			if err != nil {
				return err
			}
			if current.Live(now) {
				active++
				continue
			}
			if _, err := families(subject.Scope, subject.Key).Delete(ctx, tx, family.ID); err != nil {
				return err
			}
		}
		if active >= creation.Maximum {
			return fault.New(fault.Conflict, "active token capacity reached")
		}
		made, err := creation.At(address, now)
		if err != nil {
			return err
		}
		draft, err := familyDraft(made, subject.Scope, subject.Key)
		if err != nil {
			return err
		}
		family, err := tokenstore.QueryFoundryTokenFamilies().Create(ctx, tx, draft)
		if err != nil {
			return err
		}
		next, err := entryDraft(made, subject.Scope)
		if err != nil {
			return err
		}
		entry, err := tokenstore.QueryFoundryTokenGenerations().Create(ctx, tx, next)
		if err != nil {
			return err
		}
		result, err = record(address, subject, family, entry)
		if err != nil {
			return err
		}
		if beforeCommit != nil {
			return beforeCommit()
		}
		return nil
	})
	if err != nil {
		return token.Record{}, err
	}
	return result, nil
}
