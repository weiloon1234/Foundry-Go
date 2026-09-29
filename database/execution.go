package database

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Executor is the raw SQL capability shared by pools and transactions. Values
// belong in arguments; never concatenate untrusted values into statement text.
// Model-specific type checking belongs to generated queries above this boundary.
type Executor interface {
	Exec(context.Context, string, ...any) (Result, error)
	Query(context.Context, string, ...any) (*Rows, error)
}

// Result reports affected rows. PostgreSQL generated IDs use RETURNING with Query
// rather than the non-portable database/sql LastInsertId contract.
type Result struct{ RowsAffected int64 }

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func execute(ctx context.Context, executor sqlExecutor, classify classifier, instrument statementProbe, statement string, arguments []any) (result Result, err error) {
	started := instrument.start()
	rows := int64(-1)
	defer func() { instrument.finish(ctx, "exec", statement, started, rows, err) }()
	raw, err := executor.ExecContext(ctx, statement, arguments...)
	if err != nil {
		return Result{}, classify.wrap("execute", err)
	}
	count, err := raw.RowsAffected()
	if err != nil {
		return Result{}, classify.wrap("affected rows", err)
	}
	rows = count
	return Result{RowsAffected: count}, nil
}

func query(ctx context.Context, executor sqlExecutor, classify classifier, instrument statementProbe, observers lifecycle.Observers, owner observerOwner, release func() error, statement string, arguments []any) (*Rows, error) {
	return queryStatement(ctx, executor, classify, instrument, observers, owner, release, statement, arguments, false)
}

// queryStatement ties the stream to its query context: database/sql closes its
// driver rows on cancellation, but only Close returns Foundry's checked-out
// connection and resource owner. A forgotten stream is therefore closed when
// ctx ends instead of retaining the connection and blocking pool shutdown.
func queryStatement(ctx context.Context, executor sqlExecutor, classify classifier, instrument statementProbe, observers lifecycle.Observers, owner observerOwner, release func() error, statement string, arguments []any, autocommit bool) (*Rows, error) {
	started := instrument.start()
	rows, err := executor.QueryContext(ctx, statement, arguments...)
	if err != nil {
		failure := classify.wrap("query", err)
		if autocommit {
			failure = statementOutcome(failure)
		}
		instrument.finish(ctx, "query", statement, started, -1, failure)
		return nil, errors.Join(failure, release())
	}
	result := &Rows{raw: rows, classify: classify, observers: observers, observerOwner: owner, release: release, autocommit: autocommit, probe: instrument, started: started, statement: statement, ctx: ctx}
	// A query can finish as its context ends. Publish the stop hook while
	// holding the same mutex as Close, before the cancellation callback reads it.
	result.mu.Lock()
	result.stop = context.AfterFunc(ctx, func() { _ = result.Close() })
	result.mu.Unlock()
	return result, nil
}

// Exec executes a parameterized statement and releases its connection on return.
func (db *DB) Exec(ctx context.Context, statement string, arguments ...any) (result Result, err error) {
	instrument := db.instrumented(PrimaryPool)
	acquired := instrument.start()
	db.markWrite(ctx)
	conn, release, err := db.acquire(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, release()) }()
	instrument.wait = sinceStart(acquired)
	// Mark again on completion so a long write does not consume its own
	// read-your-writes window.
	defer db.markWrite(ctx)
	return execute(ctx, conn, db.classify, instrument, statement, arguments)
}

// Query returns a stream owning its connection until Close, complete iteration
// or the end of ctx. Always defer Close, and check Err after iteration. Do not
// share Rows between goroutines. Scan errors close the stream to release the
// connection promptly; cancellation closes a forgotten stream as a safety net.
//
// A primary query may write (for example INSERT ... RETURNING), so with
// WithStickyReads it marks the request scope when it starts and when its
// stream closes.
func (db *DB) Query(ctx context.Context, statement string, arguments ...any) (*Rows, error) {
	instrument := db.instrumented(PrimaryPool)
	acquired := instrument.start()
	db.markWrite(ctx)
	conn, release, err := db.acquire(ctx)
	if err != nil {
		return nil, err
	}
	instrument.wait = sinceStart(acquired)
	return db.markOnClose(ctx)(query(ctx, conn, db.classify, instrument, db.Observers(), observerOwner{db: db, ctx: ctx}, release, statement, arguments))
}

// FoundryAutocommitQuery runs one framework-compiled write statement outside an
// explicit transaction. Every returned failure carries the statement outcome:
// NoCommit before the statement was performed, RolledBack when the adapter
// confirms server rejection, and otherwise Unknown with CommitUnknown. Only a
// complete iteration whose Err and Close both succeed confirms the commit.
func (db *DB) FoundryAutocommitQuery(_ sqlowner.Seal, ctx context.Context, statement string, arguments ...any) (*Rows, error) {
	db.markWrite(ctx)
	instrument := db.instrumented(PrimaryPool)
	acquired := instrument.start()
	conn, release, err := db.acquire(ctx)
	if err != nil {
		if classified, ok := err.(*Error); ok && classified != nil {
			classified.outcome = NoCommit
		}
		return nil, err
	}
	instrument.wait = sinceStart(acquired)
	return db.markOnClose(ctx)(queryStatement(ctx, conn, db.classify, instrument, db.Observers(), observerOwner{db: db, ctx: ctx}, release, statement, arguments, true))
}

