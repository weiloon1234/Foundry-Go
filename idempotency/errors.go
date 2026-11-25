package idempotency

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Code contains safe public outcome categories, never stored input or key data.
type Code string

const (
	// BadKey reports an idempotency key that fails ParseKey or exceeds the store's maximum key length.
	BadKey Code = "idempotency_bad_key"
	// Mismatch reports a key already used with a different input fingerprint.
	Mismatch Code = "idempotency_mismatch"
	// InProgress reports that another submission with the same key has not yet committed; retry with the same key.
	InProgress Code = "idempotency_in_progress"
	Capacity   Code = "idempotency_capacity"
	// Unavailable reports that the outcome could not be claimed, stored or replayed, including local admission being exhausted; retry with the same scope, key and input, but a stored outcome that cannot be replayed keeps returning it.
	Unavailable Code = "idempotency_unavailable"
)

// Error returns the error code text.
func (c Code) Error() string { return string(c) }

// Error preserves a private operational cause for diagnostics; never log Unwrap.
type Error struct {
	code  Code
	cause error
}

// Error returns the code without formatting the underlying cause.
func (e *Error) Error() string { return e.code.Error() }

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error { return e.cause }

// Is reports whether target is this error's code.
func (e *Error) Is(target error) bool { return target == e.code }

// Format writes the error code without formatting its cause.
func (e *Error) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte(e.Error())) }
func failure(code Code, cause error) error {
	if cause == nil {
		cause = code
	}
	failure := &Error{code: code, cause: cause}
	return safeerror.Owned(failure.Error(), failure)
}
func invalid(message string) error { return safeerror.NewFault(fault.Invalid, message) }
