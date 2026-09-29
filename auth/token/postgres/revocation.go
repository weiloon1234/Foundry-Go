package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ token.CheckedBackend = (*Backend)(nil)
var _ token.TransactionalBackend = (*Backend)(nil)

var _ token.SelectiveBackend = (*Backend)(nil)
var _ token.GraceBackend = (*Backend)(nil)

// revokeAll removes every family of the subject, with generations and consumed
// digests, in one set-based delete under the subject lock shared with issuance.
func (b *Backend) revokeAll(ctx context.Context, tx *database.Tx, address token.Address, identity model.Identity) (uint64, error) {
	subject, present, err := lockSubject(ctx, tx, address, identity, true)
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, fault.New(fault.Internal, "credential subject is absent after locking")
	}
	result, err := tx.Exec(ctx, `DELETE FROM `+b.familyTable()+` WHERE scope = $1 AND subject_key = $2`, subject.Scope, subject.Key)
	if err != nil {
		return 0, err
	}
	return affected(result), nil
}

// RevokeOthers removes every family of the subject except keep, under the
// subject lock. A missing subject has nothing to revoke.
func (b *Backend) RevokeOthers(ctx context.Context, address token.Address, identity model.Identity, keep model.ID[token.Record]) (uint64, error) {
	if _, err := address.SubjectKey(identity); err != nil {
		return 0, err
	}
	if keep.IsZero() {
		return 0, fault.New(fault.Invalid, "token revocation requires the kept family")
	}
	var count uint64
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, present, err := lockSubject(ctx, tx, address, identity, false)
		if err != nil || !present {
			return err
		}
		result, err := tx.Exec(ctx, `DELETE FROM `+b.familyTable()+` WHERE scope = $1 AND subject_key = $2 AND id <> $3`, subject.Scope, subject.Key, keep.String())
		if err != nil {
			return err
		}
		count = affected(result)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
func (b *Backend) RevokeAll(ctx context.Context, address token.Address, identity model.Identity) (uint64, error) {
	if _, err := address.SubjectKey(identity); err != nil {
		return 0, err
	}
	var count uint64
	err := b.within(ctx, func(tx *database.Tx) error {
		var err error
		count, err = b.revokeAll(ctx, tx, address, identity)
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
func (b *Backend) RevokeAllIn(ctx context.Context, tx *database.Tx, address token.Address, identity model.Identity) (uint64, error) {
	if b == nil {
		return 0, fault.New(fault.Invalid, "credential backend is missing")
	}
	if _, err := address.SubjectKey(identity); err != nil {
		return 0, err
	}
	var count uint64
	err := credential.InSchema(ctx, tx, b.db, b.config.Schema, func(child *database.Tx) error {
		var err error
		count, err = b.revokeAll(ctx, child, address, identity)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
