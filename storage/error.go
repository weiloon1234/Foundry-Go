package storage

import (
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/value"
)

// Code is comparable with errors.Is; provider causes are retained but redacted.
type Code string

const (
	NotFound            Code = "storage_not_found"
	Forbidden           Code = "storage_forbidden"
	Unsupported         Code = "storage_unsupported"
	PreconditionFailed  Code = "storage_precondition_failed"
	RangeNotSatisfiable Code = "storage_range_not_satisfiable"
	LimitExceeded       Code = "storage_limit_exceeded"
	Invalid             Code = "storage_invalid"
	Unavailable         Code = "storage_unavailable"
	IntegrityFailed     Code = "storage_integrity_failed"
	Closed              Code = "storage_closed"
)

// Error returns the classification string.
func (c Code) Error() string { return string(c) }

// Operation identifies the storage action associated with a failure.
type Operation string

const (
	PutOperation    Operation = "put"
	OpenOperation   Operation = "open"
	StatOperation   Operation = "stat"
	DeleteOperation Operation = "delete"
	ListOperation   Operation = "list"
	CopyOperation   Operation = "copy"
	MoveOperation   Operation = "move"
	SignOperation   Operation = "sign"
	CloseOperation  Operation = "close"
)

// Outcome describes visibility of a mutation, not an instruction to retry.
// Applied errors can follow successful publication but failed cleanup/durability.
// Unknown means the caller must reconcile; no automatic mutation retry is safe.
type Outcome uint8

const (
	NotApplicable Outcome = iota
	Unchanged
	Applied
	Unknown
)

// CleanupID identifies provider cleanup work (for example a multipart upload).
// It is opaque diagnostic data; ordinary formatting deliberately hides it.
type CleanupID struct{ text string }

// NewCleanupID wraps an opaque cleanup reference without validating it.
func NewCleanupID(text string) CleanupID { return CleanupID{text: text} }

// Value explicitly returns the opaque cleanup reference.
func (id CleanupID) Value() string { return id.text }

// Format writes a label without exposing the cleanup reference.
func (CleanupID) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("storage cleanup reference")) }

// Error retains a storage classification, operation, mutation outcome and redacted cause.
type Error struct {
	code      Code
	operation Operation
	outcome   Outcome
	cause     error
	cleanup   value.Optional[CleanupID]
}

// Failure creates a classified error retaining cause without formatting it.
func Failure(code Code, op Operation, outcome Outcome, cause error) *Error {
	return &Error{code: code, operation: op, outcome: outcome, cause: cause}
}

// Error returns the classification and operation without formatting the cause.
func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.code, e.operation) }

// Format writes the classification and operation without exposing the cause.
func (e *Error) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte(e.Error())) }

// LogValue returns the classification and operation without exposing the cause.
func (e *Error) LogValue() slog.Value { return slog.StringValue(e.Error()) }

// Unwrap returns the retained cause.
func (e *Error) Unwrap() error { return e.cause }

// Is reports whether target is the same storage Code.
func (e *Error) Is(target error) bool { code, ok := target.(Code); return ok && code == e.code }

// Code returns the storage failure classification.
func (e *Error) Code() Code { return e.code }

// Operation returns the operation that failed.
func (e *Error) Operation() Operation { return e.operation }

// Outcome returns the reported mutation visibility.
func (e *Error) Outcome() Outcome { return e.outcome }

// Cleanup returns the optional provider cleanup reference.
func (e *Error) Cleanup() value.Optional[CleanupID] { return e.cleanup }

// WithCleanup returns a copy carrying id, leaving the receiver unchanged.
func (e *Error) WithCleanup(id CleanupID) *Error {
	next := *e
	next.cleanup = value.Set(id)
	return &next
}
