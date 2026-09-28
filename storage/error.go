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

func (c Code) Error() string { return string(c) }

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

func NewCleanupID(text string) CleanupID     { return CleanupID{text: text} }
func (id CleanupID) Value() string           { return id.text }
func (CleanupID) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("storage cleanup reference")) }

type Error struct {
	code      Code
	operation Operation
	outcome   Outcome
	cause     error
	cleanup   value.Optional[CleanupID]
}

func Failure(code Code, op Operation, outcome Outcome, cause error) *Error {
	return &Error{code: code, operation: op, outcome: outcome, cause: cause}
}
func (e *Error) Error() string                      { return fmt.Sprintf("%s: %s", e.code, e.operation) }
func (e *Error) Format(s fmt.State, _ rune)         { _, _ = s.Write([]byte(e.Error())) }
func (e *Error) LogValue() slog.Value               { return slog.StringValue(e.Error()) }
func (e *Error) Unwrap() error                      { return e.cause }
func (e *Error) Is(target error) bool               { code, ok := target.(Code); return ok && code == e.code }
func (e *Error) Code() Code                         { return e.code }
func (e *Error) Operation() Operation               { return e.operation }
func (e *Error) Outcome() Outcome                   { return e.outcome }
func (e *Error) Cleanup() value.Optional[CleanupID] { return e.cleanup }
func (e *Error) WithCleanup(id CleanupID) *Error {
	next := *e
	next.cleanup = value.Set(id)
	return &next
}
