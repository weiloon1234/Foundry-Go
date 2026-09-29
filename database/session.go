package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Session is an advanced, callback-scoped connection capability for operations
// such as migration locks spanning multiple transactions. It preserves one
// physical connection. Ordinary requests should use DB or Tx instead.
type Session struct {
	owner      *DB
	raw        *sql.Conn
	scope      *operationScope
	classify   classifier
	discard    atomic.Bool
	observers  lifecycle.Observers
	timeSource clock.Clock
}

// Session owns one connection until its callback and all cleanup have exited.
// Operations must be sequential; an escaped Session rejects later use. A scope
// with unfinished work is canceled and discarded instead of returned to the pool.
func (db *DB) Session(ctx context.Context, fn func(*Session) error) (err error) {
	if fn == nil {
		return fault.New(fault.Invalid, "database session needs a callback")
	}
	conn, release, err := db.acquire(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := release(); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
	}()
	lifetime, cancel := context.WithCancel(ctx)
	defer cancel()
	session := &Session{owner: db, raw: conn, scope: &operationScope{ctx: lifetime, cancel: cancel}, classify: db.classify, observers: db.Observers(), timeSource: db.Clock()}
	err = invokeScope("database session", func() error { return fn(session) })
	err = errors.Join(err, session.scope.finish())
	if err == nil {
		err = db.classify.wrap("session", lifetime.Err())
	}
	if err != nil {
		session.Discard()
	}
	if session.discard.Load() {
		err = errors.Join(err, db.classify.wrap("discard session", discardConnection(conn)))
	}
	// A session has no transaction outcome to attach. Callback failures, such as
	// migration checksum drift, keep their own identity and message; database
	// operations inside the callback already returned classified *Error values.
	return err
}

// Discard prevents this connection from returning to the pool after the callback.
// Use it whenever session state, such as an advisory lock, cannot be cleaned up.
func (s *Session) Discard() { s.discard.Store(true) }

// Exec and Query on a session may write, so with WithStickyReads they mark
// the request scope when they start and when they complete.
func (s *Session) Exec(ctx context.Context, statement string, arguments ...any) (Result, error) {
	s.owner.markWrite(ctx)
	defer s.owner.markWrite(ctx)
	return s.scope.exec(ctx, s.raw, s.classify, s.owner.instrumented(PrimaryPool), statement, arguments)
}

func (s *Session) Query(ctx context.Context, statement string, arguments ...any) (*Rows, error) {
	s.owner.markWrite(ctx)
	return s.owner.markOnClose(ctx)(s.scope.query(ctx, s.raw, s.classify, s.owner.instrumented(PrimaryPool), s.observers, statement, arguments))
}

// Transaction runs on this session's existing connection. Unlike DB.Transaction,
// the session still retains the connection while after-commit callbacks run. Such
// callbacks must not reenter this session, and reacquiring the pool needs capacity
// for an additional connection. No physical commit is hidden inside a savepoint.
func (s *Session) Transaction(ctx context.Context, fn func(*Tx) error, options ...TxOptions) error {
	sqlOptions, err := transactionOptions(fn, options)
	if err != nil {
		return err
	}
	release, err := s.scope.enter()
	if err != nil {
		return err
	}
	defer release()
	if sqlOptions == nil || !sqlOptions.ReadOnly {
		s.owner.markWrite(ctx)
	}
	operation, cancel := s.scope.context(ctx)
	defer cancel()
	return runTransaction(operation, s.raw, s.owner, s.classify, s.observers, s.timeSource, s.owner.config.CommitTimeout, func() error { return nil }, fn, sqlOptions)
}

func discardConnection(conn *sql.Conn) error {
	err := conn.Raw(func(any) error { return driver.ErrBadConn })
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return nil
	}
	return err
}
