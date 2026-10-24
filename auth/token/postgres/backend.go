package postgres

import (
	"context"
	"net/netip"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/internal/tokenstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Config selects the same schema used by explicit migrations. Instances sharing
// a store need synchronized clocks; tests may inject a shared testkit.Clock.
type Config struct {
	Schema string
	Clock  clock.Clock
}

func DefaultConfig() Config { return Config{Schema: "public", Clock: clock.System{}} }
func (c Config) Validate() error {
	if !sqlname.Valid(c.Schema) || credential.IsNil(c.Clock) {
		return fault.New(fault.Invalid, "token PostgreSQL adapter requires a schema and clock")
	}
	return nil
}

// Backend borrows a pool. Mutations own a read-committed transaction; request
// verification and listing are single statements without one. Explicit In
// methods join verified caller transactions. It never retries writes or
// connects at construction.
type Backend struct {
	db     *database.DB
	config Config
}

func New(db *database.DB, config Config) (*Backend, error) {
	if db == nil {
		return nil, fault.New(fault.Invalid, "token PostgreSQL adapter requires a database pool")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Backend{db: db, config: config}, nil
}

var _ token.Backend = (*Backend)(nil)

func (b *Backend) within(ctx context.Context, fn func(*database.Tx) error) error {
	if b == nil || b.db == nil || ctx == nil {
		return fault.New(fault.Invalid, "token PostgreSQL operation requires a backend and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.db.Transaction(ctx, func(tx *database.Tx) error {
		// Generated model queries resolve through this schema; set-based and hot-path
		// statements qualify it explicitly. Schema passed the identifier validator.
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
		return time.Time{}, fault.New(fault.Invalid, "token clock returned zero time")
	}
	return now, nil
}
func families(scope, subject string) orm.Query[tokenstore.Family] {
	f := tokenstore.Families
	q := tokenstore.Families.Query().Where(f.Scope.Eq(scope))
	if subject != "" {
		q = q.Where(f.SubjectKey.Eq(subject))
	}
	return q
}
func entries(scope string, family model.ID[tokenstore.Family]) orm.Query[tokenstore.Entry] {
	f := tokenstore.Entries
	q := tokenstore.Entries.Query().Where(f.Scope.Eq(scope))
	if !family.IsZero() {
		q = q.Where(f.FamilyID.Eq(family))
	}
	return q
}
func validateCredential(address token.Address, hash token.Digest) error {
	if err := address.Validate(); err != nil {
		return err
	}
	if hash.IsZero() {
		return fault.New(fault.Invalid, "token credential hash is empty")
	}
	return nil
}
func lockSubject(ctx context.Context, tx *database.Tx, address token.Address, identity model.Identity, create bool) (tokenstore.Subject, bool, error) {
	scope, err := address.Key()
	if err != nil {
		return tokenstore.Subject{}, false, err
	}
	key, err := address.SubjectKey(identity)
	if err != nil {
		return tokenstore.Subject{}, false, err
	}
	if create {
		data, err := value.NewJSON(identity)
		if err != nil {
			return tokenstore.Subject{}, false, err
		}
		draft := []orm.Assignment[tokenstore.Subject]{tokenstore.Subjects.Key().Set(key), tokenstore.Subjects.Scope.Set(scope), tokenstore.Subjects.Identity.Set(data)}
		if _, err := orm.Upsert(ctx, tx, tokenstore.Subjects.Query(), orm.OnConflict(orm.ConflictKey(tokenstore.Subjects.Key())).DoNothing(), draft...); err != nil {
			return tokenstore.Subject{}, false, err
		}
	}
	return subjectByKey(ctx, tx, address, key, true)
}
func subjectByKey(ctx context.Context, tx *database.Tx, address token.Address, key string, lock bool) (tokenstore.Subject, bool, error) {
	scope, err := address.Key()
	if err != nil {
		return tokenstore.Subject{}, false, err
	}
	q := tokenstore.Subjects.Query().Where(tokenstore.Subjects.Scope.Eq(scope))
	var found value.Optional[tokenstore.Subject]
	if lock {
		found, err = q.Where(tokenstore.Subjects.Key().Eq(key)).ForUpdate().First(ctx, tx)
	} else {
		found, err = q.Where(tokenstore.Subjects.Key().Eq(key)).First(ctx, tx)
	}
	if err != nil {
		return tokenstore.Subject{}, false, err
	}
	subject, present := found.Get()
	if !present {
		return tokenstore.Subject{}, false, nil
	}
	identity, err := subject.Identity.Decode()
	if err != nil {
		return tokenstore.Subject{}, false, err
	}
	expected, err := address.SubjectKey(identity)
	if err != nil {
		return tokenstore.Subject{}, false, err
	}
	if subject.Key != expected || subject.Key != key || subject.Scope != scope {
		return tokenstore.Subject{}, false, fault.New(fault.Invalid, "stored token subject is inconsistent")
	}
	return subject, true, nil
}
func record(address token.Address, subject tokenstore.Subject, family tokenstore.Family, entry tokenstore.Entry) (token.Record, error) {
	scope, err := address.Key()
	if err != nil {
		return token.Record{}, err
	}
	if subject.Scope != scope || family.Scope != scope || entry.Scope != scope || family.SubjectKey != subject.Key || entry.FamilyID != family.ID || entry.Generation > family.Generation || family.Generation > family.RotationLimit {
		return token.Record{}, fault.New(fault.Invalid, "stored token generation does not match its family")
	}
	identity, err := subject.Identity.Decode()
	if err != nil {
		return token.Record{}, err
	}
	key, err := address.SubjectKey(identity)
	if err != nil {
		return token.Record{}, err
	}
	if key != subject.Key {
		return token.Record{}, fault.New(fault.Invalid, "stored token subject does not match identity")
	}
	names, err := family.Scopes.Decode()
	if err != nil {
		return token.Record{}, err
	}
	access, err := token.ParseDigest(entry.AccessHash)
	if err != nil {
		return token.Record{}, err
	}
	var refresh value.Optional[token.Digest]
	if text, present := entry.RefreshHash.Get(); present {
		hash, err := token.ParseDigest(text)
		if err != nil {
			return token.Record{}, err
		}
		refresh = value.Set(hash)
	}
	var refreshExpiry value.Optional[temporal.DateTime]
	if expiry, present := entry.RefreshExpiresAt.Get(); present {
		refreshExpiry = value.Set(expiry)
	}
	var device auth.Device
	if text, present := family.ClientIP.Get(); present {
		if device.ClientIP, err = netip.ParseAddr(text); err != nil {
			return token.Record{}, fault.New(fault.Invalid, "stored token client address is invalid")
		}
	}
	device.UserAgent, _ = family.UserAgent.Get()
	result := token.Record{ID: model.IDFromBytes[token.Record](family.ID.Bytes()), Address: address, Subject: identity, Name: family.Name, Scopes: names, Mode: token.Mode(family.Mode), Assurance: auth.Assurance(family.Assurance), AccessHash: access, RefreshHash: refresh, Lifetime: token.Lifetime{Access: time.Duration(family.AccessNanos), RefreshIdle: time.Duration(family.RefreshIdleNanos), Absolute: family.ExpiresAt.UTC().Sub(family.CreatedAt.UTC())}, RotationLimit: family.RotationLimit, Generation: entry.Generation, CreatedAt: family.CreatedAt, IssuedAt: entry.IssuedAt, LastSeenAt: entry.LastSeenAt, AccessExpiresAt: entry.AccessExpiresAt, RefreshExpiresAt: refreshExpiry, ExpiresAt: family.ExpiresAt, Device: device}
	return result, result.Validate(address)
}
func currentRecord(ctx context.Context, tx *database.Tx, address token.Address, subject tokenstore.Subject, family tokenstore.Family) (token.Record, error) {
	found, err := entries(family.Scope, family.ID).Where(tokenstore.Entries.Generation.Eq(family.Generation)).First(ctx, tx)
	if err != nil {
		return token.Record{}, err
	}
	row, present := found.Get()
	if !present {
		return token.Record{}, fault.New(fault.Invalid, "stored token family has no current generation")
	}
	return record(address, subject, family, row)
}
func familyDraft(r token.Record, scope, subject string) ([]orm.Assignment[tokenstore.Family], error) {
	// Persist an empty JSON array rather than null for a deliberate empty grant.
	names := append([]auth.AccessScopeName{}, r.Scopes...)
	scopes, err := value.NewJSON(names)
	if err != nil {
		return nil, err
	}
	draft := []orm.Assignment[tokenstore.Family]{
		tokenstore.Families.ID.Set(model.IDFromBytes[tokenstore.Family](r.ID.Bytes())),
		tokenstore.Families.Scope.Set(scope),
		tokenstore.Families.SubjectKey.Set(subject),
		tokenstore.Families.Name.Set(r.Name),
		tokenstore.Families.Scopes.Set(scopes),
		tokenstore.Families.Mode.Set(uint8(r.Mode)),
		tokenstore.Families.Assurance.Set(uint8(r.Assurance)),
		tokenstore.Families.AccessNanos.Set(int64(r.Lifetime.Access)),
		tokenstore.Families.RefreshIdleNanos.Set(int64(r.Lifetime.RefreshIdle)),
		tokenstore.Families.RotationLimit.Set(r.RotationLimit),
		tokenstore.Families.Generation.Set(r.Generation),
		tokenstore.Families.CreatedAt.Set(r.CreatedAt),
		tokenstore.Families.ExpiresAt.Set(r.ExpiresAt),
	}
	// Zero device metadata is stored as NULL.
	if r.Device.ClientIP.IsValid() {
		draft = append(draft, tokenstore.Families.ClientIP.Set(r.Device.ClientIP.String()))
	} else {
		draft = append(draft, tokenstore.Families.ClientIP.SetNull())
	}
	if r.Device.UserAgent != "" {
		draft = append(draft, tokenstore.Families.UserAgent.Set(r.Device.UserAgent))
	} else {
		draft = append(draft, tokenstore.Families.UserAgent.SetNull())
	}
	return draft, nil
}
func entryDraft(r token.Record, scope string) ([]orm.Assignment[tokenstore.Entry], error) {
	id, err := model.NewID[tokenstore.Entry]()
	if err != nil {
		return nil, err
	}
	draft := []orm.Assignment[tokenstore.Entry]{
		tokenstore.Entries.ID.Set(id),
		tokenstore.Entries.Scope.Set(scope),
		tokenstore.Entries.FamilyID.Set(model.IDFromBytes[tokenstore.Family](r.ID.Bytes())),
		tokenstore.Entries.Generation.Set(r.Generation),
		tokenstore.Entries.AccessHash.Set(r.AccessHash.Hex()),
		tokenstore.Entries.IssuedAt.Set(r.IssuedAt),
		tokenstore.Entries.LastSeenAt.Set(r.LastSeenAt),
		tokenstore.Entries.AccessExpiresAt.Set(r.AccessExpiresAt),
	}
	if hash, present := r.RefreshHash.Get(); present {
		draft = append(draft, tokenstore.Entries.RefreshHash.Set(hash.Hex()))
	} else {
		draft = append(draft, tokenstore.Entries.RefreshHash.SetNull())
	}
	if expiry, present := r.RefreshExpiresAt.Get(); present {
		draft = append(draft, tokenstore.Entries.RefreshExpiresAt.Set(expiry))
	} else {
		draft = append(draft, tokenstore.Entries.RefreshExpiresAt.SetNull())
	}
	return draft, nil
}
