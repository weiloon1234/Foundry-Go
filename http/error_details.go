package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ErrorDeclarationOption is the sealed declaration accepted by Errors.
// Plain and detailed declarations may be mixed in one endpoint.
// Example: Errors(SeatUnavailable, RegistryExists).
type ErrorDeclarationOption interface{ errorDeclarationOption() errorDeclaration }

type errorDeclaration struct {
	definition ErrorDefinition
	details    *errorDetails
}

type errorDetails struct {
	schema      func() (contract.Schema, error)
	fingerprint [sha256.Size]byte
	err         error
}

func (d ErrorDeclaration) errorDeclarationOption() errorDeclaration {
	return errorDeclaration{definition: d.definition}
}

// Description returns an owned value snapshot of the declaration.
// Example: definition, err := RegistryExists.Description().
func (d errorDeclaration) Description() (ErrorDefinition, error) {
	if err := (ErrorDeclaration{definition: d.definition}).Validate(); err != nil {
		return ErrorDefinition{}, err
	}
	if d.details != nil {
		if d.details.err != nil {
			return ErrorDefinition{}, d.details.err
		}
		if d.definition.Status == 401 {
			return ErrorDefinition{}, safeerror.NewFault(fault.Invalid, "HTTP error details cannot use status 401")
		}
	}
	return d.definition, nil
}

// DetailedErrorDeclaration requires a concrete payload before it becomes an error.
// It is immutable and does not itself implement error.
// Example: RegistryExists.New(RegistryExistsDetails{ExistingRegistryID: id}).
type DetailedErrorDeclaration[D any] struct {
	errorDeclaration
	descriptor contract.JSON[D]
}

// DefineErrorDetails declares a public error and its typed details descriptor.
// Details are published only by endpoints that declare this exact definition.
// Example: DefineErrorDetails("registry_exists", 409, "Registry already exists.", RegistryExistsDetailsJSON()).
func DefineErrorDetails[D any](code ErrorCode, status int, message string, descriptor contract.JSON[D]) DetailedErrorDeclaration[D] {
	details := &errorDetails{schema: descriptor.Description}
	schema, err := descriptor.Description()
	if err == nil {
		err = contract.RejectPasswordOutput(schema, "HTTP error details")
	}
	if err == nil {
		var data []byte
		data, err = json.Marshal(schema)
		if err == nil {
			details.fingerprint = sha256.Sum256(data)
		}
	}
	details.err = err
	return DetailedErrorDeclaration[D]{errorDeclaration: errorDeclaration{
		definition: ErrorDefinition{Code: code, Status: status, Message: message, DetailsType: schema.Root},
		details:    details,
	}, descriptor: descriptor}
}

func (d errorDeclaration) errorDeclarationOption() errorDeclaration { return d }

// Validate checks the public definition and the details output schema.
// Example: err := RegistryExists.Validate().
func (d errorDeclaration) Validate() error {
	_, err := d.Description()
	return err
}

// MessageDefinition returns the parameter-free http.error.<code> catalog signature.
// Example: message, err := RegistryExists.MessageDefinition().
func (d errorDeclaration) MessageDefinition() (i18n.MessageDefinition, error) {
	definition, err := d.Description()
	if err != nil {
		return i18n.MessageDefinition{}, err
	}
	return (ErrorDeclaration{definition: definition}).MessageDefinition()
}

// New binds a payload to this declaration without encoding it. Keep the payload
// unchanged until the response is written, as with contract.JSON.Encode.
// Example: return Created{}, RegistryExists.New(RegistryExistsDetails{ExistingRegistryID: id}).
func (d DetailedErrorDeclaration[D]) New(payload D) *DetailedError {
	return &DetailedError{declaration: d.errorDeclaration, encode: func(ctx context.Context, limits contract.JSONLimits) ([]byte, error) {
		return d.descriptor.Encode(ctx, payload, limits)
	}}
}

// DetailedError is a bound, non-generic application error. Its payload is held
// only by a private encoder; formatting exposes the public code alone.
// Example: failure := RegistryExists.New(details).WithCause(cause).
type DetailedError struct {
	declaration errorDeclaration
	encode      func(context.Context, contract.JSONLimits) ([]byte, error)
	cause       error
}

