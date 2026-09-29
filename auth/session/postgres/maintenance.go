package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sessionstore"
	"github.com/weiloon1234/Foundry-Go/model"
)

func (b *Backend) RevokeID(ctx context.Context, address session.Address, identity model.Identity, id model.ID[session.Record]) (bool, error) {
	if _, err := address.SubjectKey(identity); err != nil {
		return false, err
	}
	if id.IsZero() {
		return false, fault.New(fault.Invalid, "session revocation requires an identifier")
	}
	removed := false
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, present, err := lockSubject(ctx, tx, address, identity, false)
		if err != nil || !present {
			return err
		}
		key := model.IDFromBytes[sessionstore.Entry](id.Bytes())
		found, err := entries(subject.Scope, subject.Key).ForUpdate().Find(ctx, tx, key)
		if err != nil {
			return err
		}
		row, present := found.Get()
		if !present {
			return nil
		}
		if _, err := record(address, subject, row); err != nil {
			return err
		}
		if _, err := entries(subject.Scope, subject.Key).Delete(ctx, tx, key); err != nil {
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

// List reads the subject's live sessions in one statement without locking the
// subject, so listing never blocks issuance or request authentication.
func (b *Backend) List(ctx context.Context, address session.Address, identity model.Identity, limit int) ([]session.Record, error) {
	key, err := address.SubjectKey(identity)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > session.MaxPageSize {
		return nil, fault.New(fault.Invalid, "invalid session listing limit")
	}
	if b == nil || b.db == nil || ctx == nil {
		return nil, fault.New(fault.Invalid, "session PostgreSQL operation requires a backend and context")
	}
	scope, err := address.Key()
	if err != nil {
		return nil, err
	}
	now, err := b.now()
	if err != nil {
		return nil, err
	}
	rows, err := b.readSessions(ctx, b.db, session.MaxSessions, `WHERE e.scope = $1 AND e.subject_key = $2 AND e.idle_expires_at > $3 AND e.expires_at > $3 ORDER BY e.created_at, e.id LIMIT $4`, scope, key, now, session.MaxSessions+1)
	if err != nil {
		return nil, err
	}
	var result []session.Record
	for _, row := range rows {
		current, err := record(address, row.subject, row.entry)
		if err != nil {
			return nil, err
		}
		if current.Live(now) && row.actorLive(now) {
			if len(result) >= limit {
				return nil, fault.New(fault.Invalid, "live session listing exceeds requested limit")
			}
			result = append(result, current)
		}
	}
	return result, nil
}

// Prune deletes at most limit expired sessions of this address in one
// set-based, index-backed statement. Rows locked by concurrent work are
// skipped, and expiry is rechecked on the row actually deleted, so a session
// touched meanwhile survives. Subject rows remain for serialization.
func (b *Backend) Prune(ctx context.Context, address session.Address, limit int) (uint64, error) {
	scope, err := address.Key()
	if err != nil {
		return 0, err
	}
	if limit < 1 || limit > session.MaxPageSize {
		return 0, fault.New(fault.Invalid, "invalid session prune limit")
	}
	if b == nil || b.db == nil || ctx == nil {
		return 0, fault.New(fault.Invalid, "session PostgreSQL operation requires a backend and context")
	}
	now, err := b.now()
	if err != nil {
		return 0, err
	}
	table := b.sessions()
	// The locking CTE is evaluated once: an IN subquery may be rescanned per row,
	// and its LIMIT would then no longer bound the delete.
	result, err := b.db.Exec(ctx, `WITH doomed AS MATERIALIZED (SELECT id FROM `+table+` WHERE scope = $1 AND (idle_expires_at <= $2 OR expires_at <= $2) LIMIT $3 FOR UPDATE SKIP LOCKED) `+
		`DELETE FROM `+table+` s USING doomed WHERE s.id = doomed.id AND s.scope = $1 AND (s.idle_expires_at <= $2 OR s.expires_at <= $2)`, scope, now, limit)
	if err != nil {
		return 0, err
	}
	if result.RowsAffected < 0 {
		return 0, fault.New(fault.Invalid, "session prune returned an invalid count")
	}
	return uint64(result.RowsAffected), nil
}
