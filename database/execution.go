package database

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
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

func execute(ctx context.Context, executor sqlExecutor, classify classifier, statement string, arguments []any) (Result, error) {
	result, err := executor.ExecContext(ctx, statement, arguments...)
	if err != nil {
		return Result{}, classify.wrap("execute", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Result{}, classify.wrap("affected rows", err)
	}
	return Result{RowsAffected: count}, nil
}

func query(ctx context.Context, executor sqlExecutor, classify classifier, observers lifecycle.Observers, owner observerOwner, release func() error, statement string, arguments []any) (*Rows, error) {
	rows, err := executor.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, errors.Join(classify.wrap("query", err), release())
	}
	return &Rows{raw: rows, classify: classify, observers: observers, observerOwner: owner, release: release}, nil
}

// Exec executes a parameterized statement and releases its connection on return.
func (db *DB) Exec(ctx context.Context, statement string, arguments ...any) (result Result, err error) {
	conn, release, err := db.acquire(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, release()) }()
	return execute(ctx, conn, db.classify, statement, arguments)
}

// Query returns a stream owning its connection until Close or complete iteration.
// Always defer Close, and check Err after iteration. Do not share Rows between
// goroutines. Scan errors close the stream to release the connection promptly.
func (db *DB) Query(ctx context.Context, statement string, arguments ...any) (*Rows, error) {
	conn, release, err := db.acquire(ctx)
	if err != nil {
		return nil, err
	}
	return query(ctx, conn, db.classify, db.Observers(), observerOwner{db: db, ctx: ctx}, release, statement, arguments)
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
	closed         bool
	err            error
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
		r.err = errors.Join(r.err, r.classify.wrap("scan", err))
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
	return append([]string(nil), columns...), r.classify.wrap("columns", err)
}

func (r *Rows) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.err
	}
	return errors.Join(r.err, r.classify.wrap("iterate", r.raw.Err()))
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
	r.err = errors.Join(r.err, r.classify.wrap("close rows", r.raw.Close()), r.classify.wrap("iterate", r.raw.Err()), r.release())
	return r.err
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
