package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sessionstore"
)

func (b *Backend) Create(ctx context.Context, address session.Address, creation session.Creation) (session.Record, error) {
	return b.create(ctx, address, creation, nil, nil)
}
func (b *Backend) CreateChecked(ctx context.Context, address session.Address, creation session.Creation, check func(context.Context, *database.Tx) error) (session.Record, error) {
	if check == nil {
		return session.Record{}, fault.New(fault.Invalid, "credential creation requires a proof check")
	}
	return b.create(ctx, address, creation, check, nil)
}
func (b *Backend) create(ctx context.Context, address session.Address, creation session.Creation, check func(context.Context, *database.Tx) error, beforeCommit func() error) (session.Record, error) {
	if err := creation.Validate(address); err != nil {
		return session.Record{}, err
	}
	var result session.Record
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
			return fault.New(fault.Internal, "session subject was not created")
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		rows, err := subjectRows(ctx, tx, subject.Scope, subject.Key)
		if err != nil {
			return err
		}
		active := 0
		for _, row := range rows {
			existing, err := record(address, subject, row)
			if err != nil {
				return err
			}
			if existing.Live(now) {
				active++
				continue
			}
			if _, err := entries(subject.Scope, subject.Key).Delete(ctx, tx, row.ID); err != nil {
				return err
			}
		}
		if active >= creation.Maximum {
			return fault.New(fault.Conflict, "active session capacity reached")
		}
		made, err := creation.At(address, now)
		if err != nil {
			return err
		}
		row, err := sessionstore.QueryFoundrySessions().Create(ctx, tx, entryDraft(made, subject.Scope, subject.Key))
		if err != nil {
			return err
		}
		result, err = record(address, subject, row)
		if err != nil {
			return err
		}
		if beforeCommit != nil {
			return beforeCommit()
		}
		return nil
	})
	if err != nil {
		return session.Record{}, err
	}
	return result, nil
}
