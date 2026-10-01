package database

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// WithEncryption gives the pool the application key ring used by encrypted
// model fields (database/encrypted). Transactions, sessions and result rows of
// the pool use the same key ring; without it, encrypted reads and writes fail
// instead of exposing or storing plaintext. Configured applications pass their
// Encryption key ring to every database connection.
func WithEncryption(keys *encryption.Keyring) Option {
	return func(settings *poolSettings) error {
		if keys == nil {
			return fault.New(fault.Invalid, "database encryption key ring cannot be nil")
		}
		if err := keys.Validate(); err != nil {
			return err
		}
		settings.encryption = keys
		return nil
	}
}

// Encryption returns the pool's encrypted-field key ring, or nil when none is
// configured. It never acquires a connection.
func (db *DB) Encryption() *encryption.Keyring { return db.fieldKeys }

// Encryption retains the owning pool's key ring on a connection-scoped session.
func (s *Session) Encryption() *encryption.Keyring { return s.owner.Encryption() }

// Encryption retains the owning pool's key ring through savepoints.
func (tx *Tx) Encryption() *encryption.Keyring { return tx.owner.Encryption() }

func (db *DB) attachEncryption(rows *Rows, err error) (*Rows, error) {
	if rows != nil {
		rows.fieldKeys = db.fieldKeys
	}
	return rows, err
}

// FieldEncryption is the decryption scope of a result stream: its query
// context and its pool's key ring. Generated decoders use it through
// FieldEncryption(row) to open encrypted fields while hydrating.
func (r *Rows) FieldEncryption() (context.Context, *encryption.Keyring) { return r.ctx, r.fieldKeys }

// EncryptedRow is a Row that can decrypt encrypted model fields. Rows from a
// pool, transaction or session implement it; framework row adapters forward it.
type EncryptedRow interface {
	Row
	FieldEncryption() (context.Context, *encryption.Keyring)
}

// FieldEncryption returns the decryption scope of row. A row without one, or
// from a pool without a key ring, fails closed: the caller must not hydrate an
// encrypted field it cannot decrypt.
func FieldEncryption(row Row) (context.Context, *encryption.Keyring, error) {
	scoped, ok := row.(EncryptedRow)
	if !ok {
		return nil, nil, fault.New(fault.Invalid, "result row cannot decrypt encrypted model fields")
	}
	ctx, keys := scoped.FieldEncryption()
	if keys == nil {
		return nil, nil, fault.New(fault.Missing, "database has no encryption key ring for encrypted model fields")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx, keys, nil
}
