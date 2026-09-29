package http

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// ErrorDeclaration is an immutable application failure with explicit public
// metadata. Return it directly as an ordinary Go error, or use WithCause to
// retain private diagnostic context. An invalid or zero declaration is never
// published; Validate and endpoint assembly report its configuration error.
type ErrorDeclaration struct{ definition ErrorDefinition }

// DefineError declares a stable code, error status (400–599), and static public
// message. The same value supplies runtime responses and endpoint metadata.
// Built-in codes are reserved. MessageDefinition enables localized messages.
func DefineError(code ErrorCode, status int, message string) ErrorDeclaration {
	return ErrorDeclaration{definition: ErrorDefinition{Code: code, Status: status, Message: message}}
}

const maxErrorMessageBytes = 16384

func (d ErrorDeclaration) Validate() error {
	info := d.definition
	if !identifier.Semantic(string(info.Code)) || info.Status < 400 || info.Status > 599 ||
		len(info.Message) > maxErrorMessageBytes || strings.TrimSpace(info.Message) == "" ||
		!utf8.ValidString(info.Message) || strings.ContainsRune(info.Message, 0) {
		return fault.New(fault.Invalid, "HTTP error requires a semantic code, error status and bounded public message")
	}
	if _, reserved := info.Code.definition(); reserved {
		return fault.New(fault.Conflict, "built-in HTTP error codes cannot be redeclared")
	}
	return nil
}

// Description validates and returns a value snapshot. Editing it does not
// change this declaration or any already registered endpoint.
func (d ErrorDeclaration) Description() (ErrorDefinition, error) {
	if err := d.Validate(); err != nil {
		return ErrorDefinition{}, err
	}
	return d.definition, nil
}

// MessageDefinition returns the parameter-free catalog signature
// (http.error.<code>) that localizes this error's public message in
// locale-enabled HTTP applications. Register it with the application's message
// declarations and supply translations; locales without one keep the declared
// message. Code, status and endpoint metadata are unchanged.
func (d ErrorDeclaration) MessageDefinition() (i18n.MessageDefinition, error) {
	info, err := d.Description()
	if err != nil {
		return i18n.MessageDefinition{}, err
	}
	// A code near the identifier length bound cannot carry the key prefix.
	definition := i18n.MessageDefinition{Key: errorMessageKey(info.Code)}
	if err := definition.Validate(); err != nil {
		return i18n.MessageDefinition{}, err
	}
	return definition, nil
}

func (d ErrorDeclaration) Error() string { return string(d.httpErrorCode()) }
func (d ErrorDeclaration) httpErrorCode() ErrorCode {
	if d.Validate() != nil {
		return InternalError
	}
	return d.definition.Code
}
func (d ErrorDeclaration) httpErrorDefinition() (ErrorDefinition, bool) {
	info, err := d.Description()
	return info, err == nil
}

// WithCause preserves errors.Is/errors.As and formats only the public code.
// Causes are never used as response messages or contract metadata.
func (d ErrorDeclaration) WithCause(cause error) error {
	return &declaredResponseError{ErrorDeclaration: d, cause: cause}
}

type declaredResponseError struct {
	ErrorDeclaration
	cause error
}

func (e *declaredResponseError) Unwrap() error        { return e.cause }
func (e *declaredResponseError) Is(target error) bool { return target == e.ErrorDeclaration }

// WithErrors adds application errors this endpoint may expose. Built-in errors
// remain available through the shared ErrorDefinitions catalog. Reuse the same
// declaration in the handler and here; duplicate codes reject assembly.
// Returning an undeclared or differently described custom error produces a safe
// internal failure, so the public response agrees with the declared contract.
func (e Endpoint[P, Q, B, R]) WithErrors(declarations ...ErrorDeclaration) Endpoint[P, Q, B, R] {
	e.errors = append(slices.Clone(e.errors), declarations...)
	return e
}

func (e Endpoint[P, Q, B, R]) errorDefinitions() ([]ErrorDefinition, error) {
	definitions := make([]ErrorDefinition, 0, len(e.errors))
	seen := make(map[ErrorCode]bool, len(e.errors))
	for _, declaration := range e.errors {
		definition, err := declaration.Description()
		if err != nil {
			return nil, err
		}
		if seen[definition.Code] {
			return nil, fault.New(fault.Duplicate, "endpoint error code is already declared")
		}
		seen[definition.Code] = true
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func allowsError(ctx context.Context, definition ErrorDefinition) bool {
	state, matched := ctx.Value(matchedRouteKey{}).(*matchedRoute)
	if !matched || state == nil || !state.typed {
		return true
	} // Explicit raw transport has no typed endpoint contract.
	return slices.Contains(state.errors, definition)
}

// ErrorDefinitions returns all built-in and declared application failures in
// code order. Conflicting declarations are rejected when the router is built.
// This snapshot and EndpointInfo.Errors share the actual response declarations.
func (r *Router) ErrorDefinitions() []ErrorDefinition {
	if r == nil {
		return nil
	}
	return slices.Clone(r.errors)
}
