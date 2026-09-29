// Package postgres implements persistent cache using an explicitly migrated
// PostgreSQL table and borrowed Foundry connection. It never creates schema at
// application boot. Tags, batches and distributed fill leases are not supplied;
// Store.Invalidate physically removes a namespace's rows.
package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheatomic"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

const MaxPrune = cacheatomic.MaxPrune

// PruneResult reports one bounded prune pass and the recounted usage.
type PruneResult = cacheatomic.PruneResult

// Config bounds one shared table. Rows count until removed, but a write never
// fails because of expired rows: at capacity the backend first deletes up to
// MaxPrune expired rows, then rejects only if still full. Usage is an estimate
// maintained by this process and recounted by every prune pass, so processes
// sharing a schema can briefly overshoot between passes. Processes sharing a
// schema must use the same bounds and the same Foundry version.
type Config struct {
	Schema        string
	MaxEntries    int
	MaxBytes      int64
	MaxValueBytes int
	// Clock, when set, replaces PostgreSQL's clock_timestamp() for expiry. Leave
	// it nil in production so every application server shares the database's
	// time; tests may inject a deterministic clock.
	Clock clock.Clock
	// PruneInterval runs automatic deletion of expired rows while the backend is
	// started. Zero disables it; otherwise 1s to 24h.
	PruneInterval time.Duration
	// Logger optionally receives redacted automatic-prune failures.
	Logger *slog.Logger
}

func DefaultConfig() Config {
	limits := cacheatomic.DefaultLimits()
	return Config{Schema: "public", MaxEntries: limits.MaxEntries, MaxBytes: limits.MaxBytes, MaxValueBytes: limits.MaxValueBytes, PruneInterval: cacheatomic.DefaultPruneInterval}
}
func (c Config) Validate() error {
	if err := c.limits().Validate(); err != nil {
		return err
	}
	if !sqlname.Valid(c.Schema) || c.Clock != nil && credential.IsNil(c.Clock) || !cacheatomic.ValidPruneInterval(c.PruneInterval) {
		return fault.New(fault.Invalid, "invalid PostgreSQL cache configuration")
	}
	return nil
}
func (c Config) limits() cacheatomic.Limits {
	return cacheatomic.Limits{MaxEntries: c.MaxEntries, MaxBytes: c.MaxBytes, MaxValueBytes: c.MaxValueBytes}
}

// Backend borrows its database. Reads are single statements on the primary with
// no lock or explicit transaction. Put, Add and Forget are single atomic
// statements; Increment and Expire lock only their own row. Start optionally
// runs the owned pruner; Close stops it and waits for admitted operations.
// Operations work without Start, but no automatic pruning runs then.
type Backend struct {
	*cacheatomic.Backend
	db      *database.DB
	config  Config
	table   string
	usage   cacheatomic.Usage
	mu      sync.Mutex
	active  int
	started bool
	closed  bool
	done    chan struct{}
	stop    context.CancelFunc
}

var _ cache.FlushBackend = (*Backend)(nil)

// FoundryAdapter marks the backend as framework-owned adapter I/O.
func (*Backend) FoundryAdapter(frameworkadapter.Seal) {}

func New(db *database.DB, config Config) (*Backend, error) {
	if db == nil {
		return nil, fault.New(fault.Invalid, "PostgreSQL cache requires a database")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	b := &Backend{db: db, config: config, table: `"` + config.Schema + `".foundry_cache_entries`, done: make(chan struct{})}
	var err error
	b.Backend, err = cacheatomic.New(b, config.MaxValueBytes)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Start performs no I/O (the table may not be migrated yet at boot): when
// PruneInterval is positive it starts the owned background pruner, whose
// failures are logged and retried at the next interval. Repeated starts are
// no-ops. The first write seeds the usage estimate with one bounded count.
func (b *Backend) Start(ctx context.Context) error {
	if b == nil || b.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "PostgreSQL cache requires a backend and context")
	}
	leave, err := b.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return nil
	}
	b.started = true
	b.mu.Unlock()
	if b.config.PruneInterval <= 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fault.New(fault.Closed, "PostgreSQL cache is closed")
	}
	lifetime, stop := context.WithCancel(context.Background())
	b.stop = stop
	b.active++
	go func() {
		defer b.leave()
		cacheatomic.RunPruner(lifetime, b.config.PruneInterval, b.config.Logger, "postgres", func(ctx context.Context) (PruneResult, error) {
			return b.Sweep(ctx, MaxPrune)
		})
	}()
	return nil
}

