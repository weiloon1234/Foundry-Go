package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	credentialruntime "github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

var _ session.CompletionBackend = (*Backend)(nil)

// ConsumePendingIn deletes only the exact, still-live pending credential under
// its subject lock. The caller already holds model/factor locks. Pool ownership
// and schema restoration follow the shared savepoint boundary. Nothing commits.
func (b *Backend) ConsumePendingIn(ctx context.Context, tx *database.Tx, address session.Address, expected session.Record) error {
	if b == nil {
		return fault.New(fault.Invalid, "credential backend is missing")
	}
	if err := expected.Validate(address); err != nil {
		return err
	}
	if expected.Assurance != auth.PendingMFA {
		return auth.Unauthenticated
	}
	return credentialruntime.InSchema(ctx, tx, b.db, b.config.Schema, func(child *database.Tx) error {
		subject, row, present, err := credential(ctx, child, address, expected.Hash)
		if err != nil {
			return err
		}
		if !present {
			return auth.Unauthenticated
		}
		current, err := record(address, subject, row)
		if err != nil {
			return err
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		if current.ID != expected.ID || current.Subject != expected.Subject || !current.Hash.Equal(expected.Hash) || current.Assurance != auth.PendingMFA || current.CreatedAt != expected.CreatedAt || current.IdleExpiresAt != expected.IdleExpiresAt || current.ExpiresAt != expected.ExpiresAt || now.Before(current.CreatedAt.UTC()) || !current.Live(now) {
			return auth.Unauthenticated
		}
		_, err = entries(subject.Scope, subject.Key).Delete(ctx, child, row.ID)
		return err
	})
}

// CreateBefore uses ordinary creation and rechecks the pending credential's
// deadline after insertion, before the transaction returns to commit. Expiry
// rolls back the inserted credential and all joined factor/consumption writes.
func (b *Backend) CreateBefore(ctx context.Context, address session.Address, creation session.Creation, deadline temporal.DateTime, check func(context.Context, *database.Tx) error) (session.Record, error) {
	if b == nil || check == nil || deadline.IsZero() || deadline.UTC().Unix() < 0 || deadline.UTC().Nanosecond()%1000 != 0 || creation.Assurance != auth.Authenticated {
		return session.Record{}, fault.New(fault.Invalid, "MFA creation requires a full proof check and deadline")
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
