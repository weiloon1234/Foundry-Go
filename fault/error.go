// Package fault provides transport-independent, classifiable framework errors.
package fault

import (
	"fmt"
	"slices"
)

// Code classifies an error without requiring callers to inspect its message.
// Codes implement error so errors.Is(err, fault.Invalid) is supported.
type Code string

const (
	Invalid   Code = "invalid"
	Duplicate Code = "duplicate"
	Missing   Code = "missing"
	Cycle     Code = "cycle"
	Closed    Code = "closed"
	Conflict  Code = "conflict"
	Internal  Code = "internal"
	Panicked  Code = "panicked"
	Timeout   Code = "timeout"
	// Overloaded reports temporarily exhausted concurrency or queue capacity.
	// The operation was not started; the same request may be retried later.
	// Transports map it to "service unavailable" rather than an internal error.
	Overloaded Code = "overloaded"
)

func (c Code) Error() string { return string(c) }

// Error retains an internal cause while exposing only its explicit safe message
// through formatting. Causes remain available through errors.Is/errors.As.
type Error struct {
	code    Code
	message string
	cause   error
	frames  []Frame
}

// New constructs an error with a safe diagnostic message.
func New(code Code, message string) *Error {
	return &Error{code: code, message: message}
}

// Wrap adds classification and a safe message without formatting the cause.
func Wrap(code Code, message string, cause error) *Error {
	return &Error{code: code, message: message, cause: cause}
}

// Panic constructs a Panicked fault retaining where the contained panic began.
// Frames carry source locations only, never argument values or the panic value.
func Panic(message string, frames []Frame) *Error {
	return &Error{code: Panicked, message: message, frames: slices.Clone(frames)}
}

func (e *Error) Error() string    { return fmt.Sprintf("%s: %s", e.code, e.message) }
func (e *Error) GoString() string { return e.Error() }
func (e *Error) Unwrap() error    { return e.cause }
func (e *Error) Code() Code       { return e.code }
func (e *Error) Message() string  { return e.message }
func (e *Error) Is(target error) bool {
	code, ok := target.(Code)
	return ok && e.code == code
}

// Frames returns an owned copy of a contained panic's source locations.
func (e *Error) Frames() []Frame { return slices.Clone(e.frames) }