// Close rejects new operations, stops the pruner and waits for admitted work.
// It never closes the borrowed database. A canceled caller only stops waiting.
func (b *Backend) Close(ctx context.Context) error {
	if b == nil || b.done == nil || ctx == nil {
		return fault.New(fault.Invalid, "PostgreSQL cache close needs context")
	}
	b.mu.Lock()
	b.closed = true
	if b.stop != nil {
		b.stop()
	}
	b.finish()
	b.mu.Unlock()
	select {
	case <-b.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *Backend) Done() <-chan struct{} { return b.done }
func (b *Backend) enter(ctx context.Context) (func(), error) {
	if b == nil || b.db == nil || b.done == nil || ctx == nil {
		return nil, fault.New(fault.Invalid, "PostgreSQL cache requires a backend and context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, fault.New(fault.Closed, "PostgreSQL cache is closed")
	}
	b.active++
	return b.leave, nil
}
func (b *Backend) leave() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active--
	b.finish()
}
func (b *Backend) finish() {
	if b.closed && b.active == 0 {
		select {
		case <-b.done:
		default:
			close(b.done)
		}
	}
}

func identity(key cache.EntryKey) string {
	hash := sha256.Sum256([]byte(key.String()))
	return hex.EncodeToString(hash[:])
}

// queryRow scans at most one row and reports whether it existed.
func queryRow(ctx context.Context, executor database.Executor, statement string, arguments []any, destinations ...any) (found bool, err error) {
	rows, err := executor.Query(ctx, statement, arguments...)
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	if !rows.Next() {
		return false, rows.Err()
	}
	if err := rows.Scan(destinations...); err != nil {
		return false, err
	}
	return true, rows.Err()
}

// now reads the authority time on executor, after any lock it holds.
func (b *Backend) now(ctx context.Context, executor database.Executor) (time.Time, error) {
	if b.config.Clock != nil {
		return b.config.Clock.Now(), nil
	}
	var now time.Time
	found, err := queryRow(ctx, executor, `SELECT pg_catalog.clock_timestamp()`, nil, &now)
	if err == nil && !found {
		err = fault.New(fault.Internal, "missing PostgreSQL clock result")
	}
	return now, err
}

// nowSQL is the liveness instant for a single statement: the database clock,
// or an injected clock bound as parameter n.
func (b *Backend) nowSQL(parameter string) (string, []any) {
	if b.config.Clock == nil {
		return `pg_catalog.clock_timestamp()`, nil
	}
	return parameter + `::timestamptz`, []any{b.config.Clock.Now().UTC()}
}

// expirySQL computes a write's expiry inside its statement from parameter n:
// microseconds after the database clock, or an absolute injected-clock instant.
func (b *Backend) expirySQL(parameter string, ttl cache.TTL) (string, any) {
	if b.config.Clock == nil {
		expression := `CASE WHEN ` + parameter + `::bigint IS NULL THEN NULL ELSE pg_catalog.clock_timestamp() + ` + parameter + `::bigint * interval '1 microsecond' END`
		if ttl.IsForever() {
			return expression, nil
		}
		micros := ttl.Duration() / time.Microsecond
		if ttl.Duration()%time.Microsecond != 0 {
			micros++
		}
		return expression, int64(micros)
	}
	if ttl.IsForever() {
		return parameter + `::timestamptz`, nil
	}
	return parameter + `::timestamptz`, b.config.Clock.Now().Add(ttl.Duration()).UTC()
}

// Read is one statement on the primary: no lock and no explicit transaction.
// Over-bound or mismatched rows read as absent.
func (b *Backend) Read(ctx context.Context, key cache.EntryKey, data bool) (*cacheatomic.Record, time.Time, error) {
	leave, err := b.enter(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer leave()
	var observed time.Time
	var address string
	var payload []byte
	var size int
	var expiry sql.NullTime
	found, err := queryRow(ctx, b.db.Primary(), `SELECT pg_catalog.clock_timestamp(), address, CASE WHEN $3 AND octet_length(payload) <= $2 THEN payload END, octet_length(payload), expires_at FROM `+b.table+` WHERE key = $1`, []any{identity(key), b.config.MaxValueBytes, data}, &observed, &address, &payload, &size, &expiry)
	if err != nil || !found {
		return nil, time.Time{}, err
	}
	if b.config.Clock != nil {
		observed = b.config.Clock.Now()
	}
	if address != key.String() || size > b.config.MaxValueBytes || data && size != len(payload) {
		return nil, observed, nil
	}
	current := &cacheatomic.Record{Expires: expiry.Time}
	if data {
		current.Data = payload
		if current.Data == nil {
			current.Data = []byte{}
		}
	}
	return current, observed, nil
}

// storedSize returns the physical size of key's row, or -1 when absent.
func (b *Backend) storedSize(ctx context.Context, id string) (int64, error) {
	var size int64
	found, err := queryRow(ctx, b.db.Primary(), `SELECT octet_length(address)+octet_length(payload) FROM `+b.table+` WHERE key = $1`, []any{id}, &size)
	if err != nil || !found {
		return -1, err
	}
	return size, nil
}

// admit checks the usage estimate for a single-statement write. A replacement
// only needs byte room. At capacity it reclaims expired rows (at most once per
// second per backend) and rejects only if the cache is still full.
func (b *Backend) admit(ctx context.Context, id string, size int64) error {
	limits := b.config.limits()
	if err := b.seed(ctx); err != nil {
		return err
	}
	for reclaimed := false; ; reclaimed = true {
		if b.usage.Fits(limits, -1, size) {
			return nil
		}
		existing, err := b.storedSize(ctx, id)
		if err != nil {
			return err
		}
		if existing >= 0 && b.usage.Fits(limits, existing, size) {
			return nil
		}
		if reclaimed || !b.usage.ReconcileDue() {
			return cacheatomic.CapacityError("PostgreSQL cache")
		}
		result, err := b.sweep(ctx, MaxPrune)
		if err != nil {
			return err
		}
		b.usage.Reclaimed(result)
	}
}

// seed counts the table once before the first write so the estimate starts
// from stored rows; Start performs no I/O and the table may be migrated later.
func (b *Backend) seed(ctx context.Context) error {
	if b.usage.Counted() {
		return nil
	}
	_, err := b.recount(ctx)
	return err
}

// write validates one single-statement write before any I/O.
func (b *Backend) write(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL) (func(), string, error) {
	if ctx == nil {
		return nil, "", fault.New(fault.Invalid, "PostgreSQL cache requires a backend and context")
	}
	if err := key.Validate(); err != nil {
		return nil, "", err
	}
	if err := ttl.Validate(); err != nil {
		return nil, "", err
	}
	if len(data) > b.config.MaxValueBytes {
		return nil, "", fault.New(fault.Invalid, "cache value exceeds limit")
	}
	leave, err := b.enter(ctx)
	if err != nil {
		return nil, "", err
	}
	id := identity(key)
	if err := b.admit(ctx, id, int64(len(key.String())+len(data))); err != nil {
		leave()
		return nil, "", err
	}
	return leave, id, nil
}

// Put replaces one row in a single atomic statement with the expiry computed
// by the database (or injected) clock at write time.
func (b *Backend) Put(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL) error {
	leave, id, err := b.write(ctx, key, data, ttl)
	if err != nil {
		return err
	}
	defer leave()
	expiry, value := b.expirySQL("$4", ttl)
	var previous sql.NullInt64
	_, err = queryRow(ctx, b.db.Primary(), `WITH previous AS (SELECT octet_length(address)+octet_length(payload) AS size FROM `+b.table+` WHERE key = $1)
INSERT INTO `+b.table+` AS e (key,address,payload,expires_at) VALUES ($1,$2,$3,`+expiry+`)
ON CONFLICT (key) DO UPDATE SET address=EXCLUDED.address,payload=EXCLUDED.payload,expires_at=EXCLUDED.expires_at
RETURNING (SELECT size FROM previous)`, []any{id, key.String(), nonNil(data), value}, &previous)
	if err != nil {
		return err
	}
	b.applyWrite(previous, int64(len(key.String())+len(data)))
	return nil
}

// Add inserts, or replaces only an expired, over-bound or mismatched row, in
// one statement; the conflict condition is evaluated on the locked row.
func (b *Backend) Add(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL) (bool, error) {
	leave, id, err := b.write(ctx, key, data, ttl)
	if err != nil {
		return false, err
	}
	defer leave()
	expiry, value := b.expirySQL("$4", ttl)
	now, clockArgs := b.nowSQL("$6")
	var previous sql.NullInt64
	added, err := queryRow(ctx, b.db.Primary(), `WITH previous AS (SELECT octet_length(address)+octet_length(payload) AS size FROM `+b.table+` WHERE key = $1)
INSERT INTO `+b.table+` AS e (key,address,payload,expires_at) VALUES ($1,$2,$3,`+expiry+`)
ON CONFLICT (key) DO UPDATE SET address=EXCLUDED.address,payload=EXCLUDED.payload,expires_at=EXCLUDED.expires_at
WHERE e.address <> EXCLUDED.address OR octet_length(e.payload) > $5 OR e.expires_at <= `+now+`
RETURNING (SELECT size FROM previous)`, append([]any{id, key.String(), nonNil(data), value, b.config.MaxValueBytes}, clockArgs...), &previous)
	if err != nil || !added {
		return false, err
	}
	b.applyWrite(previous, int64(len(key.String())+len(data)))
	return true, nil
}

// Forget deletes the row in one statement and reports whether it was a live,
// usable entry. Unusable rows are removed but reported as absent.
func (b *Backend) Forget(ctx context.Context, key cache.EntryKey) (bool, error) {
	if ctx == nil {
		return false, fault.New(fault.Invalid, "PostgreSQL cache requires a backend and context")
	}
	if err := key.Validate(); err != nil {
		return false, err
	}
	leave, err := b.enter(ctx)
	if err != nil {
		return false, err
	}
	defer leave()
	now, clockArgs := b.nowSQL("$4")
	var live bool
	var size int64
	found, err := queryRow(ctx, b.db.Primary(), `DELETE FROM `+b.table+` WHERE key = $1 RETURNING address = $2 AND octet_length(payload) <= $3 AND (expires_at IS NULL OR expires_at > `+now+`), octet_length(address)+octet_length(payload)`, append([]any{identity(key), key.String(), b.config.MaxValueBytes}, clockArgs...), &live, &size)
	if err != nil || !found {
		return false, err
	}
	b.usage.Add(-1, -size)
	return live, nil
}
func (b *Backend) applyWrite(previous sql.NullInt64, size int64) {
	if previous.Valid {
		b.usage.Add(0, size-previous.Int64)
	} else {
		b.usage.Add(1, size)
	}
}
func nonNil(data []byte) []byte {
	if data == nil {
		return []byte{}
	}
	return data
}

// errFull rolls back a locked change that does not fit so expiry can be reclaimed.
var errFull = cacheatomic.CapacityError("PostgreSQL cache")

// Access runs one change while holding the key's row lock (SELECT ... FOR
// UPDATE). An absent key is inserted with ON CONFLICT DO NOTHING; losing that
// race re-reads and locks the concurrent row. The authority time is read after
// the lock. No schema-wide lock is taken.
func (b *Backend) Access(ctx context.Context, key cache.EntryKey, mode cacheatomic.Mode, change cacheatomic.Change) error {
	leave, err := b.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	if err := b.seed(ctx); err != nil {
		return err
	}
	id := identity(key)
	address := key.String()
	for reclaimed := false; ; reclaimed = true {
		var entries, bytes int64
		err := b.db.Primary().Transaction(ctx, func(tx *database.Tx) error {
			entries, bytes = 0, 0
			for range 8 {
				var stored string
				var payload []byte
				var size int
				var expiry sql.NullTime
				exists, err := queryRow(ctx, tx, `SELECT address, CASE WHEN $2 AND octet_length(payload) <= $3 THEN payload END, octet_length(payload), expires_at FROM `+b.table+` WHERE key = $1 FOR UPDATE`, []any{id, mode == cacheatomic.Payload, b.config.MaxValueBytes}, &stored, &payload, &size, &expiry)
				if err != nil {
					return err
				}
				var old *cacheatomic.Record
				if exists && stored == address && size <= b.config.MaxValueBytes {
					old = &cacheatomic.Record{Data: payload, Expires: expiry.Time}
					if mode == cacheatomic.Payload && old.Data == nil {
						old.Data = []byte{}
					}
				}
				now, err := b.now(ctx, tx)
				if err != nil {
					return err
				}
				next, update, err := change(now, old)
				if err != nil || !update {
					return err
				}
				previous := int64(-1)
				if exists {
					previous = int64(len(stored) + size)
				}
				if next == nil {
					if !exists {
						return nil
					}
					if _, err := tx.Exec(ctx, `DELETE FROM `+b.table+` WHERE key = $1`, id); err != nil {
						return err
					}
					entries, bytes = -1, -previous
					return nil
				}
				cost := int64(len(address) + len(next.Data))
				if !b.usage.Fits(b.config.limits(), previous, cost) {
					return errFull
				}
				var expires any
				if !next.Expires.IsZero() {
					expires = next.Expires.UTC()
				}
				if exists {
					if _, err := tx.Exec(ctx, `UPDATE `+b.table+` SET address=$2,payload=$3,expires_at=$4 WHERE key = $1`, id, address, nonNil(next.Data), expires); err != nil {
						return err
					}
					entries, bytes = 0, cost-previous
					return nil
				}
				result, err := tx.Exec(ctx, `INSERT INTO `+b.table+` (key,address,payload,expires_at) VALUES($1,$2,$3,$4) ON CONFLICT (key) DO NOTHING`, id, address, nonNil(next.Data), expires)
				if err != nil {
					return err
				}
				if result.RowsAffected == 1 {
					entries, bytes = 1, cost
					return nil
				}
				// A concurrent insert won; lock and apply the change to it.
			}
			return fault.New(fault.Conflict, "PostgreSQL cache entry changed repeatedly")
		}, database.TxOptions{Isolation: database.ReadCommitted})
		if err == nil {
			b.usage.Add(entries, bytes)
			return nil
		}
		if !errors.Is(err, errFull) {
			return err
		}
		if reclaimed || !b.usage.ReconcileDue() {
			return cacheatomic.CapacityError("PostgreSQL cache")
		}
		result, err := b.sweep(ctx, MaxPrune)
		if err != nil {
			return err
		}
		b.usage.Reclaimed(result)
	}
}

// Inspect locks only this row, reads the authority time after the lock and
// changes expiry without selecting the payload. Unusable rows are absent.
func (b *Backend) Inspect(ctx context.Context, key cache.EntryKey, inspect cacheatomic.Inspection) (bool, error) {
	leave, err := b.enter(ctx)
	if err != nil {
		return false, err
	}
	defer leave()
	found := false
	id := identity(key)
	err = b.db.Primary().Transaction(ctx, func(tx *database.Tx) error {
		var address string
		var size int
		var expiry sql.NullTime
		exists, err := queryRow(ctx, tx, `SELECT address,octet_length(payload),expires_at FROM `+b.table+` WHERE key = $1 FOR UPDATE`, []any{id}, &address, &size, &expiry)
		if err != nil || !exists || address != key.String() || size > b.config.MaxValueBytes {
			return err
		}
		now, err := b.now(ctx, tx)
		if err != nil {
			return err
		}
		expires, live, err := inspect(now, expiry.Time)
		found = live
		if err != nil || !found || expires == nil {
			return err
		}
		var next any
		if !expires.IsZero() {
			next = expires.UTC()
		}
		_, err = tx.Exec(ctx, `UPDATE `+b.table+` SET expires_at=$2 WHERE key = $1`, id, next)
		return err
	}, database.TxOptions{Isolation: database.ReadCommitted})
	return found && err == nil, err
}

// Prune deletes at most limit expired rows in this configured cache table.
func (b *Backend) Prune(ctx context.Context, limit int) (int, error) {
	result, err := b.Sweep(ctx, limit)
	return result.Removed, err
}

// Sweep deletes at most limit expired rows (skipping rows locked by concurrent
// operations) and recounts the usage estimate with a bounded scan.
func (b *Backend) Sweep(ctx context.Context, limit int) (PruneResult, error) {
	if limit < 1 || limit > MaxPrune {
		return PruneResult{}, fault.New(fault.Invalid, "invalid cache prune limit")
	}
	leave, err := b.enter(ctx)
	if err != nil {
		return PruneResult{}, err
	}
	defer leave()
	return b.sweep(ctx, limit)
}
func (b *Backend) sweep(ctx context.Context, limit int) (PruneResult, error) {
	now, clockArgs := b.nowSQL("$2")
	result, err := b.db.Primary().Exec(ctx, `DELETE FROM `+b.table+` WHERE key IN (SELECT key FROM `+b.table+` WHERE expires_at <= `+now+` ORDER BY expires_at,key LIMIT $1 FOR UPDATE SKIP LOCKED)`, append([]any{limit}, clockArgs...)...)
	if err != nil {
		return PruneResult{}, err
	}
	counted, err := b.recount(ctx)
	counted.Removed = int(result.RowsAffected)
	return counted, err
}

// recount replaces the usage estimate from a bounded scan of the table.
func (b *Backend) recount(ctx context.Context) (PruneResult, error) {
	bound := 2*b.config.MaxEntries + MaxPrune + 1
	var result PruneResult
	found, err := queryRow(ctx, b.db.Primary(), `SELECT count(*), COALESCE(sum(size),0)::bigint FROM (SELECT octet_length(address)+octet_length(payload) AS size FROM `+b.table+` LIMIT $1) AS counted`, []any{bound}, &result.Entries, &result.Bytes)
	if err == nil && !found {
		err = fault.New(fault.Internal, "missing PostgreSQL cache usage result")
	}
	if err != nil {
		return PruneResult{}, err
	}
	// An incomplete count is a lower bound above capacity, so writes reclaim.
	result.Complete = result.Entries < int64(bound)
	b.usage.Reconcile(result.Entries, result.Bytes)
	return result, nil
}

// FlushNamespace deletes every row whose address belongs to namespace in one
// statement. It is not a fence against writes that start afterwards.
func (b *Backend) FlushNamespace(ctx context.Context, namespace cache.Namespace) (uint64, error) {
	prefix, err := cacheatomic.NamespacePrefix(namespace)
	if err != nil {
		return 0, err
	}
	leave, err := b.enter(ctx)
	if err != nil {
		return 0, err
	}
	defer leave()
	var count, bytes int64
	found, err := queryRow(ctx, b.db.Primary(), `WITH removed AS (DELETE FROM `+b.table+` WHERE starts_with(address, $1) RETURNING octet_length(address)+octet_length(payload) AS size) SELECT count(*), COALESCE(sum(size),0)::bigint FROM removed`, []any{prefix}, &count, &bytes)
	if err == nil && !found {
		err = fault.New(fault.Internal, "missing PostgreSQL cache flush result")
	}
	if err != nil {
		return 0, err
	}
	b.usage.Add(-count, -bytes)
	return uint64(count), nil
}
