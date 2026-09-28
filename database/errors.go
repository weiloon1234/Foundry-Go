// Package database owns execution and transaction contracts shared by Foundry's
// generated query APIs and infrastructure adapters. SQL strings and scan targets
// are explicit raw boundaries; model-specific compile-time guarantees belong to
// generated APIs rather than arbitrary SQL.
package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// Code classifies a database failure without matching driver error messages.
// Use errors.Is(err, database.UniqueViolation), for example.
type Code string

const (
	QueryFailed          Code = "query_failed"
	NotFound             Code = "not_found"
	TooManyRows          Code = "too_many_rows"
	Unavailable          Code = "unavailable"
	Canceled             Code = "canceled"
	DeadlineExceeded     Code = "deadline_exceeded"
	Closed               Code = "closed"
	NotReady             Code = "not_ready"
	Busy                 Code = "busy"
	UniqueViolation      Code = "unique_violation"
	ForeignKeyViolation  Code = "foreign_key_violation"
	CheckViolation       Code = "check_violation"
	NotNullViolation     Code = "not_null_violation"
	SerializationFailure Code = "serialization_failure"
	Deadlock             Code = "deadlock"
	CommitUnknown        Code = "commit_unknown"
	AfterCommitFailed    Code = "after_commit_failed"
)

func (c Code) Error() string { return string(c) }

// Outcome distinguishes a transaction's database outcome from callback success.
type Outcome string

const (
	Unspecified Outcome = "unspecified"
	NoCommit    Outcome = "no_commit"
	Committed   Outcome = "committed"
	RolledBack  Outcome = "rolled_back"
	Unknown     Outcome = "unknown"
)

// Detail is the safe, structured classification produced by a driver adapter.
// Constraint and SQLState are metadata, never query text, arguments or detail
// messages. CommitRejected is true only when the server confirms no commit.
type Detail struct {
	Code           Code
	SQLState       string
	Constraint     string
	CommitRejected bool
}

// Error retains the driver cause while formatting only operation and code.
// Unwrapped causes may contain SQL, values or credentials; do not log them.
type Error struct {
	operation string
	detail    Detail
	outcome   Outcome
	cause     error
}

func (e *Error) Error() string        { return "database " + e.operation + ": " + e.detail.Code.Error() }
func (e *Error) GoString() string     { return e.Error() }
func (e *Error) Unwrap() error        { return e.cause }
func (e *Error) Is(target error) bool { code, ok := target.(Code); return ok && e.detail.Code == code }
func (e *Error) Code() Code           { return e.detail.Code }
func (e *Error) SQLState() string     { return e.detail.SQLState }
func (e *Error) Constraint() string   { return e.detail.Constraint }
func (e *Error) Outcome() Outcome     { return e.outcome }

type classifier func(error) Detail

func (classify classifier) wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	// Error traversal is extension code too, including returned callback errors.
	// Keep the pool/transaction owner until inspection actually exits.
	var result *Error
	failed := callback.Isolated("classify database failure", func() error {
		result = classify.inspect(operation, err)
		return nil
	})
	if failed != nil {
		return &Error{operation: operation, detail: Detail{Code: QueryFailed}, outcome: Unspecified, cause: failed}
	}
	return result
}

func (classify classifier) inspect(operation string, err error) *Error {
	var prior *Error
	matched := false
	complete := errorgraph.Walk(err, func(current error) bool {
		prior, matched = errorgraph.AsShallow[*Error](current)
		return !matched
	})
	if !complete || matched && prior == nil {
		// Do not hand an unbounded graph to the driver classifier. Preserve the
		// private cause, while transaction ownership determines commit/rollback.
		return &Error{operation: operation, detail: Detail{Code: QueryFailed}, outcome: Unspecified, cause: err}
	}
	if matched {
		return &Error{operation: operation, detail: prior.detail, outcome: prior.outcome, cause: err}
	}
	detail := Detail{Code: QueryFailed}
	if classify != nil {
		detail = classify(err)
	}
	if detail.Code == "" {
		detail.Code = QueryFailed
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		detail.Code = DeadlineExceeded
	case errors.Is(err, context.Canceled):
		detail.Code = Canceled
	case errors.Is(err, sql.ErrNoRows):
		detail.Code = NotFound
	case errors.Is(err, sql.ErrConnDone), errors.Is(err, sql.ErrTxDone):
		detail.Code = Closed
	case errors.Is(err, driver.ErrBadConn):
		detail.Code = Unavailable
	}
	return &Error{operation: operation, detail: detail, outcome: Unspecified, cause: err}
}

func failure(operation string, code Code) error {
	return &Error{operation: operation, detail: Detail{Code: code}, outcome: Unspecified}
}
