package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/tokenstore"
	"github.com/weiloon1234/Foundry-Go/model"
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

// List reads the subject's live families with their current generation in one
// statement without locking the subject, so listing never blocks issuance,
// refresh or request authentication.
func (b *Backend) List(ctx context.Context, address token.Address, identity model.Identity, limit int) ([]token.Record, error) {
	key, err := address.SubjectKey(identity)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > token.MaxTokens {
		return nil, fault.New(fault.Invalid, "invalid token listing limit")
	}
	if b == nil || b.db == nil || ctx == nil {
		return nil, fault.New(fault.Invalid, "token PostgreSQL operation requires a backend and context")
	}
	scope, err := address.Key()
	if err != nil {
		return nil, err
	}
	now, err := b.now()
	if err != nil {
		return nil, err
	}
	rows, err := b.readTokens(ctx, b.db, token.MaxTokens, `WHERE f.scope = $1 AND f.subject_key = $2 AND g.generation = f.generation AND NOT `+dead("$3")+` ORDER BY f.created_at, f.id LIMIT $4`, scope, key, now, token.MaxTokens+1)
	if err != nil {
		return nil, err
	}
	var result []token.Record
	for _, row := range rows {
		current, err := record(address, row.subject, row.family, row.entry)
		if err != nil {
			return nil, err
		}
		if current.Live(now) {
			if len(result) >= limit {
				return nil, fault.New(fault.Invalid, "live token listing exceeds requested limit")
			}
			result = append(result, current)
		}
	}
	return result, nil
}

// Prune deletes at most limit expired families of this address, with their
// generations and consumed digests, in one set-based statement. Candidates come
// from the family absolute-expiry and generation expiry indexes; the dead
// predicate is rechecked on each locked family against its current generation,
// and families locked by a concurrent refresh or revocation are skipped.
// Subject rows remain for issuance/revocation serialization.
func (b *Backend) Prune(ctx context.Context, address token.Address, limit int) (uint64, error) {
	scope, err := address.Key()
	if err != nil {
		return 0, err
	}
	if limit < 1 || limit > token.MaxPruneFamilies {
		return 0, fault.New(fault.Invalid, "invalid token prune family limit")
	}
	if b == nil || b.db == nil || ctx == nil {
		return 0, fault.New(fault.Invalid, "token PostgreSQL operation requires a backend and context")
	}
	now, err := b.now()
	if err != nil {
		return 0, err
	}
	families, generations := b.familyTable(), b.generationTable()
	current := ` JOIN ` + families + ` f ON f.id = g.family_id AND f.scope = g.scope AND f.generation = g.generation`
	// Both CTEs are evaluated once: an IN subquery may be rescanned per row, and
	// its LIMIT would then no longer bound the delete.
	result, err := b.db.Exec(ctx, `WITH candidates AS MATERIALIZED (`+
		`(SELECT f.id FROM `+families+` f WHERE f.scope = $1 AND f.expires_at <= $2 ORDER BY f.expires_at LIMIT $3) UNION `+
		`(SELECT g.family_id FROM `+generations+` g`+current+` WHERE g.scope = $1 AND g.refresh_expires_at <= $2 ORDER BY g.refresh_expires_at LIMIT $3) UNION `+
		`(SELECT g.family_id FROM `+generations+` g`+current+` WHERE g.scope = $1 AND g.refresh_expires_at IS NULL AND g.access_expires_at <= $2 ORDER BY g.access_expires_at LIMIT $3)`+
		`), doomed AS MATERIALIZED (SELECT f.id FROM `+families+` f JOIN `+generations+` g ON g.family_id = f.id AND g.scope = f.scope AND g.generation = f.generation `+
		`WHERE f.scope = $1 AND f.id IN (SELECT id FROM candidates) AND `+dead("$2")+` LIMIT $3 FOR UPDATE OF f SKIP LOCKED) `+
		`DELETE FROM `+families+` t USING doomed WHERE t.id = doomed.id AND t.scope = $1`, scope, now, limit)
	if err != nil {
		return 0, err
	}
	return affected(result), nil
}
