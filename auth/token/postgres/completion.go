package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

var _ token.CompletionBackend = (*Backend)(nil)

// ConsumePendingIn deletes only the exact, still-live pending credential under
// its subject lock. The caller already holds model/factor locks. Pool ownership
// and schema restoration follow the shared savepoint boundary. Nothing commits.
func (b *Backend) ConsumePendingIn(ctx context.Context, tx *database.Tx, address token.Address, expected token.Record) error {
	if b == nil {
		return fault.New(fault.Invalid, "credential backend is missing")
	}
	if err := expected.Validate(address); err != nil {
		return err
	}
	if expected.Assurance != auth.PendingMFA || expected.Mode != token.Challenge {
		return auth.Unauthenticated
	}
	return credential.InSchema(ctx, tx, b.db, b.config.Schema, func(child *database.Tx) error {
		subject, family, entry, present, err := readCredential(ctx, child, address, expected.AccessHash, false, true)
		if err != nil {
			return err
		}
		if !present {
			return auth.Unauthenticated
		}
		if entry.Generation != family.Generation {
			return auth.Unauthenticated
		}
		current, err := record(address, subject, family, entry)
		if err != nil {
			return err
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		if current.ID != expected.ID || current.Subject != expected.Subject || !current.AccessHash.Equal(expected.AccessHash) || current.Assurance != auth.PendingMFA || current.Mode != token.Challenge || current.CreatedAt != expected.CreatedAt || current.AccessExpiresAt != expected.AccessExpiresAt || current.ExpiresAt != expected.ExpiresAt || now.Before(current.CreatedAt.UTC()) || !current.LiveAccess(now) {
			return auth.Unauthenticated
		}
		_, err = families(subject.Scope, subject.Key).Delete(ctx, child, family.ID)
		return err
	})
}

// CreateBefore uses ordinary creation and rechecks the pending credential's
// deadline after insertion, before the transaction returns to commit. Expiry
// rolls back the inserted credential and all joined factor/consumption writes.
func (b *Backend) CreateBefore(ctx context.Context, address token.Address, creation token.Creation, deadline temporal.DateTime, check func(context.Context, *database.Tx) error) (token.Record, error) {
	if b == nil || check == nil || deadline.IsZero() || deadline.UTC().Unix() < 0 || deadline.UTC().Nanosecond()%1000 != 0 || creation.Assurance != auth.Authenticated {
		return token.Record{}, fault.New(fault.Invalid, "MFA creation requires a full proof check and deadline")
	}
	return b.create(ctx, address, creation, check, func() error {
		now, err := b.now()
		if err != nil {
			return err
		}
		if !now.Before(deadline.UTC()) {
			return auth.Unauthenticated
		}
		return nil
	})
}
