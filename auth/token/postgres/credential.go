package postgres

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/tokenstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func credentialQuery(scope string, hash token.Digest, refresh bool) tokenstore.EntryQuery {
	q := entries(scope, model.ID[tokenstore.Family]{})
	if refresh {
		return q.Where(tokenstore.EntryFields().RefreshHash.Eq(hash.Hex()))
	}
	return q.Where(tokenstore.EntryFields().AccessHash.Eq(hash.Hex()))
}

// credential uses an initial hash lookup solely as a locator. Mutations lock the
// stable subject, then re-read the family and the exact generation/hash. Every
// writer follows this order, including when a consumed refresh token is replayed.
func readCredential(ctx context.Context, tx *database.Tx, address token.Address, hash token.Digest, refresh, lock bool) (tokenstore.Subject, tokenstore.Family, tokenstore.Entry, bool, error) {
	scope, err := address.Key()
	if err != nil {
		return tokenstore.Subject{}, tokenstore.Family{}, tokenstore.Entry{}, false, err
	}
	q := credentialQuery(scope, hash, refresh)
	found, err := q.First(ctx, tx)
	if err != nil {
		return tokenstore.Subject{}, tokenstore.Family{}, tokenstore.Entry{}, false, err
	}
	entry, present := found.Get()
	if !present {
		return tokenstore.Subject{}, tokenstore.Family{}, tokenstore.Entry{}, false, nil
	}
	foundFamily, err := families(scope, "").Find(ctx, tx, entry.FamilyID)
	if err != nil {
		return tokenstore.Subject{}, tokenstore.Family{}, tokenstore.Entry{}, false, err
	}
	family, present := foundFamily.Get()
	if !present {
		return tokenstore.Subject{}, tokenstore.Family{}, tokenstore.Entry{}, false, nil
	}
	subject, present, err := subjectByKey(ctx, tx, address, family.SubjectKey, lock)
	if err != nil || !present {
		return tokenstore.Subject{}, tokenstore.Family{}, tokenstore.Entry{}, false, err
	}
	if lock {
		foundFamily, err = families(scope, subject.Key).ForUpdate().Find(ctx, tx, family.ID)
		if err != nil {
			return tokenstore.Subject{}, tokenstore.Family{}, tokenstore.Entry{}, false, err
		}
		family, present = foundFamily.Get()
		if !present {
			return tokenstore.Subject{}, tokenstore.Family{}, tokenstore.Entry{}, false, nil
		}
		found, err = q.ForUpdate().Find(ctx, tx, entry.ID)
		if err != nil {
			return tokenstore.Subject{}, tokenstore.Family{}, tokenstore.Entry{}, false, err
		}
		entry, present = found.Get()
		if !present {
			return tokenstore.Subject{}, tokenstore.Family{}, tokenstore.Entry{}, false, nil
		}
	}
	return subject, family, entry, true, nil
}

// Lookup is the per-request read path: one schema-qualified statement joins the
// generation, family and subject without a transaction or row lock, so parallel
// requests never serialize on the subject. Expiry is checked against the clock
// sampled after the read; a revocation committing concurrently applies from the
// next auth scope. Touch records activity with one conditional UPDATE that
// cannot revive a refreshed, revoked or expired generation.
func (b *Backend) Lookup(ctx context.Context, address token.Address, hash token.Digest, touch bool) (value.Optional[token.Record], error) {
	return b.lookup(ctx, address, hash, touch, 0)
}

// LookupWithin also accepts the generation immediately before the current one
// while its successor is younger than grace and its own access is live.
func (b *Backend) LookupWithin(ctx context.Context, address token.Address, hash token.Digest, grace time.Duration) (value.Optional[token.Record], error) {
	if grace <= 0 {
		return value.Optional[token.Record]{}, fault.New(fault.Invalid, "token access grace must be positive")
	}
	return b.lookup(ctx, address, hash, false, grace)
}

