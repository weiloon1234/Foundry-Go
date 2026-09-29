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

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
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
	// QueryCanceled is a server-side cancellation, such as statement_timeout or
	// an administrator request. Caller context cancellation remains Canceled.
	QueryCanceled Code = "query_canceled"
	// LockNotAvailable reports NOWAIT or lock_timeout expiry. The statement did
	// not acquire its lock; retry policy belongs to the caller.
	LockNotAvailable          Code = "lock_not_available"
	ExclusionViolation        Code = "exclusion_violation"
	RestrictViolation         Code = "restrict_violation"
	ReadOnlyTransaction       Code = "read_only_sql_transaction"
	StringDataRightTruncation Code = "string_data_right_truncation"
	InvalidTextRepresentation Code = "invalid_text_representation"
	NumericValueOutOfRange    Code = "numeric_value_out_of_range"
	InvalidDatetimeFormat     Code = "invalid_datetime_format"
	DatetimeFieldOverflow     Code = "datetime_field_overflow"
	DivisionByZero            Code = "division_by_zero"
	// DataException covers other invalid values rejected by the server.
	DataException Code = "data_exception"
	ObjectInUse   Code = "object_in_use"
	// InsufficientResources covers server disk, memory and configured limits.
	// Connection-slot exhaustion remains Unavailable.
	InsufficientResources    Code = "insufficient_resources"
	IdleInTransactionTimeout Code = "idle_in_transaction_timeout"
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
// StatementRejected is true only when the adapter confirms a failed statement
// outside an explicit transaction did not commit: it was never sent, or the
// server rejected it before its implicit transaction could complete. Adapters
// must leave it false for connection loss, cancellation or unknown completion.
type Detail struct {
	Code              Code
	SQLState          string
	Constraint        string
	CommitRejected    bool
	StatementRejected bool
}

// Error retains the driver cause while formatting only operation and code.
// Unwrapped causes may contain SQL, values or credentials; do not log them.
type Error struct {
	operation string
	detail    Detail
	outcome   Outcome
	cause     error
	// notSent follows database/sql's ErrBadConn contract: the operation was
	// not performed and may be retried on another connection.
	notSent bool
}

func (e *Error) Error() string        { return "database " + e.operation + ": " + e.detail.Code.Error() }
func (e *Error) GoString() string     { return e.Error() }
func (e *Error) Unwrap() error        { return e.cause }
func (e *Error) Is(target error) bool { code, ok := target.(Code); return ok && e.detail.Code == code }
func (e *Error) Code() Code           { return e.detail.Code }
func (e *Error) SQLState() string     { return e.detail.SQLState }
func (e *Error) Constraint() string   { return e.detail.Constraint }
func (e *Error) Outcome() Outcome     { return e.outcome }

// FoundryDiagnostic contributes the safe classification to redacted failure
// diagnostics: operation, code, SQLSTATE, constraint and outcome only.
func (e *Error) FoundryDiagnostic(errordiag.Seal) []fault.Attribute {
	if e == nil {
		return nil
	}
	attributes := []fault.Attribute{{Key: "database_operation", Value: e.operation}, {Key: "database_code", Value: string(e.detail.Code)}}
	if e.detail.SQLState != "" {
		attributes = append(attributes, fault.Attribute{Key: "sqlstate", Value: e.detail.SQLState})
	}
	if e.detail.Constraint != "" {
		attributes = append(attributes, fault.Attribute{Key: "constraint", Value: e.detail.Constraint})
	}
	if e.outcome != "" && e.outcome != Unspecified {
		attributes = append(attributes, fault.Attribute{Key: "database_outcome", Value: string(e.outcome)})
	}
	return attributes
}

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

// invokeScope runs a scope callback in an owned goroutine. An ordinary returned
// error keeps its exact identity and formatting; panic and Goexit still become
// callback isolation faults.
func invokeScope(operation string, fn func() error) error {
	var returned error
	isolated := callback.Isolated(operation, func() error {
		returned = fn()
		return returned
	})
	if isolated != nil && returned != nil {
		return returned
	}
	return isolated
}

// scope classifies a callback scope's combined failure. Failures containing a
// framework database classification, or whose graph cannot be inspected
// completely, become *Error so the owner can attach its outcome. Application
// callback errors keep their own identity and formatting; the owner reports
// any rollback uncertainty separately.
func (classify classifier) scope(operation string, err error) (*Error, error) {
	if err == nil {
		return nil, nil
	}
	var result *Error
	application := false
	failed := callback.Isolated("classify database scope failure", func() error {
		found := false
		complete := errorgraph.Walk(err, func(current error) bool {
			_, found = errorgraph.AsShallow[*Error](current)
			return !found
		})
		if complete && !found {
			application = true
			return nil
		}
		result = classify.inspect(operation, err)
		return nil
	})
	if failed != nil {
		return &Error{operation: operation, detail: Detail{Code: QueryFailed}, outcome: Unspecified, cause: failed}, nil
	}
	if application {
		return nil, err
	}
	return result, nil
}

// scoped returns an application failure unchanged or its database classification.
func (classify classifier) scoped(operation string, err error) error {
	classified, application := classify.scope(operation, err)
	if classified != nil {
		return classified
	}
	return application
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
	return &Error{operation: operation, detail: detail, outcome: Unspecified, cause: err, notSent: errors.Is(err, driver.ErrBadConn)}
}

// statementOutcome labels a failure of one statement outside an explicit
// transaction. Only adapter-confirmed rejection or database/sql's not-performed
// contract proves no commit; every other failure after acquisition is Unknown.
func statementOutcome(err error) error {
	classified, ok := err.(*Error)
	if !ok || classified == nil {
		return err
	}
	switch {
	case classified.notSent:
		classified.outcome = NoCommit
	case classified.detail.StatementRejected:
		classified.outcome = RolledBack
	default:
		classified.outcome = Unknown
		classified.detail.Code = CommitUnknown
	}
	return classified
}

func failure(operation string, code Code) error {
	return &Error{operation: operation, detail: Detail{Code: code}, outcome: Unspecified}
}

// poolExhausted marks an AcquireTimeout expiry while the caller's context was
// still live: temporary capacity exhaustion before the operation started. The
// classification code is kept; fault.Overloaded joins the cause so transports
// report a retryable 503 with a short retry hint.
func poolExhausted(err error) error {
	classified, ok := err.(*Error)
	if !ok || classified == nil {
		return err
	}
	classified.cause = errors.Join(fault.New(fault.Overloaded, "database pool acquisition timed out"), classified.cause)
	classified.notSent = true
	return classified
}

// FoundryStatementUnknown labels a failure observed after a framework
// autocommit statement was sent, such as a result that could not be decoded:
// the statement's implicit transaction may already have committed.
func FoundryStatementUnknown(_ sqlowner.Seal, operation string, cause error) error {
	return &Error{operation: operation, detail: Detail{Code: CommitUnknown}, outcome: Unknown, cause: cause}
}

// NewError constructs a classified framework failure without a driver cause.
// Typed query layers use it for results such as a required row that is absent,
// so every database outcome is inspected through the same *Error contract.
func NewError(operation string, code Code) error {
	if code == "" {
		code = QueryFailed
	}
	return failure(operation, code)
}
