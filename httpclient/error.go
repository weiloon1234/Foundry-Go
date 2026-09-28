package httpclient

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type ErrorKind string

const (
	InvalidRequest  ErrorKind = "invalid_request"
	TransportFailed ErrorKind = "transport_failed"
	BodyFailed      ErrorKind = "body_failed"
	StatusFailed    ErrorKind = "status_failed"
	DecodeFailed    ErrorKind = "decode_failed"
	CallbackFailed  ErrorKind = "callback_failed"
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

func (e *Error) Error() string {
	return fmt.Sprintf("outbound HTTP %s (%s, attempt %d, status %d)", e.kind, e.name, e.attempt, e.status)
}
func (e *Error) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte(e.Error())) }
func (e *Error) Unwrap() error                  { return e.cause }
func (e *Error) Kind() ErrorKind                { return e.kind }
func (e *Error) Name() Name                     { return e.name }
func (e *Error) Method() string                 { return e.method }
func (e *Error) Status() int                    { return e.status }
func (e *Error) Attempt() int                   { return e.attempt }
func (e *Error) Is(target error) bool {
	if target == fault.Invalid {
		return e.kind == InvalidRequest || e.kind == DecodeFailed
	}
	if target == fault.Internal {
		return e.kind == TransportFailed || e.kind == BodyFailed || e.kind == CallbackFailed
	}
	return false
}
func invalid() error { return fault.New(fault.Invalid, "invalid outbound HTTP declaration or request") }
func failure(kind ErrorKind, name Name, method string, attempt, status int, cause error) error {
	return &Error{kind: kind, name: name, method: method, attempt: attempt, status: status, cause: cause}
}
func canceled(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
