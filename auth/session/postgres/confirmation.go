package postgres

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Confirm stamps confirmed_at on one live, fully authenticated session of the
// subject with a conditional UPDATE, then returns its current record.
func (b *Backend) Confirm(ctx context.Context, address session.Address, identity model.Identity, id model.ID[session.Record]) (value.Optional[session.Record], error) {
	key, scope, err := b.sessionTarget(ctx, address, identity, id)
	if err != nil {
		return value.Optional[session.Record]{}, err
	}
	now, err := b.now()
	if err != nil {
		return value.Optional[session.Record]{}, err
	}
	result, err := b.db.Exec(ctx, `UPDATE `+b.sessions()+` SET confirmed_at = $4 WHERE id = $1 AND scope = $2 AND subject_key = $3 AND assurance = 2 AND impersonator_session IS NULL AND idle_expires_at > $4 AND expires_at > $4`, id.String(), scope, key, now)
	if err != nil || result.RowsAffected != 1 {
		return value.Optional[session.Record]{}, err
	}
	rows, err := b.readSessions(ctx, b.db, 1, `WHERE e.id = $1 AND e.scope = $2 AND e.subject_key = $3`, id.String(), scope, key)
	if err != nil || len(rows) == 0 {
		return value.Optional[session.Record]{}, err
	}
	current, err := record(address, rows[0].subject, rows[0].entry)
	if err != nil {
		return value.Optional[session.Record]{}, err
	}
	if !current.Live(now) {
		return value.Optional[session.Record]{}, nil
	}
	return value.Set(current), nil
}

// ConfirmedWithin reports whether the live session confirmed its password at
// or after now-within, using this backend's clock.
func (b *Backend) ConfirmedWithin(ctx context.Context, address session.Address, identity model.Identity, id model.ID[session.Record], within time.Duration) (bool, error) {
	if within <= 0 {
		return false, fault.New(fault.Invalid, "password confirmation window must be positive")
	}
	key, scope, err := b.sessionTarget(ctx, address, identity, id)
	if err != nil {
		return false, err
	}
	now, err := b.now()
	if err != nil {
		return false, err
	}
	var recent bool
	err = database.ScanOne(ctx, b.db, `SELECT EXISTS (SELECT 1 FROM `+b.sessions()+` WHERE id = $1 AND scope = $2 AND subject_key = $3 AND confirmed_at >= $4 AND idle_expires_at > $5 AND expires_at > $5)`, []any{id.String(), scope, key, now.Add(-within), now}, &recent)
	return recent, err
}

func (b *Backend) sessionTarget(ctx context.Context, address session.Address, identity model.Identity, id model.ID[session.Record]) (string, string, error) {
	if b == nil || b.db == nil || ctx == nil || id.IsZero() {
		return "", "", fault.New(fault.Invalid, "session PostgreSQL operation requires a backend, context and session")
	}
	key, err := address.SubjectKey(identity)
	if err != nil {
		return "", "", err
	}
	scope, err := address.Key()
	return key, scope, err
}
