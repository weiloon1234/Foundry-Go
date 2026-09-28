package idempotency

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Code contains safe public outcome categories, never stored input or key data.
type Code string

const (
	BadKey      Code = "idempotency_bad_key"
	Mismatch    Code = "idempotency_mismatch"
	InProgress  Code = "idempotency_in_progress"
	Capacity    Code = "idempotency_capacity"
	Unavailable Code = "idempotency_unavailable"
)

func (c Code) Error() string { return string(c) }

// Error preserves a private operational cause for diagnostics; never log Unwrap.
type Error struct {
	code  Code
	cause error
}

func (e *Error) Error() string              { return e.code.Error() }
func (e *Error) Unwrap() error              { return e.cause }
func (e *Error) Is(target error) bool       { return target == e.code }
func (e *Error) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte(e.Error())) }
func failure(code Code, cause error) error  { return &Error{code: code, cause: cause} }
func invalid(message string) error          { return fault.New(fault.Invalid, message) }
