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
var _ session.SelectiveBackend = (*Backend)(nil)
var _ session.ConfirmationBackend = (*Backend)(nil)

// revokeAll removes every session of the subject with one set-based delete
// under the subject lock shared with issuance.
func (b *Backend) revokeAll(ctx context.Context, tx *database.Tx, address session.Address, identity model.Identity) (uint64, error) {
	subject, present, err := lockSubject(ctx, tx, address, identity, true)
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, fault.New(fault.Internal, "credential subject is absent after locking")
	}
	// Impersonation sessions started from these sessions end with them: Lookup
	// and List refuse any impersonation whose actor session no longer exists.
	result, err := tx.Exec(ctx, `DELETE FROM `+b.sessions()+` WHERE scope = $1 AND subject_key = $2`, subject.Scope, subject.Key)
	if err != nil {
		return 0, err
	}
	return uint64(max(result.RowsAffected, 0)), nil
}

// RevokeOthers removes every session of the subject except keep, under the
// subject lock. A missing subject has nothing to revoke.
func (b *Backend) RevokeOthers(ctx context.Context, address session.Address, identity model.Identity, keep model.ID[session.Record]) (uint64, error) {
	if _, err := address.SubjectKey(identity); err != nil {
		return 0, err
	}
	if keep.IsZero() {
		return 0, fault.New(fault.Invalid, "session revocation requires the kept session")
	}
	var count uint64
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, present, err := lockSubject(ctx, tx, address, identity, false)
		if err != nil || !present {
			return err
		}
		result, err := tx.Exec(ctx, `DELETE FROM `+b.sessions()+` WHERE scope = $1 AND subject_key = $2 AND id <> $3`, subject.Scope, subject.Key, keep.String())
		if err != nil {
			return err
		}
		count = uint64(max(result.RowsAffected, 0))
		return nil
	})
	if err != nil {
		return 0, err
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
		count, err = b.revokeAll(ctx, child, address, identity)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