// Error returns only the public code, or internal_error for an invalid declaration.
// Example: code := failure.Error().
func (e *DetailedError) Error() string { return string(e.httpErrorCode()) }

// Format keeps every formatting verb free of payload and cause text.
// Example: fmt.Sprintf("%#v", failure).
func (e DetailedError) Format(state fmt.State, verb rune) { _, _ = io.WriteString(state, e.Error()) }

// WithCause returns a copy retaining the cause for errors.Is and errors.As.
// Example: failure := RegistryExists.New(details).WithCause(cause).
func (e *DetailedError) WithCause(cause error) *DetailedError {
	copy := *e
	copy.cause = cause
	return &copy
}

// Unwrap returns the private cause without formatting it.
// Example: cause := errors.Unwrap(failure).
func (e *DetailedError) Unwrap() error { return e.cause }

func (e *DetailedError) httpErrorCode() ErrorCode {
	if definition, valid := e.httpErrorDefinition(); valid {
		return definition.Code
	}
	return InternalError
}
func (e *DetailedError) httpErrorDefinition() (ErrorDefinition, bool) {
	if e == nil || e.encode == nil || e.declaration.details == nil {
		return ErrorDefinition{}, false
	}
	definition, err := e.declaration.Description()
	return definition, err == nil
}

// ErrorDetailsSchema returns an owned details schema for a registered error code.
// Plain or unknown codes have no details schema and return an error.
// Example: schema, err := router.ErrorDetailsSchema("registry_exists").
func (r *Router) ErrorDetailsSchema(code ErrorCode) (contract.Schema, error) {
	if r != nil {
		if details := r.errorDetails[code]; details != nil {
			return details.schema()
		}
	}
	return contract.Schema{}, safeerror.NewFault(fault.Missing, "HTTP error has no details schema")
}

func allowsErrorDetails(ctx context.Context, failure *DetailedError) bool {
	state, _ := ctx.Value(matchedRouteKey{}).(*matchedRoute)
	if state == nil || !state.typed {
		return false
	}
	details := state.errorDetails[failure.declaration.definition.Code]
	return details != nil && details.fingerprint == failure.declaration.details.fingerprint
}

type detailedErrorResponse struct {
	ErrorResponse
	Details json.RawMessage `json:"details"`
}

// Only WriteError prepares payloads. Classification probes never run encoders.
func prepareErrorResponse(ctx context.Context, payload ErrorResponse, detailed *DetailedError, presenter *errorPresenter) ([]byte, ErrorResponse, error) {
	var details []byte
	var preparationErr error
	var limits contract.JSONLimits
	encoding := context.WithoutCancel(ctx)
	if detailed != nil {
		state, _ := ctx.Value(matchedRouteKey{}).(*matchedRoute)
		limits = state.responseLimits
		preparationErr = callback.Isolated("HTTP error details encoding", func() error {
			var err error
			details, err = detailed.encode(encoding, limits)
			return err
		})
	}
	if preparationErr == nil && presenter != nil {
		presenter.present(ctx, &payload)
	}
	if detailed != nil && preparationErr == nil {
		body, err := value.EncodeJSON(encoding, detailedErrorResponse{ErrorResponse: payload, Details: details}, value.JSONEncodingLimits{
			Bytes: limits.Bytes - 1, Depth: limits.Depth, Nodes: limits.Nodes, Steps: limits.Steps,
		})
		if err == nil {
			return body, payload, nil
		}
		preparationErr = err
	}
	if preparationErr != nil {
		definition, _ := InternalError.definition()
		payload = ErrorResponse{Status: definition.Status, Code: definition.Code, Message: definition.Message, RequestID: payload.RequestID}
		if presenter != nil {
			presenter.present(ctx, &payload)
		}
		preparationErr = safeerror.WrapFailure(fault.Internal, "typed HTTP error details failed their contract or EndpointLimits.Response bounds", preparationErr)
	}
	return nil, payload, preparationErr
}
