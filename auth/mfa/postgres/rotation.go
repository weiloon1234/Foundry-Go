package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ mfa.RotationBackend = (*Backend)(nil)

// Rotation statements are schema-qualified set operations: the typed query
// layer has no prefix predicate or compare-and-set update. The schema passed
// the shared identifier validator at construction; every value is bound.
func (b *Backend) factorTable() string { return `"` + b.config.Schema + `".foundry_mfa_factors` }

// StaleCiphertexts walks factors in primary-key order and returns those whose
// envelope does not carry the active key's prefix. It locks nothing.
func (b *Backend) StaleCiphertexts(ctx context.Context, active encryption.KeyID, after string, limit int) ([]mfa.StoredCiphertext, error) {
	if b == nil || b.db == nil || ctx == nil {
		return nil, fault.New(fault.Invalid, "MFA operation requires a backend and context")
	}
	if limit < 1 || limit > mfa.MaxRotationBatch {
		return nil, fault.New(fault.Invalid, "invalid MFA rotation batch size")
	}
	prefix, err := encryption.EnvelopePrefix(active)
	if err != nil {
		return nil, err
	}
	var result []mfa.StoredCiphertext
	err = database.ForEach(ctx, b.db, `SELECT key, generation::text, ciphertext FROM `+b.factorTable()+` WHERE key > $1 AND left(ciphertext, $2) <> $3 ORDER BY key LIMIT $4`, []any{after, len(prefix), prefix, limit}, func(row database.Row) (mfa.StoredCiphertext, error) {
		var owner, generation, encoded string
		if err := row.Scan(&owner, &generation, &encoded); err != nil {
			return mfa.StoredCiphertext{}, err
		}
		id, err := model.ParseID[mfa.Record](generation)
		if err != nil {
			return mfa.StoredCiphertext{}, err
		}
		ciphertext, err := encryption.ParseCiphertext(encoded)
		if err != nil {
			return mfa.StoredCiphertext{}, err
		}
		return mfa.StoredCiphertext{Owner: owner, Generation: id, Ciphertext: ciphertext}, nil
	}, func(item mfa.StoredCiphertext) error {
		if len(result) >= limit {
			return fault.New(fault.Invalid, "MFA rotation rows exceed their bound")
		}
		result = append(result, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReplaceCiphertext is one conditional UPDATE: it waits for a concurrent factor
// transaction holding the row and then rechecks generation and envelope, so a
// replaced, removed or already rotated factor is never overwritten.
func (b *Backend) ReplaceCiphertext(ctx context.Context, stored mfa.StoredCiphertext, next encryption.Ciphertext) (bool, error) {
	if b == nil || b.db == nil || ctx == nil {
		return false, fault.New(fault.Invalid, "MFA operation requires a backend and context")
	}
	if stored.Owner == "" || stored.Generation.IsZero() || stored.Ciphertext.IsZero() || next.IsZero() || len(next.Encoded()) > 512 {
		return false, fault.New(fault.Invalid, "invalid MFA ciphertext replacement")
	}
	result, err := b.db.Exec(ctx, `UPDATE `+b.factorTable()+` SET ciphertext = $4 WHERE key = $1 AND generation = $2 AND ciphertext = $3`, stored.Owner, stored.Generation.String(), stored.Ciphertext.Encoded(), next.Encoded())
	if err != nil {
		return false, err
	}
	return result.RowsAffected == 1, nil
}
