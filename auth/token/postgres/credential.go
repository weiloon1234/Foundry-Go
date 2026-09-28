package postgres

import (
	"context"

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

// Lookup is read-only unless touch is explicitly requested. Its authorization
// snapshot is the current generation
// observed in this transaction; later revocation applies to the next auth scope.
func (b *Backend) Lookup(ctx context.Context, address token.Address, hash token.Digest, touch bool) (value.Optional[token.Record], error) {
	if err := validateCredential(address, hash); err != nil {
		return value.Optional[token.Record]{}, err
	}
	var result value.Optional[token.Record]
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, family, entry, present, err := readCredential(ctx, tx, address, hash, false, touch)
		if err != nil || !present {
			return err
		}
		if entry.Generation != family.Generation {
			return nil
		}
		current, err := record(address, subject, family, entry)
		if err != nil {
			return err
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		if !current.LiveAccess(now) {
			return nil
		}
		if touch && now.After(current.LastSeenAt.UTC()) {
			seen, err := temporal.NewDateTime(now)
			if err != nil {
				return err
			}
			entry, err = entries(family.Scope, family.ID).Update(ctx, tx, entry.ID, tokenstore.EntryDraft{}.SetLastSeenAt(seen))
			if err != nil {
				return err
			}
			current, err = record(address, subject, family, entry)
			if err != nil {
				return err
			}
		}
		result = value.Set(current)
		return nil
	})
	if err != nil {
		return value.Optional[token.Record]{}, err
	}
	return result, nil
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
		if err != nil || !present {
			return err
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
		if entry.Generation != family.Generation {
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
