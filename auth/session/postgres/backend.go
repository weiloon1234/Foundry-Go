// Package postgres persists session credentials through Foundry-owned PostgreSQL
// models and explicit migrations. It borrows an existing pool and never migrates
// or connects during construction.
package postgres

import (
	"context"
	"reflect"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sessionstore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Config explicitly selects the schema used by both migrations and runtime.
// Clock is server-side application time, sampled after taking a subject lock.
// Instances sharing a store need synchronized clocks. Tests may inject testkit.Clock.
type Config struct {
	Schema string
	Clock  clock.Clock
}

func DefaultConfig() Config { return Config{Schema: "public", Clock: clock.System{}} }
func (c Config) Validate() error {
	if !sqlname.Valid(c.Schema) || c.Clock == nil {
		return fault.New(fault.Invalid, "session PostgreSQL adapter requires a schema and clock")
	}
	v := reflect.ValueOf(c.Clock)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if v.IsNil() {
			return fault.New(fault.Invalid, "session PostgreSQL clock is nil")
		}
	}
	return nil
}

type Backend struct {
	db     *database.DB
	config Config
}

func New(db *database.DB, config Config) (*Backend, error) {
	if db == nil {
		return nil, fault.New(fault.Invalid, "session PostgreSQL adapter requires a database pool")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Backend{db: db, config: config}, nil
}

var _ session.Backend = (*Backend)(nil)

func (b *Backend) within(ctx context.Context, fn func(*database.Tx) error) error {
	if b == nil || b.db == nil || ctx == nil {
		return fault.New(fault.Invalid, "session PostgreSQL operation requires a backend and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.db.Transaction(ctx, func(tx *database.Tx) error {
		// Infrastructure statement: values cannot parameterize schema identifiers.
		// Schema passed the shared SQL identifier validator at construction.
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+b.config.Schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	}, database.TxOptions{Isolation: database.ReadCommitted})
}
func (b *Backend) now() (time.Time, error) {
	now := b.config.Clock.Now().UTC().Truncate(time.Microsecond)
	instant, err := temporal.NewDateTime(now)
	if err != nil {
		return time.Time{}, err
	}
	if instant.IsZero() {
		return time.Time{}, fault.New(fault.Invalid, "session clock returned zero time")
	}
	return now, nil
}
func entries(scope, subject string) sessionstore.EntryQuery {
	f := sessionstore.EntryFields()
	q := sessionstore.QueryFoundrySessions().Where(f.Scope.Eq(scope))
	if subject != "" {
		q = q.Where(f.SubjectKey.Eq(subject))
	}
	return q
}
func lockSubject(ctx context.Context, tx *database.Tx, address session.Address, identity model.Identity, create bool) (sessionstore.Subject, bool, error) {
	scope, err := address.Key()
	if err != nil {
		return sessionstore.Subject{}, false, err
	}
	key, err := address.SubjectKey(identity)
	if err != nil {
		return sessionstore.Subject{}, false, err
	}
	if create {
		data, err := value.NewJSON(identity)
		if err != nil {
			return sessionstore.Subject{}, false, err
		}
		draft := sessionstore.SubjectDraft{}.SetKey(key).SetScope(scope).SetIdentity(data)
		if _, err := sessionstore.QueryFoundrySessionSubjects().Upsert(ctx, tx, draft, query.OnConflict(sessionstore.SubjectFields().Key).DoNothing()); err != nil {
			return sessionstore.Subject{}, false, err
		}
	}
	return lockSubjectKey(ctx, tx, address, key)
}
func lockSubjectKey(ctx context.Context, tx *database.Tx, address session.Address, key string) (sessionstore.Subject, bool, error) {
	scope, err := address.Key()
	if err != nil {
		return sessionstore.Subject{}, false, err
	}
	found, err := sessionstore.QueryFoundrySessionSubjects().Where(sessionstore.SubjectFields().Scope.Eq(scope)).ForUpdate().Find(ctx, tx, key)
	if err != nil {
		return sessionstore.Subject{}, false, err
	}
	subject, present := found.Get()
	if !present {
		return sessionstore.Subject{}, false, nil
	}
	identity, err := subject.Identity.Decode()
	if err != nil {
		return sessionstore.Subject{}, false, err
	}
	expected, err := address.SubjectKey(identity)
	if err != nil {
		return sessionstore.Subject{}, false, err
	}
	if key != expected || subject.Scope != scope {
		return sessionstore.Subject{}, false, fault.New(fault.Invalid, "stored session subject scope is inconsistent")
	}
	return subject, true, nil
}
func record(address session.Address, subject sessionstore.Subject, row sessionstore.Entry) (session.Record, error) {
	scope, err := address.Key()
	if err != nil {
		return session.Record{}, err
	}
	if row.Scope != scope || row.SubjectKey != subject.Key || subject.Scope != scope {
		return session.Record{}, fault.New(fault.Invalid, "stored session address does not match subject")
	}
	identity, err := subject.Identity.Decode()
	if err != nil {
		return session.Record{}, err
	}
	expected, err := address.SubjectKey(identity)
	if err != nil {
		return session.Record{}, err
	}
	if subject.Key != expected {
		return session.Record{}, fault.New(fault.Invalid, "stored session subject does not match its identity")
	}
	hash, err := session.ParseDigest(row.SecretHash)
	if err != nil {
		return session.Record{}, err
	}
	result := session.Record{ID: model.IDFromBytes[session.Record](row.ID.Bytes()), Address: address, Subject: identity, Hash: hash, Assurance: auth.Assurance(row.Assurance), Remember: row.Remember, Lifetime: session.Lifetime{Idle: time.Duration(row.IdleNanos), Absolute: row.ExpiresAt.UTC().Sub(row.CreatedAt.UTC()), Sliding: row.Sliding}, CreatedAt: row.CreatedAt, LastSeenAt: row.LastSeenAt, IdleExpiresAt: row.IdleExpiresAt, ExpiresAt: row.ExpiresAt}
	return result, result.Validate(address)
}
func entryDraft(r session.Record, scope, subject string) sessionstore.EntryDraft {
	return sessionstore.EntryDraft{}.
		SetID(model.IDFromBytes[sessionstore.Entry](r.ID.Bytes())).SetScope(scope).SetSubjectKey(subject).
		SetSecretHash(r.Hash.Hex()).SetAssurance(uint8(r.Assurance)).SetRemember(r.Remember).
		SetSliding(r.Lifetime.Sliding).SetIdleNanos(int64(r.Lifetime.Idle)).
		SetCreatedAt(r.CreatedAt).SetLastSeenAt(r.LastSeenAt).SetIdleExpiresAt(r.IdleExpiresAt).SetExpiresAt(r.ExpiresAt)
}
func subjectRows(ctx context.Context, tx *database.Tx, scope, subject string) ([]sessionstore.Entry, error) {
	f := sessionstore.EntryFields()
	rows, err := entries(scope, subject).OrderBy(f.CreatedAt.Asc(), f.ID.Asc()).Limit(session.MaxSessions+1).All(ctx, tx)
	if err != nil {
		return nil, err
	}
	if len(rows) > session.MaxSessions {
		return nil, fault.New(fault.Invalid, "stored session subject exceeds capacity")
	}
	return rows, nil
}
