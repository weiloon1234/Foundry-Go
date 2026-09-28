package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sessionstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
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

func (b *Backend) List(ctx context.Context, address session.Address, identity model.Identity, limit int) ([]session.Record, error) {
	if _, err := address.SubjectKey(identity); err != nil {
		return nil, err
	}
	if limit < 1 || limit > session.MaxPageSize {
		return nil, fault.New(fault.Invalid, "invalid session listing limit")
	}
	var result []session.Record
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, present, err := lockSubject(ctx, tx, address, identity, false)
		if err != nil || !present {
			return err
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		rows, err := subjectRows(ctx, tx, subject.Scope, subject.Key)
		if err != nil {
			return err
		}
		for _, row := range rows {
			record, err := record(address, subject, row)
			if err != nil {
				return err
			}
			if record.Live(now) {
				if len(result) >= limit {
					return fault.New(fault.Invalid, "live session listing exceeds requested limit")
				}
				result = append(result, record)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (b *Backend) Prune(ctx context.Context, address session.Address, limit int) (uint64, error) {
	scope, err := address.Key()
	if err != nil {
		return 0, err
	}
	if limit < 1 || limit > session.MaxPageSize {
		return 0, fault.New(fault.Invalid, "invalid session prune limit")
	}
	var removed uint64
	err = b.within(ctx, func(tx *database.Tx) error {
		now, err := b.now()
		if err != nil {
			return err
		}
		instant, err := temporal.NewDateTime(now)
		if err != nil {
			return err
		}
		f := sessionstore.EntryFields()
		rows, err := entries(scope, "").Where(query.Or(f.IdleExpiresAt.Lte(instant), f.ExpiresAt.Lte(instant))).OrderBy(f.SubjectKey.Asc(), f.ID.Asc()).Limit(limit).All(ctx, tx)
		if err != nil {
			return err
		}
		// A consistent subject lock order prevents concurrent prune batches from
		// deadlocking. Candidate rows are re-read after the subject lock is held.
		var subject sessionstore.Subject
		for _, candidate := range rows {
			if subject.Key != candidate.SubjectKey {
				locked, present, err := lockSubjectKey(ctx, tx, address, candidate.SubjectKey)
				if err != nil {
					return err
				}
				if !present {
					return fault.New(fault.Invalid, "session record has no subject")
				}
				subject = locked
			}
			current, err := entries(scope, subject.Key).ForUpdate().Find(ctx, tx, candidate.ID)
			if err != nil {
				return err
			}
			row, present := current.Get()
			if !present {
				continue
			}
			record, err := record(address, subject, row)
			if err != nil {
				return err
			}
			now, err := b.now()
			if err != nil {
				return err
			}
			if record.Live(now) {
				continue
			}
			if _, err := entries(scope, subject.Key).Delete(ctx, tx, row.ID); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}
