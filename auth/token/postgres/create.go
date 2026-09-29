package postgres

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/tokenstore"
)

func (b *Backend) Create(ctx context.Context, address token.Address, creation token.Creation) (token.Record, error) {
	return b.create(ctx, address, creation, nil, nil)
}
func (b *Backend) CreateChecked(ctx context.Context, address token.Address, creation token.Creation, check func(context.Context, *database.Tx) error) (token.Record, error) {
	if check == nil {
		return token.Record{}, fault.New(fault.Invalid, "credential creation requires a proof check")
	}
	return b.create(ctx, address, creation, check, nil)
}
func (b *Backend) create(ctx context.Context, address token.Address, creation token.Creation, check func(context.Context, *database.Tx) error, beforeCommit func() error) (token.Record, error) {
	if err := creation.Validate(address); err != nil {
		return token.Record{}, err
	}
	var result token.Record
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
			return fault.New(fault.Internal, "token subject was not created")
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
		draft, err := familyDraft(made, subject.Scope, subject.Key)
		if err != nil {
			return err
		}
		family, err := tokenstore.QueryFoundryTokenFamilies().Create(ctx, tx, draft)
		if err != nil {
			return err
		}
		next, err := entryDraft(made, subject.Scope)
		if err != nil {
			return err
		}
		entry, err := tokenstore.QueryFoundryTokenGenerations().Create(ctx, tx, next)
		if err != nil {
			return err
		}
		result, err = record(address, subject, family, entry)
		if err != nil {
			return err
		}
		if beforeCommit != nil {
			return beforeCommit()
		}
		return nil
	})
	if err != nil {
		return token.Record{}, err
	}
	return result, nil
}

// enforceCapacity runs under the subject lock. Expired families are removed
// first in one statement, so they never count. Personal/renewable families and
// pending-MFA challenges have separate caps: a new challenge replaces the
// oldest challenges; a full family evicts the oldest full families or fails
// with auth.CredentialLimit under RejectNew.
func (b *Backend) enforceCapacity(ctx context.Context, tx *database.Tx, subject tokenstore.Subject, creation token.Creation, now time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM `+b.familyTable()+` f USING `+b.generationTable()+` g WHERE f.scope = $1 AND f.subject_key = $2 AND g.family_id = f.id AND g.scope = f.scope AND g.generation = f.generation AND `+dead("$3"), subject.Scope, subject.Key, now); err != nil {
		return err
	}
	pending := creation.Mode == token.Challenge
	var same []string
	err := database.ForEach(ctx, tx, `SELECT id::text, mode FROM `+b.familyTable()+` WHERE scope = $1 AND subject_key = $2 ORDER BY created_at, id LIMIT $3`, []any{subject.Scope, subject.Key, token.MaxTokens + 1}, func(row database.Row) (string, error) {
		var id string
		var mode int16
		if err := row.Scan(&id, &mode); err != nil {
			return "", err
		}
		if (token.Mode(mode) == token.Challenge) != pending {
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
	_, err = tx.Exec(ctx, `DELETE FROM `+b.familyTable()+` WHERE scope = $1 AND subject_key = $2 AND id = ANY($3::uuid[])`, subject.Scope, subject.Key, same[:len(same)-limit+1])
	return err
}
