package database

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Isolation selects portable PostgreSQL transaction isolation levels.
type Isolation uint8

const (
	DefaultIsolation Isolation = iota
	ReadCommitted
	RepeatableRead
	Serializable
)

// TxOptions controls one transaction. The zero value uses server defaults.
type TxOptions struct {
	Isolation Isolation
	ReadOnly  bool
}

func (o TxOptions) sqlOptions() (*sql.TxOptions, error) {
	levels := [...]sql.IsolationLevel{sql.LevelDefault, sql.LevelReadCommitted, sql.LevelRepeatableRead, sql.LevelSerializable}
	if int(o.Isolation) >= len(levels) {
		return nil, fault.New(fault.Invalid, "invalid transaction isolation")
	}
	return &sql.TxOptions{Isolation: levels[o.Isolation], ReadOnly: o.ReadOnly}, nil
}

func transactionOptions(fn func(*Tx) error, options []TxOptions) (*sql.TxOptions, error) {
	if fn == nil || len(options) > 1 {
		return nil, fault.New(fault.Invalid, "transaction needs a callback and at most one option set")
	}
	var selected TxOptions
	if len(options) == 1 {
		selected = options[0]
	}
	return selected.sqlOptions()
}

// TxState describes scope lifetime. A released savepoint has not committed;
// only the outer transaction can produce a Committed database outcome.
type TxState string

const (
	TxActive     TxState = "active"
	TxFinishing  TxState = "finishing"
	TxCommitted  TxState = "committed"
	TxRolledBack TxState = "rolled_back"
	TxUnknown    TxState = "unknown"
	TxReleased   TxState = "savepoint_released"
)

// Tx is a callback-scoped transaction or savepoint. Use it sequentially and
// close query rows before returning from its callback. Overlapping operations
// fail with Busy. An escaped Tx is closed after its callback; it cannot commit
// independently or be reused in a later request.
type Tx struct {
	owner      *DB
	raw        *sql.Tx
	scope      *operationScope
	classify   classifier
	control    *transactionControl
	mu         sync.Mutex
	state      TxState
	after      []func(context.Context) error
	observers  lifecycle.Observers
	timeSource clock.Clock
}

type transactionControl struct {
	sequence atomic.Uint64
	mu       sync.Mutex
	abort    error
	cancel   context.CancelFunc
}

func (c *transactionControl) poison(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.abort = errors.Join(c.abort, err)
}
func (c *transactionControl) failure() error { c.mu.Lock(); defer c.mu.Unlock(); return c.abort }

// Transaction runs one callback exactly once. Options may contain at most one
// value. Callback failure, panic or Goexit rolls back; no user callback is retried.
// The begin context owns transaction lifetime. The callback runs in an owned
// goroutine so Goexit cannot bypass rollback. Cancellation is cooperative.
//
// Successful commit releases the connection before after-commit callbacks run.
// An Error with Outcome()==Committed means persistence succeeded despite a
// callback/release failure. Unknown commit outcomes require reconciliation;
// retrying this whole callback could duplicate external side effects.
func (db *DB) Transaction(ctx context.Context, fn func(*Tx) error, options ...TxOptions) (err error) {
	sqlOptions, err := transactionOptions(fn, options)
	if err != nil {
		return err
	}
	// After-commit work no longer owns a connection, but remains part of the
	// operation that pool shutdown must drain.
	if err := db.own(); err != nil {
		return err
	}
	defer db.release()
	conn, release, err := db.acquire(ctx)
	if err != nil {
		return err
	}
	return runTransaction(ctx, conn, db, db.classify, db.Observers(), db.Clock(), release, fn, sqlOptions)
}