// Row is the read capability passed to typed hydration callbacks. Standard Go
// sql.Scanner implementations work at this explicit raw decoding boundary.
type Row interface{ Scan(...any) error }

// Rows owns a sequential result stream. It does not materialize the full result
// set. Scan destinations may be partially populated on errors and must then be
// discarded. Generated hydration builds a fresh value before publishing it.
type Rows struct {
	mu             sync.Mutex
	raw            *sql.Rows
	classify       classifier
	observers      lifecycle.Observers
	observerOwner  observerOwner
	observerScoped bool
	release        func() error
	stop           func() bool
	autocommit     bool
	closed         bool
	err            error
	// afterClose runs once when the stream closes, for read-your-writes marking.
	afterClose func()
	// Instrumentation: the event is reported once, when the stream closes.
	probe     statementProbe
	started   time.Time
	statement string
	ctx       context.Context
	returned  int64
}

// wrap classifies stream failures. Autocommit write streams also label their
// statement outcome because a failure after sending may follow the commit.
func (r *Rows) wrap(operation string, err error) error {
	classified := r.classify.wrap(operation, err)
	if r.autocommit {
		return statementOutcome(classified)
	}
	return classified
}

// Observers retains the actual database owner's immutable registrations, even
// after the stream closes. Executor wrappers preserve this metadata by returning
// the original Rows. Inspecting it never constructs or dispatches model hooks.
func (r *Rows) Observers() lifecycle.Observers { return r.observers }

func (r *Rows) Next() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	if r.raw.Next() {
		r.returned++
		return true
	}
	_ = r.close()
	return false
}

func (r *Rows) Scan(destinations ...any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return failure("scan", Closed)
	}
	if err := r.raw.Scan(destinations...); err != nil {
		r.err = errors.Join(r.err, r.wrap("scan", err))
		_ = r.close()
		return r.err
	}
	return nil
}

// Columns returns a caller-owned snapshot of result column names.
func (r *Rows) Columns() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, failure("columns", Closed)
	}
	columns, err := r.raw.Columns()
	return append([]string(nil), columns...), r.wrap("columns", err)
}

func (r *Rows) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.err
	}
	return errors.Join(r.err, r.wrap("iterate", r.raw.Err()))
}

// Close is idempotent and preserves scan, iteration, and release errors.
func (r *Rows) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.close()
}

func (r *Rows) close() error {
	if r.closed {
		return r.err
	}
	r.closed = true
	if r.stop != nil {
		r.stop()
	}
	closeErr := r.wrap("close rows", r.raw.Close())
	iterateErr := r.wrap("iterate", r.raw.Err())
	released := r.release()
	if r.autocommit {
		released = statementOutcome(released)
	}
	r.err = errors.Join(r.err, closeErr, iterateErr, released)
	if r.afterClose != nil {
		r.afterClose()
	}
	r.probe.finish(r.ctx, "query", r.statement, r.started, r.returned, r.err)
	return r.err
}

// sinceStart measures from an instrumentation sample; zero when disabled.
func sinceStart(started time.Time) time.Duration {
	if started.IsZero() {
		return 0
	}
	return time.Since(started)
}

// ScanOne requires exactly one row and always closes the stream. A zero-row
// result is NotFound, not a generic query failure. Multiple rows are TooManyRows.
// Arguments and destinations are explicit runtime-checked SQL boundaries.
func ScanOne(ctx context.Context, executor Executor, statement string, arguments []any, destinations ...any) (err error) {
	rows, err := executor.Query(ctx, statement, arguments...)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return failure("scan one", NotFound)
	}
	if err := rows.Scan(destinations...); err != nil {
		return err
	}
	if rows.Next() {
		return failure("scan one", TooManyRows)
	}
	return rows.Err()
}

// ForEach hydrates and yields one value at a time, closing rows on early return,
// error or panic. It retains no collection; callbacks decide what to retain.
func ForEach[T any](ctx context.Context, executor Executor, statement string, arguments []any, scan func(Row) (T, error), yield func(T) error) (err error) {
	if scan == nil || yield == nil {
		return fault.New(fault.Invalid, "row iteration requires scan and yield callbacks")
	}
	rows, err := executor.Query(ctx, statement, arguments...)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		value, err := scan(rows)
		if err != nil {
			return err
		}
		if err := yield(value); err != nil {
			return err
		}
	}
	return rows.Err()
}
