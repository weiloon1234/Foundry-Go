package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sessionstore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func validateCredential(address session.Address, hash session.Digest) error {
	if err := address.Validate(); err != nil {
		return err
	}
	if hash.IsZero() {
		return fault.New(fault.Invalid, "session credential hash is empty")
	}
	return nil
}

// credential takes the stable subject lock before locking/re-reading its session
// row. Rotation/revocation/touch share this order. The initial lookup is only a
// locator; it cannot authenticate a row that changed while waiting for the lock.
func credential(ctx context.Context, tx *database.Tx, address session.Address, hash session.Digest) (sessionstore.Subject, sessionstore.Entry, bool, error) {
	scope, err := address.Key()
	if err != nil {
		return sessionstore.Subject{}, sessionstore.Entry{}, false, err
	}
	f := sessionstore.EntryFields()
	locator, err := entries(scope, "").Where(f.SecretHash.Eq(hash.Hex())).First(ctx, tx)
	if err != nil {
		return sessionstore.Subject{}, sessionstore.Entry{}, false, err
	}
	first, present := locator.Get()
	if !present {
		return sessionstore.Subject{}, sessionstore.Entry{}, false, nil
	}
	subject, present, err := lockSubjectKey(ctx, tx, address, first.SubjectKey)
	if err != nil || !present {
		return sessionstore.Subject{}, sessionstore.Entry{}, false, err
	}
	locked, err := entries(scope, subject.Key).Where(f.SecretHash.Eq(hash.Hex())).ForUpdate().Find(ctx, tx, first.ID)
	if err != nil {
		return sessionstore.Subject{}, sessionstore.Entry{}, false, err
	}
	row, present := locked.Get()
	return subject, row, present, nil
}

// Lookup is the per-request read path: one schema-qualified statement joins
// the session and its subject without a transaction or row lock, so parallel
// requests, listings, pruning and revocation never serialize on the subject.
// Expiry is checked against the clock sampled after the read. A sliding
// session records activity only when it is older than TouchInterval, with one
// conditional UPDATE that cannot revive a removed, rotated or expired row. A
// revocation committing concurrently with this read applies from the next scope.
func (b *Backend) Lookup(ctx context.Context, address session.Address, hash session.Digest, touch bool) (value.Optional[session.Record], error) {
	if err := validateCredential(address, hash); err != nil {
		return value.Optional[session.Record]{}, err
	}
	if b == nil || b.db == nil || ctx == nil {
		return value.Optional[session.Record]{}, fault.New(fault.Invalid, "session PostgreSQL operation requires a backend and context")
	}
	scope, err := address.Key()
	if err != nil {
		return value.Optional[session.Record]{}, err
	}
	rows, err := b.readSessions(ctx, b.db, 1, `WHERE e.scope = $1 AND e.secret_hash = $2`, scope, hash.Hex())
	if err != nil || len(rows) == 0 {
		return value.Optional[session.Record]{}, err
	}
	current, err := record(address, rows[0].subject, rows[0].entry)
	if err != nil {
		return value.Optional[session.Record]{}, err
	}
	now, err := b.now()
	if err != nil {
		return value.Optional[session.Record]{}, err
	}
	// An impersonation session ends as soon as its actor session is revoked or
	// expires; it can never outlive the actor.
	if !current.Live(now) || !rows[0].actorLive(now) {
		return value.Optional[session.Record]{}, nil
	}
	if touch && current.NeedsTouch(now) {
		updated, live, err := current.Touch(now)
		if err != nil {
			return value.Optional[session.Record]{}, err
		}
		if !live {
			return value.Optional[session.Record]{}, nil
		}
		result, err := b.db.Exec(ctx, `UPDATE `+b.sessions()+` SET last_seen_at = $4, idle_expires_at = $5 WHERE id = $1 AND scope = $2 AND secret_hash = $3 AND last_seen_at < $4 AND idle_expires_at > $6 AND expires_at > $6`,
			current.ID.String(), scope, hash.Hex(), updated.LastSeenAt.UTC(), updated.IdleExpiresAt.UTC(), now)
		if err != nil {
			return value.Optional[session.Record]{}, err
		}
		if result.RowsAffected == 1 {
			current = updated
		}
	}
	return value.Set(current), nil
}
func (b *Backend) Rotate(ctx context.Context, address session.Address, old, next session.Digest) (value.Optional[session.Record], error) {
	if err := validateCredential(address, old); err != nil {
		return value.Optional[session.Record]{}, err
	}
	if next.IsZero() || next.Equal(old) {
		return value.Optional[session.Record]{}, fault.New(fault.Invalid, "session rotation requires a new secret hash")
	}
	var result value.Optional[session.Record]
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, row, present, err := credential(ctx, tx, address, old)
		if err != nil || !present {
			return err
		}
		current, err := record(address, subject, row)
		if err != nil {
			return err
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		current, live, err := current.Touch(now)
		if err != nil || !live {
			return err
		}
		draft := sessionstore.EntryDraft{}.SetSecretHash(next.Hex()).SetLastSeenAt(current.LastSeenAt).SetIdleExpiresAt(current.IdleExpiresAt)
		row, err = entries(subject.Scope, subject.Key).Update(ctx, tx, row.ID, draft)
		if err != nil {
			return err
		}
		updated, err := record(address, subject, row)
		if err != nil {
			return err
		}
		result = value.Set(updated)
		return nil
	})
	if err != nil {
		return value.Optional[session.Record]{}, err
	}
	return result, nil
}
func (b *Backend) Revoke(ctx context.Context, address session.Address, hash session.Digest) (bool, error) {
	if err := validateCredential(address, hash); err != nil {
		return false, err
	}
	removed := false
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, row, present, err := credential(ctx, tx, address, hash)
		if err != nil || !present {
			return err
		}
		if _, err := record(address, subject, row); err != nil {
			return err
		}
		if _, err := entries(subject.Scope, subject.Key).Delete(ctx, tx, row.ID); err != nil {
			return err
		}
		removed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}
