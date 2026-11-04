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
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
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
	depth       int
	err         error
}

func (d ErrorDeclaration) errorDeclarationOption() errorDeclaration {
	return errorDeclaration{definition: d.definition}
}

func (d errorDeclaration) description() (ErrorDefinition, error) {
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
	return DetailedErrorDeclaration[D]{
		errorDeclaration: defineErrorDetails(code, status, message, descriptor.Description),
		descriptor:       descriptor,
	}
}

func defineErrorDetails(code ErrorCode, status int, message string, describe func() (contract.Schema, error)) errorDeclaration {
	details := &errorDetails{schema: describe}
	schema, err := describe()
	if err == nil {
		err = contract.RequireObjectRoot(schema)
	}
	if err == nil {
		envelope, envelopeErr := ErrorResponseJSON().Description()
		err = envelopeErr
		if err == nil && schema.Root == envelope.Root {
			err = safeerror.NewFault(fault.Invalid, "HTTP error details cannot use the error envelope")
		}
	}
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
	if err == nil {
		details.depth = requiredDetailsDepth(schema)
	}
	details.err = err
	return errorDeclaration{
		definition: ErrorDefinition{Code: code, Status: status, Message: message, DetailsType: schema.Root},
		details:    details,
	}
}

func (d errorDeclaration) errorDeclarationOption() errorDeclaration { return d }

// Description returns an owned value snapshot of the declaration.
// Example: definition, err := RegistryExists.Description().
func (d DetailedErrorDeclaration[D]) Description() (ErrorDefinition, error) {
	return d.description()
}

// Validate checks the public definition and the details output schema.
// Example: err := RegistryExists.Validate().
func (d DetailedErrorDeclaration[D]) Validate() error {
	_, err := d.description()
	return err
}

// MessageDefinition returns the parameter-free http.error.<code> catalog signature.
// Example: message, err := RegistryExists.MessageDefinition().
func (d DetailedErrorDeclaration[D]) MessageDefinition() (i18n.MessageDefinition, error) {
	definition, err := d.description()
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

// Matches reports whether err contains this exact definition and details schema.
// Inspection is bounded; invalid declarations and failed inspection never match.
// Example: if RegistryExists.Matches(err) { /* handle the declared failure */ }.
func (d DetailedErrorDeclaration[D]) Matches(err error) bool {
	definition, invalid := d.description()
	if invalid != nil || d.details == nil || err == nil {
		return false
	}
	matched := false
	failure := callback.Isolated("HTTP detailed error identification", func() error {
		complete := errorgraph.Walk(err, func(current error) bool {
			bound, found := errorgraph.AsShallow[interface {
				error
				detailedError() *DetailedError
			}](current)
			if found {
				candidate := bound.detailedError()
				if info, valid := candidate.httpErrorDefinition(); valid {
					matched = info == definition && candidate.declaration.details.fingerprint == d.details.fingerprint
				}
			}
			return !matched
		})
		if !complete {
			matched = false
		}
		return nil
	})
	return failure == nil && matched
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
	if e == nil {
		return nil
	}
	copy := *e
	copy.cause = cause
	return &copy
}

// Unwrap returns the private cause without formatting it.
// Example: cause := errors.Unwrap(failure).
func (e *DetailedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *DetailedError) detailedError() *DetailedError { return e }

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
	definition, err := e.declaration.description()
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
	if state == nil {
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
		// Input inspection can invoke omitzero IsZero before jsoncodec owns methods.
		preparationErr = callback.Isolated("HTTP error details encoding", func() error {
			var err error
			remaining := limits
			overhead, err := errorEnvelopeSteps(encoding, payload, limits)
			if err != nil {
				return err
			}
			remaining.Steps -= overhead
			details, err = detailed.encode(encoding, remaining)
			return err
		})
	}
	if preparationErr == nil && presenter != nil {
		presenter.present(ctx, &payload)
	}
	if detailed != nil && preparationErr == nil {
		body, err := encodeDetailedResponse(encoding, payload, details, limits)
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

func encodeDetailedResponse(ctx context.Context, payload ErrorResponse, details json.RawMessage, limits contract.JSONLimits) ([]byte, error) {
	return value.EncodeJSON(ctx, detailedErrorResponse{ErrorResponse: payload, Details: details}, value.JSONEncodingLimits{
		Bytes: limits.Bytes - 1, Depth: limits.Depth, Nodes: limits.Nodes, Steps: limits.Steps,
	})
}

func errorEnvelopeSteps(ctx context.Context, payload ErrorResponse, limits contract.JSONLimits) (int, error) {
	descriptor := ErrorResponseJSON()
	data, err := descriptor.Encode(ctx, payload, limits)
	if err != nil {
		return 0, err
	}
	steps, err := descriptor.ValidationSteps(ctx, data, limits)
	// The extension adds one object key and one declared property. The details
	// descriptor owns its root visit and all work below it.
	return steps + 2, err
}

func validateErrorDetailsLimits(declaration errorDeclaration, limits contract.JSONLimits) error {
	definition := declaration.definition
	payload := ErrorResponse{Status: definition.Status, Code: definition.Code, Message: definition.Message}
	floor := limits
	floor.Steps = DefaultEndpointLimits().Response.Steps
	_, err := encodeDetailedResponse(context.Background(), payload, json.RawMessage(`{}`), floor)
	if err != nil || limits.Depth < 1+declaration.details.depth {
		return safeerror.NewFault(fault.Invalid, "EndpointLimits.Response cannot fit the declared HTTP error details floor")
	}
	return nil
}

// Find the shallowest value admitted by required paths. Nullable fields, empty
// variable collections and optional properties need no deeper children. Start
// unresolved cycles above the supported bound; finite union branches can lower it.
func requiredDetailsDepth(schema contract.Schema) int {
	const unreachable = contract.MaxJSONDepth + 1
	types := make(map[contract.TypeID]contract.Type, len(schema.Types))
	depths := make(map[contract.TypeID]int, len(schema.Types))
	parents := make(map[contract.TypeID][]contract.TypeID)
	queue := make([]contract.TypeID, 0, len(schema.Types))
	for _, typ := range schema.Types {
		types[typ.ID], depths[typ.ID] = typ, unreachable
		queue = append(queue, typ.ID)
		for _, property := range typ.Properties {
			if property.Required {
				parents[property.Type] = append(parents[property.Type], typ.ID)
			}
		}
		for _, variant := range typ.Variants {
			parents[variant.Type] = append(parents[variant.Type], typ.ID)
		}
		if typ.Element != "" {
			parents[typ.Element] = append(parents[typ.Element], typ.ID)
		}
	}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		typ, depth := types[id], 0
		if !typ.Nullable {
			switch typ.Kind {
			case contract.ObjectKind:
				for _, property := range typ.Properties {
					if property.Required {
						depth = max(depth, 1+depths[property.Type])
					}
				}
			case contract.AliasKind:
				depth = depths[typ.Element]
			case contract.ArrayKind:
				if length, fixed := typ.Length.Get(); fixed && length > 0 {
					depth = 1 + depths[typ.Element]
				}
			case contract.UnionKind:
				depth = unreachable
				for _, variant := range typ.Variants {
					depth = min(depth, max(1, depths[variant.Type]))
				}
			}
		}
		if depth < depths[id] {
			depths[id] = depth
			queue = append(queue, parents[id]...)
		}
	}
	return depths[schema.Root]
}