func runTransaction(ctx context.Context, conn *sql.Conn, owner *DB, classify classifier, observers lifecycle.Observers, source clock.Clock, release func() error, fn func(*Tx) error, options *sql.TxOptions) (err error) {
	released := false
	defer func() {
		if !released {
			err = errors.Join(err, release())
		}
	}()
	lifetime, cancel := context.WithCancel(ctx)
	defer cancel()
	raw, err := conn.BeginTx(lifetime, options)
	if err != nil {
		return classify.wrap("begin", err)
	}
	tx := &Tx{owner: owner, raw: raw, scope: &operationScope{ctx: lifetime, cancel: cancel}, classify: classify, state: TxActive, control: &transactionControl{cancel: cancel}, observers: observers, timeSource: source}
	// Defensive cleanup also covers a framework panic after BEGIN. SQL rollback
	// is harmless after an already terminal commit/rollback.
	defer raw.Rollback()
	err = callback.Isolated("transaction callback", func() error { return fn(tx) })
	err = errors.Join(err, tx.finishScope(), tx.control.failure())
	if err == nil {
		err = lifetime.Err()
	}
	if err != nil {
		rollbackErr := raw.Rollback()
		confirmed := rollbackErr == nil
		if rollbackErr == sql.ErrTxDone {
			rollbackErr = nil
		} // begin-context auto rollback owns cleanup; its result is unavailable
		classified := classify.wrap("transaction", err).(*Error)
		if confirmed {
			tx.setState(TxRolledBack)
			classified.outcome = RolledBack
		} else {
			tx.setState(TxUnknown)
			classified.outcome = NoCommit
			rollbackErr = errors.Join(rollbackErr, discardConnection(conn))
		}
		return errors.Join(classified, classify.wrap("rollback", rollbackErr))
	}
	if commitErr := raw.Commit(); commitErr != nil {
		classified := classify.wrap("commit", commitErr).(*Error)
		if classified.detail.CommitRejected {
			classified.outcome = RolledBack
			tx.setState(TxRolledBack)
		} else {
			classified.outcome = Unknown
			classified.detail.Code = CommitUnknown
			tx.setState(TxUnknown)
			return errors.Join(classified, classify.wrap("discard uncertain commit", discardConnection(conn)))
		}
		return classified
	}
	tx.setState(TxCommitted)
	var failures []error
	released = true
	if err := release(); err != nil {
		failures = append(failures, err)
	}
	for _, fn := range tx.callbacks() {
		if err := callback.Isolated("after commit", func() error { return fn(ctx) }); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) != 0 {
		return &Error{operation: "after commit", detail: Detail{Code: AfterCommitFailed}, outcome: Committed, cause: errors.Join(failures...)}
	}
	return nil
}

func (tx *Tx) setState(state TxState) { tx.mu.Lock(); defer tx.mu.Unlock(); tx.state = state }
func (tx *Tx) State() TxState         { tx.mu.Lock(); defer tx.mu.Unlock(); return tx.state }

func (tx *Tx) enter() (func() error, error) {
	return tx.scope.enter()
}

func (tx *Tx) finishScope() error {
	tx.setState(TxFinishing)
	return tx.scope.finish()
}

func (tx *Tx) callbacks() []func(context.Context) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	return append([]func(context.Context) error(nil), tx.after...)
}

// AfterCommit registers process-local work. Registration order is retained;
// failures are collected without skipping later callbacks. Savepoint callbacks
// reach the outer transaction only after successful release. These callbacks
// have no crash durability; enqueue durable publication through the outbox layer.
func (tx *Tx) AfterCommit(fn func(context.Context) error) error {
	if fn == nil {
		return fault.New(fault.Invalid, "after-commit callback is nil")
	}
	release, err := tx.enter()
	if err != nil {
		return err
	}
	defer release()
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.after = append(tx.after, fn)
	return nil
}

func (tx *Tx) Exec(ctx context.Context, statement string, arguments ...any) (result Result, err error) {
	return tx.scope.exec(ctx, tx.raw, tx.classify, statement, arguments)
}

func (tx *Tx) Query(ctx context.Context, statement string, arguments ...any) (*Rows, error) {
	return tx.scope.query(ctx, tx.raw, tx.classify, tx.observers, statement, arguments)
}

// An operation may have a tighter context, but it cannot outlive the transaction
// or savepoint's context even if its caller supplied context.Background().
func (tx *Tx) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return tx.scope.context(ctx)
}

// BelongsTo checks pool ownership without I/O. Infrastructure adapters use it
// before joining an application's transaction. It is not authorization and does
// not establish that the transaction is still active; normal operations enforce
// lifetime. Savepoints and connection-scoped transactions retain the same owner.
func (tx *Tx) BelongsTo(db *DB) bool { return tx != nil && db != nil && tx.owner == db }

// SharesTransaction reports whether two scopes belong to the same outer SQL
// transaction, including nested savepoints. It performs no I/O and does not
// prove either scope is active. Independent transactions from one pool differ.
func (tx *Tx) SharesTransaction(other *Tx) bool {
	return tx != nil && other != nil && tx.control != nil && tx.control == other.control
}
