package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sessionstore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func validateCredential(address session.Address, hash session.Digest) error {
	if err := address.Validate(); err != nil {
		return err
	}
	if hash.IsZero() {
		return fault.New(fault.Invalid, "session credential hash is empty")
	}
	return nil
}

// credential takes the stable subject lock before locking/re-reading its session
// row. Rotation/revocation/touch share this order. The initial lookup is only a
// locator; it cannot authenticate a row that changed while waiting for the lock.
func credential(ctx context.Context, tx *database.Tx, address session.Address, hash session.Digest) (sessionstore.Subject, sessionstore.Entry, bool, error) {
	scope, err := address.Key()
	if err != nil {
		return sessionstore.Subject{}, sessionstore.Entry{}, false, err
	}
	f := sessionstore.EntryFields()
	locator, err := entries(scope, "").Where(f.SecretHash.Eq(hash.Hex())).First(ctx, tx)
	if err != nil {
		return sessionstore.Subject{}, sessionstore.Entry{}, false, err
	}
	first, present := locator.Get()
	if !present {
		return sessionstore.Subject{}, sessionstore.Entry{}, false, nil
	}
	subject, present, err := lockSubjectKey(ctx, tx, address, first.SubjectKey)
	if err != nil || !present {
		return sessionstore.Subject{}, sessionstore.Entry{}, false, err
	}
	locked, err := entries(scope, subject.Key).Where(f.SecretHash.Eq(hash.Hex())).ForUpdate().Find(ctx, tx, first.ID)
	if err != nil {
		return sessionstore.Subject{}, sessionstore.Entry{}, false, err
	}
	row, present := locked.Get()
	return subject, row, present, nil
}
func (b *Backend) Lookup(ctx context.Context, address session.Address, hash session.Digest, touch bool) (value.Optional[session.Record], error) {
	if err := validateCredential(address, hash); err != nil {
		return value.Optional[session.Record]{}, err
	}
	var result value.Optional[session.Record]
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, row, present, err := credential(ctx, tx, address, hash)
		if err != nil || !present {
			return err
		}
		current, err := record(address, subject, row)
		if err != nil {
			return err
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		if !current.Live(now) {
			return nil
		}
		if touch {
			updated, live, err := current.Touch(now)
			if err != nil || !live {
				return err
			}
			if updated.LastSeenAt != current.LastSeenAt || updated.IdleExpiresAt != current.IdleExpiresAt {
				row, err = entries(subject.Scope, subject.Key).Update(ctx, tx, row.ID, sessionstore.EntryDraft{}.SetLastSeenAt(updated.LastSeenAt).SetIdleExpiresAt(updated.IdleExpiresAt))
				if err != nil {
					return err
				}
				current, err = record(address, subject, row)
				if err != nil {
					return err
				}
			}
		}
		result = value.Set(current)
		return nil
	})
	if err != nil {
		return value.Optional[session.Record]{}, err
	}
	return result, nil
}
func (b *Backend) Rotate(ctx context.Context, address session.Address, old, next session.Digest) (value.Optional[session.Record], error) {
	if err := validateCredential(address, old); err != nil {
		return value.Optional[session.Record]{}, err
	}
	if next.IsZero() || next.Equal(old) {
		return value.Optional[session.Record]{}, fault.New(fault.Invalid, "session rotation requires a new secret hash")
	}
	var result value.Optional[session.Record]
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, row, present, err := credential(ctx, tx, address, old)
		if err != nil || !present {
			return err
		}
		current, err := record(address, subject, row)
		if err != nil {
			return err
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		current, live, err := current.Touch(now)
		if err != nil || !live {
			return err
		}
		draft := sessionstore.EntryDraft{}.SetSecretHash(next.Hex()).SetLastSeenAt(current.LastSeenAt).SetIdleExpiresAt(current.IdleExpiresAt)
		row, err = entries(subject.Scope, subject.Key).Update(ctx, tx, row.ID, draft)
		if err != nil {
			return err
		}
		updated, err := record(address, subject, row)
		if err != nil {
			return err
		}
		result = value.Set(updated)
		return nil
	})
	if err != nil {
		return value.Optional[session.Record]{}, err
	}
	return result, nil
}
func (b *Backend) Revoke(ctx context.Context, address session.Address, hash session.Digest) (bool, error) {
	if err := validateCredential(address, hash); err != nil {
		return false, err
	}
	removed := false
	err := b.within(ctx, func(tx *database.Tx) error {
		subject, row, present, err := credential(ctx, tx, address, hash)
		if err != nil || !present {
			return err
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
