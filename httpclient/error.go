package httpclient

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// ErrorKind identifies an outbound HTTP failure category.
type ErrorKind string

const (
	InvalidRequest  ErrorKind = "invalid_request"
	TransportFailed ErrorKind = "transport_failed"
	BodyFailed      ErrorKind = "body_failed"
	StatusFailed    ErrorKind = "status_failed"
	DecodeFailed    ErrorKind = "decode_failed"
	CallbackFailed  ErrorKind = "callback_failed"
	// Overloaded reports that the client's queued operation admission was
	// exhausted; no request was sent. It matches fault.Overloaded.
	Overloaded ErrorKind = "overloaded"
)

// Error exposes stable operation metadata, never URL, header, query, body or
// underlying transport text. The cause is available only through explicit inspection.
type Error struct {
	kind            ErrorKind
	name            Name
	method          string
	status, attempt int
	cause           error
}

// Error returns the failure kind, client name, attempt and status without formatting
// its cause.
func (e *Error) Error() string {
	return fmt.Sprintf("outbound HTTP %s (%s, attempt %d, status %d)", e.kind, e.name, e.attempt, e.status)
}

// Format writes the safe error summary without formatting its cause.
func (e *Error) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte(e.Error())) }

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error { return e.cause }

// Kind returns the outbound failure category.
func (e *Error) Kind() ErrorKind { return e.kind }

// Name returns the client name recorded for the failure.
func (e *Error) Name() Name { return e.name }

// Method returns the request method recorded for the failure.
func (e *Error) Method() string { return e.method }

// Status returns the HTTP status recorded for the failure.
func (e *Error) Status() int { return e.status }

// Attempt returns the attempt number recorded for the failure.
func (e *Error) Attempt() int { return e.attempt }

// Is matches invalid, internal and overloaded framework faults according to the failure
// kind.
func (e *Error) Is(target error) bool {
	if target == fault.Invalid {
		return e.kind == InvalidRequest || e.kind == DecodeFailed
	}
	if target == fault.Internal {
		return e.kind == TransportFailed || e.kind == BodyFailed || e.kind == CallbackFailed
	}
	if target == fault.Overloaded {
		return e.kind == Overloaded
	}
	return false
}
func invalid() error {
	return safeerror.NewFault(fault.Invalid, "invalid outbound HTTP declaration or request")
}
func failure(kind ErrorKind, name Name, method string, attempt, status int, cause error) error {
	failure := &Error{kind: kind, name: name, method: method, attempt: attempt, status: status, cause: cause}
	return safeerror.Owned(failure.Error(), failure)
}
func canceled(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
