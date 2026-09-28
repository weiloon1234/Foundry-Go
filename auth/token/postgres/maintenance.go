package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/tokenstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func (b *Backend) RevokeID(ctx context.Context, address token.Address, identity model.Identity, id model.ID[token.Record]) (bool, error) {
	if _, err := address.SubjectKey(identity); err != nil {
		return false, err
	}
	if id.IsZero() {
		return false, fault.New(fault.Invalid, "token revocation requires an identifier")
	}
	var removed bool
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, present, err := lockSubject(ctx, tx, address, identity, false)
		if err != nil || !present {
			return err
		}
		key := model.IDFromBytes[tokenstore.Family](id.Bytes())
		found, err := families(subject.Scope, subject.Key).ForUpdate().Find(ctx, tx, key)
		if err != nil {
			return err
		}
		family, present := found.Get()
		if !present {
			return nil
		}
		if _, err := currentRecord(ctx, tx, address, subject, family); err != nil {
			return err
		}
		if _, err := families(subject.Scope, subject.Key).Delete(ctx, tx, key); err != nil {
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
func (b *Backend) List(ctx context.Context, address token.Address, identity model.Identity, limit int) ([]token.Record, error) {
	if _, err := address.SubjectKey(identity); err != nil {
		return nil, err
	}
	if limit < 1 || limit > token.MaxTokens {
		return nil, fault.New(fault.Invalid, "invalid token listing limit")
	}
	var result []token.Record
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, present, err := lockSubject(ctx, tx, address, identity, false)
		if err != nil || !present {
			return err
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		rows, err := subjectFamilies(ctx, tx, subject)
		if err != nil {
			return err
		}
		for _, family := range rows {
			current, err := currentRecord(ctx, tx, address, subject, family)
			if err != nil {
				return err
			}
			if current.Live(now) {
				if len(result) >= limit {
					return fault.New(fault.Invalid, "live token listing exceeds requested limit")
				}
				result = append(result, current)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type pruneFamilyAlias struct{}
type pruneEntryAlias struct{}

func pruneCandidates(ctx context.Context, tx *database.Tx, scope string, now temporal.DateTime, limit int) ([]tokenstore.PruneCandidate, error) {
	left := query.As[pruneFamilyAlias](families(scope, ""), "token_family")
	right := query.As[pruneEntryAlias](entries(scope, model.ID[tokenstore.Family]{}), "token_generation")
	lf, rf := tokenstore.FamilyFieldsAt(left.Scope()), tokenstore.EntryFieldsAt(right.Scope())
	joined := query.InnerJoin(left, right, query.OnAnd(query.On(lf.ID, rf.FamilyID), query.On(lf.Generation, rf.Generation)))
	family := tokenstore.FamilyFieldsAt(query.LeftScope(joined, left.Scope()))
	entry := tokenstore.EntryFieldsAt(query.RightScope(joined, right.Scope()))
	expired := query.Or(family.ExpiresAt.Lte(now), entry.RefreshExpiresAt.Lte(now), query.And(entry.RefreshExpiresAt.IsNull(), entry.AccessExpiresAt.Lte(now)))
	return tokenstore.ProjectPruneCandidate(joined).SelectID(family.ID.Value()).SelectSubjectKey(family.SubjectKey.Value()).Query().
		Where(expired).OrderBy(family.SubjectKey.Asc(), family.ID.Asc()).Limit(limit).All(ctx, tx)
}
func (b *Backend) Prune(ctx context.Context, address token.Address, limit int) (uint64, error) {
	scope, err := address.Key()
	if err != nil {
		return 0, err
	}
	if limit < 1 || limit > token.MaxPruneFamilies {
		return 0, fault.New(fault.Invalid, "invalid token prune family limit")
	}
	var count uint64
	err = b.within(ctx, func(tx *database.Tx) error {
		now, err := b.now()
		if err != nil {
			return err
		}
		instant, err := temporal.NewDateTime(now)
		if err != nil {
			return err
		}
		candidates, err := pruneCandidates(ctx, tx, scope, instant, limit)
		if err != nil {
			return err
		}
		var subject tokenstore.Subject
		for _, candidate := range candidates {
			if subject.Key != candidate.SubjectKey {
				locked, present, err := subjectByKey(ctx, tx, address, candidate.SubjectKey, true)
				if err != nil {
					return err
				}
				if !present {
					return fault.New(fault.Invalid, "token family has no subject")
				}
				subject = locked
			}
			found, err := families(scope, subject.Key).ForUpdate().Find(ctx, tx, candidate.ID)
			if err != nil {
				return err
			}
			family, present := found.Get()
			if !present {
				continue
			}
			current, err := currentRecord(ctx, tx, address, subject, family)
			if err != nil {
				return err
			}
			now, err := b.now()
			if err != nil {
				return err
			}
			if current.Live(now) {
				continue
			}
			if _, err := families(scope, subject.Key).Delete(ctx, tx, family.ID); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
