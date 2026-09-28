// Package postgres persists single-use recovery credentials through explicit
// migrations and generated queries. It borrows the application's existing pool.
package postgres

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/challengestore"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Config's schema is shared by challenge and domain-model queries inside the
// callback. Instances must share synchronized clocks. Construction does no I/O.
type Config struct {
	Schema string
	Clock  clock.Clock
}

func DefaultConfig() Config { return Config{Schema: "public", Clock: clock.System{}} }
func (c Config) Validate() error {
	if !sqlname.Valid(c.Schema) || credential.IsNil(c.Clock) {
		return fault.New(fault.Invalid, "challenge PostgreSQL adapter requires a schema and clock")
	}
	return nil
}

type Backend struct {
	db     *database.DB
	config Config
}

func New(db *database.DB, config Config) (*Backend, error) {
	if db == nil {
		return nil, fault.New(fault.Invalid, "challenge PostgreSQL adapter requires a database pool")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Backend{db: db, config: config}, nil
}

var _ challenge.Backend = (*Backend)(nil)

func (b *Backend) within(ctx context.Context, fn func(*database.Tx) error) error {
	if b == nil || b.db == nil || ctx == nil {
		return fault.New(fault.Invalid, "challenge operation requires a backend and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.db.Transaction(ctx, func(tx *database.Tx) error {
		// Validated schema control is infrastructure SQL. All row operations use the
		// shared generated query AST, including application callbacks.
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+b.config.Schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	}, database.TxOptions{Isolation: database.ReadCommitted})
}
func (b *Backend) now() (temporal.DateTime, error) {
	instant, err := temporal.NewDateTime(b.config.Clock.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return temporal.DateTime{}, err
	}
	if instant.IsZero() {
		return temporal.DateTime{}, fault.New(fault.Invalid, "challenge clock returned zero time")
	}
	return instant, nil
}
func entries(scope, subject string) challengestore.EntryQuery {
	f := challengestore.EntryFields()
	q := challengestore.QueryFoundryChallenges().Where(f.Scope.Eq(scope))
	if subject != "" {
		q = q.Where(f.SubjectKey.Eq(subject))
	}
	return q
}
func validateCredential(address challenge.Address, hash challenge.Digest) error {
	if err := address.Validate(); err != nil {
		return err
	}
	if hash.IsZero() {
		return fault.New(fault.Invalid, "challenge credential hash is empty")
	}
	return nil
}
