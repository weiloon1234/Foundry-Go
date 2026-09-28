// Package postgres stores encrypted MFA factors through explicit migrations and
// generated model queries. It borrows an existing pool; constructors do no I/O.
package postgres

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/mfastore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Config's schema applies to factor and domain-model callbacks. Instances must
// use synchronized clocks. All callbacks use this backend's supplied transaction.
type Config struct {
	Schema string
	Clock  clock.Clock
}

func DefaultConfig() Config { return Config{Schema: "public", Clock: clock.System{}} }
func (c Config) Validate() error {
	if !sqlname.Valid(c.Schema) || credential.IsNil(c.Clock) {
		return fault.New(fault.Invalid, "MFA PostgreSQL adapter requires a schema and clock")
	}
	return nil
}

type Backend struct {
	db     *database.DB
	config Config
}

func New(db *database.DB, config Config) (*Backend, error) {
	if db == nil {
		return nil, fault.New(fault.Invalid, "MFA PostgreSQL adapter requires a database pool")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Backend{db: db, config: config}, nil
}

var _ mfa.Backend = (*Backend)(nil)

func (b *Backend) within(ctx context.Context, fn func(*database.Tx) error) error {
	if b == nil || b.db == nil || ctx == nil {
		return fault.New(fault.Invalid, "MFA operation requires a backend and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.db.Transaction(ctx, func(tx *database.Tx) error {
		// Validated schema selection is infrastructure SQL; row operations are typed.
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+b.config.Schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	}, database.TxOptions{Isolation: database.ReadCommitted})
}
func (b *Backend) now() (temporal.DateTime, error) {
	now, err := temporal.NewDateTime(b.config.Clock.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return temporal.DateTime{}, err
	}
	if now.UTC().Unix() < 0 {
		return temporal.DateTime{}, fault.New(fault.Invalid, "invalid MFA server time")
	}
	return now, nil
}
func factors(scope string) mfastore.FactorQuery {
	return mfastore.QueryFoundryMfaFactors().Where(mfastore.FactorFields().Scope.Eq(scope))
}

func (b *Backend) Within(ctx context.Context, address mfa.Address, identity model.Identity, prepare func(context.Context, *database.Tx) error, change func(context.Context, *database.Tx, value.Optional[mfa.Record], temporal.DateTime) (mfa.Change, error)) (value.Optional[mfa.Record], error) {
	return b.mutate(ctx, address, identity, prepare, change, func(fn func(*database.Tx) error) error { return b.within(ctx, fn) })
}

func (b *Backend) WithinIn(ctx context.Context, tx *database.Tx, address mfa.Address, identity model.Identity, prepare func(context.Context, *database.Tx) error, change func(context.Context, *database.Tx, value.Optional[mfa.Record], temporal.DateTime) (mfa.Change, error)) (value.Optional[mfa.Record], error) {
	if b == nil {
		return value.Optional[mfa.Record]{}, fault.New(fault.Invalid, "MFA backend is missing")
	}
	return b.mutate(ctx, address, identity, prepare, change, func(fn func(*database.Tx) error) error {
		return credential.InSchema(ctx, tx, b.db, b.config.Schema, fn)
	})
}

var _ mfa.TransactionalBackend = (*Backend)(nil)

func (b *Backend) mutate(ctx context.Context, address mfa.Address, identity model.Identity, prepare func(context.Context, *database.Tx) error, change func(context.Context, *database.Tx, value.Optional[mfa.Record], temporal.DateTime) (mfa.Change, error), within func(func(*database.Tx) error) error) (value.Optional[mfa.Record], error) {
	var result value.Optional[mfa.Record]
	key, err := address.SubjectKey(identity)
	if err != nil {
		return result, err
	}
	scope, err := address.Key()
	if err != nil {
		return result, err
	}
	if prepare == nil || change == nil {
		return result, fault.New(fault.Invalid, "MFA transaction requires preparation and mutation")
	}
	err = within(func(tx *database.Tx) error {
		// The domain model is the stable serialization row, including first enroll.
		if err := prepare(ctx, tx); err != nil {
			return err
		}
		found, err := factors(scope).ForUpdate().Find(ctx, tx, key)
		if err != nil {
			return err
		}
		var current value.Optional[mfa.Record]
		if row, present := found.Get(); present {
			record, err := decode(address, row)
			if err != nil {
				return err
			}
			current = value.Set(record)
		}
		now, err := b.now()
		if err != nil {
			return err
		}
		mutation, err := change(ctx, tx, current, now)
		if err != nil {
			return err
		}
		if err := mutation.Validate(address, identity); err != nil {
			return err
		}
		if err := b.checkDeadline(mutation, now); err != nil {
			return err
		}
		if mutation.Removes() {
			if found.IsSet() {
				if _, err := factors(scope).Delete(ctx, tx, key); err != nil {
					return err
				}
			}
		} else {
			next, _ := mutation.Next().Get()
			draft, err := encode(next)
			if err != nil {
				return err
			}
			var row mfastore.Factor
			if found.IsSet() {
				row, err = factors(scope).Update(ctx, tx, key, draft.UnsetKey())
			} else {
				row, err = mfastore.QueryFoundryMfaFactors().Create(ctx, tx, draft)
			}
			if err != nil {
				return err
			}
			actual, err := decode(address, row)
			if err != nil {
				return err
			}
			result = value.Set(actual)
		}
		return b.checkDeadline(mutation, now)
	})
	if err != nil {
		return value.Optional[mfa.Record]{}, err
	}
	return result, nil
}
func (b *Backend) checkDeadline(change mfa.Change, started temporal.DateTime) error {
	now, err := b.now()
	if err != nil {
		return err
	}
	if now.UTC().Before(started.UTC()) {
		return fault.New(fault.Invalid, "MFA clock moved backwards during mutation")
	}
	if until, present := change.Deadline().Get(); present && !now.UTC().Before(until.UTC()) {
		return auth.Unauthenticated
	}
	return nil
}
