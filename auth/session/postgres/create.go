package postgres

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
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
		if err := b.enforceCapacity(ctx, tx, subject, creation, now); err != nil {
			return err
		}
		made, err := creation.At(address, now)
		if err != nil {
			return err
		}
		client, agent := deviceColumns(made)
		impersonator, guard, actor, err := impersonatorColumns(made)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO `+b.sessions()+` (id, scope, subject_key, secret_hash, assurance, remember, sliding, idle_nanos, created_at, last_seen_at, idle_expires_at, expires_at, client_ip, user_agent, impersonator_identity, impersonator_guard, impersonator_session) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::uuid)`,
			made.ID.String(), subject.Scope, subject.Key, made.Hash.Hex(), int16(made.Assurance), made.Remember, made.Lifetime.Sliding, int64(made.Lifetime.Idle),
			made.CreatedAt.UTC(), made.LastSeenAt.UTC(), made.IdleExpiresAt.UTC(), made.ExpiresAt.UTC(), client, agent, impersonator, guard, actor); err != nil {
			return err
		}
		result = made
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

// enforceCapacity runs under the subject lock. Expired rows are removed first
// so they never count. Full, pending-MFA and impersonation sessions are three
// separate classes: a new pending or impersonation session replaces the oldest
// of its class (PendingMaximum); a full session evicts the oldest full ones or
// fails with auth.CredentialLimit under RejectNew.
func (b *Backend) enforceCapacity(ctx context.Context, tx *database.Tx, subject sessionstore.Subject, creation session.Creation, now time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM `+b.sessions()+` WHERE scope = $1 AND subject_key = $2 AND (idle_expires_at <= $3 OR expires_at <= $3)`, subject.Scope, subject.Key, now); err != nil {
		return err
	}
	class := func(assurance auth.Assurance, impersonated bool) int {
		switch {
		case assurance == auth.PendingMFA:
			return 1
		case impersonated:
			return 2
		}
		return 0
	}
	wanted := class(creation.Assurance, creation.Impersonator.IsSet())
	pending := wanted != 0
	var same []string
	err := database.ForEach(ctx, tx, `SELECT id::text, assurance, impersonator_session IS NOT NULL FROM `+b.sessions()+` WHERE scope = $1 AND subject_key = $2 ORDER BY created_at, id LIMIT $3`, []any{subject.Scope, subject.Key, session.MaxSessions + 1}, func(row database.Row) (string, error) {
		var id string
		var assurance int16
		var impersonated bool
		if err := row.Scan(&id, &assurance, &impersonated); err != nil {
			return "", err
		}
		if class(auth.Assurance(assurance), impersonated) != wanted {
			return "", nil
		}
		return id, nil
	}, func(id string) error {
		if id != "" {
			same = append(same, id)
		}
		return nil
	})
	if err != nil {
		return err
	}
	limit := creation.Maximum
	if pending {
		limit = creation.PendingMaximum
	}
	if len(same) < limit {
		return nil
	}
	if !pending && creation.Limit == auth.RejectNew {
		return auth.CredentialLimit
	}
	_, err = tx.Exec(ctx, `DELETE FROM `+b.sessions()+` WHERE scope = $1 AND subject_key = $2 AND id = ANY($3::uuid[])`, subject.Scope, subject.Key, same[:len(same)-limit+1])
	return err
}
