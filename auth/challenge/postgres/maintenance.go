package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/challengestore"
	"github.com/weiloon1234/Foundry-Go/model"
)

func (b *Backend) Revoke(ctx context.Context, address challenge.Address, identity model.Identity) (bool, error) {
	if _, err := address.SubjectKey(identity); err != nil {
		return false, err
	}
	removed := false
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, present, err := lockSubject(ctx, tx, address, identity, false)
		if err != nil || !present {
			return err
		}
		found, err := entries(subject.Scope, subject.Key).First(ctx, tx)
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
func (b *Backend) Prune(ctx context.Context, address challenge.Address, limit int) (uint64, error) {
	scope, err := address.Key()
	if err != nil {
		return 0, err
	}
	if limit < 1 || limit > challenge.MaxPrune {
		return 0, fault.New(fault.Invalid, "invalid challenge prune limit")
	}
	var removed uint64
	err = b.within(ctx, func(tx *database.Tx) error {
		now, err := b.now()
		if err != nil {
			return err
		}
		fields := challengestore.EntryFields()
		candidates, err := challengestore.ProjectPruneCandidate(entries(scope, "")).SelectID(fields.ID.Value()).SelectSubjectKey(fields.SubjectKey.Value()).Query().Where(fields.ExpiresAt.Lte(now)).OrderBy(fields.SubjectKey.Asc(), fields.ID.Asc()).Limit(limit).All(ctx, tx)
		if err != nil {
			return err
		}
		for _, candidate := range candidates {
			subject, present, err := subjectByKey(ctx, tx, address, candidate.SubjectKey)
			if err != nil {
				return err
			}
			if !present {
				return fault.New(fault.Invalid, "stored challenge has no subject")
			}
			found, err := entries(scope, subject.Key).ForUpdate().Find(ctx, tx, candidate.ID)
			if err != nil {
				return err
			}
			row, present := found.Get()
			if !present {
				continue
			}
			selected, err := record(address, subject, row)
			if err != nil {
				return err
			}
			now, err := b.now()
			if err != nil {
				return err
			}
			if now.UTC().Before(selected.ExpiresAt.UTC()) {
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