func (b *Backend) lookup(ctx context.Context, address token.Address, hash token.Digest, touch bool, grace time.Duration) (value.Optional[token.Record], error) {
	if err := validateCredential(address, hash); err != nil {
		return value.Optional[token.Record]{}, err
	}
	if b == nil || b.db == nil || ctx == nil {
		return value.Optional[token.Record]{}, fault.New(fault.Invalid, "token PostgreSQL operation requires a backend and context")
	}
	scope, err := address.Key()
	if err != nil {
		return value.Optional[token.Record]{}, err
	}
	rows, err := b.readTokens(ctx, b.db, 1, `WHERE g.scope = $1 AND g.access_hash = $2`, scope, hash.Hex())
	if err != nil || len(rows) == 0 {
		return value.Optional[token.Record]{}, err
	}
	row := rows[0]
	current, err := record(address, row.subject, row.family, row.entry)
	if err != nil {
		return value.Optional[token.Record]{}, err
	}
	switch {
	case row.entry.Generation == row.family.Generation:
	case grace > 0 && row.entry.Generation+1 == row.family.Generation:
		successor, present := row.successor.Get()
		if !present {
			return value.Optional[token.Record]{}, fault.New(fault.Invalid, "stored token family has no current generation")
		}
		current.SupersededAt = value.Set(successor)
		if err := current.Validate(address); err != nil {
			return value.Optional[token.Record]{}, err
		}
	default:
		return value.Optional[token.Record]{}, nil
	}
	now, err := b.now()
	if err != nil {
		return value.Optional[token.Record]{}, err
	}
	if !current.LiveAccessWithin(now, grace) {
		return value.Optional[token.Record]{}, nil
	}
	if touch && now.After(current.LastSeenAt.UTC()) {
		seen, err := temporal.NewDateTime(now)
		if err != nil {
			return value.Optional[token.Record]{}, err
		}
		result, err := b.db.Exec(ctx, `UPDATE `+b.generationTable()+` g SET last_seen_at = $3 FROM `+b.familyTable()+` f WHERE g.id = $1 AND g.scope = $2 AND f.id = g.family_id AND f.scope = g.scope AND f.generation = g.generation AND g.last_seen_at < $3 AND g.access_expires_at > $3 AND f.expires_at > $3`,
			row.entry.ID.String(), scope, now)
		if err != nil {
			return value.Optional[token.Record]{}, err
		}
		if result.RowsAffected == 1 {
			current.LastSeenAt = seen
		}
	}
	return value.Set(current), nil
}

// revokeReplayed revokes the family of a refresh digest consumed before the
// previous generation. Only the compact consumed set still holds such digests,
// and replaying one must revoke its family exactly like any other reuse.
func (b *Backend) revokeReplayed(ctx context.Context, tx *database.Tx, address token.Address, hash token.Digest) error {
	_, _, err := b.revokeConsumed(ctx, tx, address, hash)
	return err
}

// revokeConsumed deletes the family of a consumed refresh digest under its
// subject lock and returns that subject, shared by replay detection and logout.
func (b *Backend) revokeConsumed(ctx context.Context, tx *database.Tx, address token.Address, hash token.Digest) (tokenstore.Subject, bool, error) {
	scope, err := address.Key()
	if err != nil {
		return tokenstore.Subject{}, false, err
	}
	var family, subjectKey string
	err = database.ForEach(ctx, tx, `SELECT f.id::text, f.subject_key FROM `+b.consumedTable()+` c JOIN `+b.familyTable()+` f ON f.id = c.family_id WHERE c.refresh_hash = $1 AND f.scope = $2`, []any{hash.Hex(), scope}, func(row database.Row) ([2]string, error) {
		var result [2]string
		return result, row.Scan(&result[0], &result[1])
	}, func(row [2]string) error {
		family, subjectKey = row[0], row[1]
		return nil
	})
	if err != nil || family == "" {
		return tokenstore.Subject{}, false, err
	}
	subject, present, err := subjectByKey(ctx, tx, address, subjectKey, true)
	if err != nil || !present {
		return tokenstore.Subject{}, false, err
	}
	result, err := tx.Exec(ctx, `DELETE FROM `+b.familyTable()+` WHERE id = $1 AND scope = $2 AND subject_key = $3`, family, subject.Scope, subject.Key)
	if err != nil {
		return tokenstore.Subject{}, false, err
	}
	return subject, result.RowsAffected == 1, nil
}

var _ token.RefreshRevocationBackend = (*Backend)(nil)

