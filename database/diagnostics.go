package database

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// presentFailure runs only for fresh internal classifications after the owner
// settles their outcome. Public NewError may contain application operation text.
func presentFailure(failure *Error) error {
	if failure == nil {
		return nil
	}
	failure.captureDiagnostic(true)
	if _, owned := safeerror.OwnedMessage(failure.cause); owned {
		return safeerror.WithAttributes(safeerror.Retain(failure.cause, failure), failure, failure.diagnostic)
	}
	// Adapter and copied classifications can carry arbitrary Code strings.
	// Only framework constants are diagnostic text; metadata stays structural.
	if knownCode(failure.detail.Code) {
		return safeerror.WithAttributes(safeerror.Owned(failure.Error(), failure), failure, failure.diagnostic)
	}
	return failure
}

func knownCode(code Code) bool {
	switch code {
	case QueryFailed, NotFound, TooManyRows, Unavailable, Canceled,
		DeadlineExceeded, Closed, NotReady, Busy, UniqueViolation,
		ForeignKeyViolation, CheckViolation, NotNullViolation,
		SerializationFailure, Deadlock, CommitUnknown, AfterCommitFailed,
		QueryCanceled, LockNotAvailable, ExclusionViolation, RestrictViolation,
		ReadOnlyTransaction, StringDataRightTruncation, InvalidTextRepresentation,
		NumericValueOutOfRange, InvalidDatetimeFormat, DatetimeFieldOverflow,
		DivisionByZero, DataException, ObjectInUse, InsufficientResources,
		IdleInTransactionTimeout:
		return true
	default:
		return false
	}
}

func (e *Error) captureDiagnostic(internal bool) {
	attributes := []fault.Attribute{}
	if knownCode(e.detail.Code) {
		attributes = append(attributes, fault.Attribute{Key: "database_code", Value: string(e.detail.Code)})
	}
	// Operations belong to the framework execution owner. Public constructors and
	// adapter metadata are not diagnostic authorities.
	if internal {
		attributes = append(attributes, fault.Attribute{Key: "database_operation", Value: e.operation})
	}
	switch e.outcome {
	case NoCommit, Committed, RolledBack, Unknown:
		attributes = append(attributes, fault.Attribute{Key: "database_outcome", Value: string(e.outcome)})
	}
	e.diagnostic = attributes
}

func diagnosticCode(code Code) string {
	if code == "" || knownCode(code) {
		return string(code)
	}
	return string(QueryFailed)
}

// diagnosticState is a reporting allowlist, not the adapter's classifier. Only
// these public protocol constants may be displayed; unknown metadata stays in
// Detail for explicit reconciliation. Never accept arbitrary five-byte text.
func diagnosticState(state string) string {
	switch state {
	case "23505", "23503", "23502", "23514", "23P01", "23001",
		"40001", "40P01", "55P03", "55006", "57014", "25006", "25P03",
		"22001", "22P02", "22003", "22007", "22008", "22012", "53300",
		"57P01", "57P02", "57P03", "57P04", "57P05", "28P01", "28000":
		return state
	}
	return ""
}
