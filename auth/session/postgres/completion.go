package postgres

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	credentialruntime "github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sessionstore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ session.CompletionBackend = (*Backend)(nil)
var _ session.ResumptionBackend = (*Backend)(nil)

// ConsumePendingIn deletes only the exact, still-live pending credential under
// its subject lock. The caller already holds model/factor locks. Pool ownership
// and schema restoration follow the shared savepoint boundary. Nothing commits.
func (b *Backend) ConsumePendingIn(ctx context.Context, tx *database.Tx, address session.Address, expected session.Record) error {
	return b.consumeIn(ctx, tx, address, expected, auth.PendingMFA)
}

func (b *Backend) consumeIn(ctx context.Context, tx *database.Tx, address session.Address, expected session.Record, assurance auth.Assurance) error {
	if b == nil {
		return safeerror.NewFault(fault.Invalid, "credential backend is missing")
	}
	if err := expected.Validate(address); err != nil {
		return err
	}
	if expected.Assurance != assurance || expected.Impersonator.IsSet() {
		return auth.Unauthenticated.WithCause(nil)
	}
	return credentialruntime.InSchema(ctx, tx, b.db, b.config.Schema, func(child *database.Tx) error {
		subject, row, present, err := credential(ctx, child, address, expected.Hash)
		if err != nil {
			return err
		}
		if !present {
			return auth.Unauthenticated.WithCause(nil)
		}
		current, err := record(address, subject, row)
		if err != nil {
			return err
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		if current.ID != expected.ID || current.Subject != expected.Subject || !current.Hash.Equal(expected.Hash) || current.Assurance != assurance || current.Impersonator.IsSet() || current.CreatedAt != expected.CreatedAt || current.IdleExpiresAt != expected.IdleExpiresAt || current.ExpiresAt != expected.ExpiresAt || now.Before(current.CreatedAt.UTC()) || !current.Live(now) {
			return auth.Unauthenticated.WithCause(nil)
		}
		_, err = orm.Delete(ctx, child, entries(subject.Scope, subject.Key), sessionstore.Entries.ID, row.ID)
		return err
	})
}

// CreateBefore uses ordinary creation and rechecks the existing credential's
// deadline after insertion, before the transaction returns to commit. Expiry
// rolls back the inserted credential and all joined factor/consumption writes.
func (b *Backend) CreateBefore(ctx context.Context, address session.Address, creation session.Creation, deadline temporal.DateTime, check func(context.Context, *database.Tx) error) (session.Record, error) {
	if b == nil || check == nil || deadline.IsZero() || deadline.UTC().Unix() < 0 || deadline.UTC().Nanosecond()%1000 != 0 || creation.Assurance != auth.Authenticated {
		return session.Record{}, safeerror.NewFault(fault.Invalid, "credential creation requires a full proof check and deadline")
	}
	return b.create(ctx, address, creation, check, func() error {
		now, err := b.now()
		if err != nil {
			return err
		}
		if !now.Before(deadline.UTC()) {
			return auth.Unauthenticated.WithCause(nil)
		}
		return nil
	})
}

// ResumeActor rotates the exact live actor session in place after
// impersonation. The check runs first in the transaction; the subject and row
// are then locked in the order shared by every session writer, and the record
// is revalidated against expected before its secret is replaced.
func (b *Backend) ResumeActor(ctx context.Context, address session.Address, expected session.Record, next session.Digest, check func(context.Context, *database.Tx) error) (value.Optional[session.Record], error) {
	if b == nil || check == nil {
		return value.Optional[session.Record]{}, safeerror.NewFault(fault.Invalid, "session resumption requires a backend and check")
	}
	if err := expected.Validate(address); err != nil {
		return value.Optional[session.Record]{}, err
	}
	if next.IsZero() || next.Equal(expected.Hash) {
		return value.Optional[session.Record]{}, safeerror.NewFault(fault.Invalid, "session resumption requires a new secret hash")
	}
	if expected.Assurance != auth.Authenticated || expected.Impersonator.IsSet() {
		return value.Optional[session.Record]{}, nil
	}
	var result value.Optional[session.Record]
	err := b.within(ctx, func(tx *database.Tx) error {
		if err := check(ctx, tx); err != nil {
			return err
		}
		subject, row, present, err := credential(ctx, tx, address, expected.Hash)
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
		if current.ID != expected.ID || current.Subject != expected.Subject || current.Assurance != auth.Authenticated || current.Impersonator.IsSet() || current.CreatedAt != expected.CreatedAt || current.ExpiresAt != expected.ExpiresAt || current.Remember != expected.Remember || !current.Live(now) {
			return nil
		}
		touched, live, err := current.Touch(now)
		if err != nil || !live {
			return err
		}
		draft := []orm.Assignment[sessionstore.Entry]{orm.TextSet(sessionstore.Entries.SecretHash, next.Hex()), orm.TimeSet(sessionstore.Entries.LastSeenAt, touched.LastSeenAt), orm.TimeSet(sessionstore.Entries.IdleExpiresAt, touched.IdleExpiresAt)}
		row, err = orm.Update(ctx, tx, entries(subject.Scope, subject.Key), sessionstore.Entries.ID, row.ID, draft...)
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