// RevokeRefresh deletes the family of a current, previous or consumed refresh
// digest in one transaction, locking its subject before the family like every
// other writer. Expired families are removed as well.
func (b *Backend) RevokeRefresh(ctx context.Context, address token.Address, hash token.Digest) (value.Optional[model.Identity], error) {
	if err := validateCredential(address, hash); err != nil {
		return value.Optional[model.Identity]{}, err
	}
	var removed value.Optional[model.Identity]
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, family, _, found, err := readCredential(ctx, tx, address, hash, true, true)
		if err != nil {
			return err
		}
		if found {
			// readCredential locked the subject and this family row.
			if _, err := families(subject.Scope, subject.Key).Delete(ctx, tx, family.ID); err != nil {
				return err
			}
		} else {
			consumed, present, err := b.revokeConsumed(ctx, tx, address, hash)
			if err != nil || !present {
				return err
			}
			subject = consumed
		}
		identity, err := subject.Identity.Decode()
		if err != nil {
			return err
		}
		removed = value.Set(identity)
		return nil
	})
	if err != nil {
		return value.Optional[model.Identity]{}, err
	}
	return removed, nil
}

// retire keeps the family's current and immediately previous generations. Older
// generations move to the compact consumed-refresh set, which still identifies
// reuse, so rotation no longer accumulates full generation rows.
func (b *Backend) retire(ctx context.Context, tx *database.Tx, family tokenstore.Family, previous uint32) error {
	if _, err := tx.Exec(ctx, `INSERT INTO `+b.consumedTable()+` (refresh_hash, family_id) SELECT refresh_hash, family_id FROM `+b.generationTable()+` WHERE family_id = $1 AND scope = $2 AND generation < $3 AND refresh_hash IS NOT NULL ON CONFLICT (refresh_hash) DO NOTHING`, family.ID.String(), family.Scope, int64(previous)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM `+b.generationTable()+` WHERE family_id = $1 AND scope = $2 AND generation < $3`, family.ID.String(), family.Scope, int64(previous))
	return err
}

func (b *Backend) Refresh(ctx context.Context, address token.Address, old, access, refresh token.Digest) (value.Optional[token.Record], error) {
	if err := validateCredential(address, old); err != nil {
		return value.Optional[token.Record]{}, err
	}
	if access.IsZero() || refresh.IsZero() || access.Equal(refresh) || old.Equal(access) || old.Equal(refresh) {
		return value.Optional[token.Record]{}, fault.New(fault.Invalid, "refresh requires distinct new hashes")
	}
	var result value.Optional[token.Record]
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, family, entry, present, err := readCredential(ctx, tx, address, old, true, true)
		if err != nil {
			return err
		}
		if !present {
			return b.revokeReplayed(ctx, tx, address, old)
		}
		current, err := record(address, subject, family, entry)
		if err != nil {
			return err
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		// Returning nil commits revocation even though the runtime later reports an
		// invalid refresh credential. Returning auth.Unauthenticated here would undo it.
		if entry.Generation != family.Generation || !current.LiveRefresh(now) || current.Generation >= current.RotationLimit {
			_, err := families(subject.Scope, subject.Key).Delete(ctx, tx, family.ID)
			return err
		}
		next, live, err := current.Refreshed(now, access, refresh)
		if err != nil {
			return err
		}
		if !live {
			return fault.New(fault.Invalid, "live token could not refresh under its family lock")
		}
		draft, err := entryDraft(next, subject.Scope)
		if err != nil {
			return err
		}
		made, err := tokenstore.QueryFoundryTokenGenerations().Create(ctx, tx, draft)
		if err != nil {
			return err
		}
		family, err = families(subject.Scope, subject.Key).Update(ctx, tx, family.ID, tokenstore.FamilyDraft{}.SetGeneration(next.Generation))
		if err != nil {
			return err
		}
		if err := b.retire(ctx, tx, family, current.Generation); err != nil {
			return err
		}
		next, err = record(address, subject, family, made)
		if err != nil {
			return err
		}
		result = value.Set(next)
		return nil
	})
	if err != nil {
		return value.Optional[token.Record]{}, err
	}
	return result, nil
}

func (b *Backend) Revoke(ctx context.Context, address token.Address, hash token.Digest) (bool, error) {
	if err := validateCredential(address, hash); err != nil {
		return false, err
	}
	var removed bool
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, family, entry, present, err := readCredential(ctx, tx, address, hash, false, true)
		if err != nil || !present {
			return err
		}
		// The previous generation's access token still authenticates during the
		// refresh grace, so a logout with it (for example right after a refresh)
		// must end the family too. Older generations are not retained as rows.
		if entry.Generation != family.Generation && entry.Generation+1 != family.Generation {
			return nil
		}
		if _, err := record(address, subject, family, entry); err != nil {
			return err
		}
		if _, err := families(subject.Scope, subject.Key).Delete(ctx, tx, family.ID); err != nil {
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
