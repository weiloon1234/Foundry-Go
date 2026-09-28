package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	credentialruntime "github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ session.CheckedBackend = (*Backend)(nil)
var _ session.TransactionalBackend = (*Backend)(nil)

func revokeAll(ctx context.Context, tx *database.Tx, address session.Address, identity model.Identity) (uint64, error) {
	subject, present, err := lockSubject(ctx, tx, address, identity, true)
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, fault.New(fault.Internal, "credential subject is absent after locking")
	}
	rows, err := subjectRows(ctx, tx, subject.Scope, subject.Key)
	if err != nil {
		return 0, err
	}
	var count uint64
	for _, row := range rows {
		if _, err := record(address, subject, row); err != nil {
			return 0, err
		}
		if _, err := entries(subject.Scope, subject.Key).Delete(ctx, tx, row.ID); err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}
func (b *Backend) RevokeAll(ctx context.Context, address session.Address, identity model.Identity) (uint64, error) {
	if _, err := address.SubjectKey(identity); err != nil {
		return 0, err
	}
	var count uint64
	err := b.within(ctx, func(tx *database.Tx) error {
		var err error
		count, err = revokeAll(ctx, tx, address, identity)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// RevokeAllIn returns a provisional count: only the caller's outer transaction
// can commit. It rejects a transaction from another pool and scopes schema changes
// inside a savepoint. The original standalone method reuses the same row logic.
func (b *Backend) RevokeAllIn(ctx context.Context, tx *database.Tx, address session.Address, identity model.Identity) (uint64, error) {
	if b == nil {
		return 0, fault.New(fault.Invalid, "credential backend is missing")
	}
	if _, err := address.SubjectKey(identity); err != nil {
		return 0, err
	}
	var count uint64
	err := credentialruntime.InSchema(ctx, tx, b.db, b.config.Schema, func(child *database.Tx) error {
		var err error
		count, err = revokeAll(ctx, child, address, identity)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
