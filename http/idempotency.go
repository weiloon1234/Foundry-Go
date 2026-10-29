package http

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// TransactionHandler retains the concrete wire input/result and requires the
// runner-owned transaction. It cannot receive or write an HTTP response writer.
type TransactionHandler[P, Q, B, R any] func(context.Context, *database.Tx, Input[P, Q, B]) (R, error)

// TransactionPreparation runs on EVERY request, before claiming or replaying.
// Resolve current resource policy and trusted scope here. The returned closure
// may capture that request's concrete authorized models for the winning callback.
type TransactionPreparation[P, Q, B, R any] func(context.Context, Input[P, Q, B]) (idempotency.Scope, TransactionHandler[P, Q, B, R], error)

type IdempotentEndpoint[P, Q, B, R any] struct {
	endpoint   Endpoint[P, Q, B, R]
	store      *idempotency.Store
	definition idempotency.Definition
	headers    func(context.Context, R) ([]ResponseHeader, error)
	wrap       func(RouteRegistration) RouteRegistration
	// signedURL is the signed link policy when wrap signs the registration.
	signedURL *SignedURLInfo
	err       error
}

// Idempotent supports transaction-bound JSON/empty operations on unsafe methods.
// Configure the ordinary endpoint's rules, hooks and response before adapting it.
func Idempotent[P, Q, B, R any](endpoint Endpoint[P, Q, B, R], store *idempotency.Store, definition idempotency.Definition) IdempotentEndpoint[P, Q, B, R] {
	return IdempotentEndpoint[P, Q, B, R]{endpoint: endpoint, store: store, definition: definition}
}

func IdempotentSigned[P, Q, B, R any](endpoint SignedEndpoint[P, Q, B, R], store *idempotency.Store, definition idempotency.Definition) IdempotentEndpoint[P, Q, B, R] {
	bound := Idempotent(endpoint.endpoint, store, definition)
	bound.err = ValidateSigned(endpoint)
	bound.signedURL = signedURLInfo(endpoint.signer.policy)
	bound.wrap = func(r RouteRegistration) RouteRegistration {
		return signedRegistration(r, endpoint.signer, endpoint.endpoint.route.spec, EndpointPattern(endpoint.endpoint), true)
	}
	return bound
}
func IdempotentEndpointID[P, Q, B, R any](e IdempotentEndpoint[P, Q, B, R]) RouteID {
	return EndpointID(e.endpoint)
}

// IdempotentHeaders prepares bounded application headers inside the transaction.
// Only Location, Content-Language and ETag are replayable. Cookies, credentials,
// security, framing and request/trace headers never enter the stored outcome.
func IdempotentHeaders[P, Q, B, R any](e IdempotentEndpoint[P, Q, B, R], prepare func(context.Context, R) ([]ResponseHeader, error)) IdempotentEndpoint[P, Q, B, R] {
	if prepare == nil || e.headers != nil {
		e.err = safeerror.NewFault(fault.Invalid, "idempotent response headers need one callback")
	}
	e.headers = prepare
	return e
}
func ValidateIdempotent[P, Q, B, R any](e IdempotentEndpoint[P, Q, B, R]) error {
	if e.endpoint.headers != nil {
		// Replays must reproduce the stored outcome exactly.
		return safeerror.NewFault(fault.Invalid, "idempotent endpoints declare replayable headers with IdempotentHeaders")
	}
	for _, err := range []error{e.err, ValidateEndpoint(e.endpoint), e.store.Validate(), e.definition.Validate()} {
		if err != nil {
			return err
		}
	}
	if e.endpoint.body.kind != payloadJSON && e.endpoint.body.kind != payloadEmpty && e.endpoint.body.kind != payloadForm {
		return safeerror.NewFault(fault.Invalid, "idempotent endpoints require JSON, form or empty input")
	}
	if e.endpoint.response.credentials || e.endpoint.response.kind != payloadJSON && e.endpoint.response.kind != payloadEmpty {
		return safeerror.NewFault(fault.Invalid, "idempotent endpoints require a non-credential JSON or empty response")
	}
	switch EndpointMethod(e.endpoint) {
	case POST, PUT, PATCH, DELETE:
	default:
		return safeerror.NewFault(fault.Invalid, "idempotency requires an unsafe operation method")
	}
	if err := validateIdempotentMiddlewares(e.endpoint.route.middlewares); err != nil {
		return err
	}
	if e.endpoint.response.kind == payloadJSON && e.endpoint.response.decodeJSON == nil {
		return safeerror.NewFault(fault.Invalid, "idempotent response codec cannot restore its concrete result")
	}

	return nil
}
func idempotentEndpointDescribed[P, Q, B, R any](e IdempotentEndpoint[P, Q, B, R]) Endpoint[P, Q, B, R] {
	endpoint := e.endpoint
	info := idempotencyInfo(e.definition, e.store.Config(), e.headers != nil)
	endpoint.idempotency = &info
	endpoint.limits.Body.Bytes = min(endpoint.limits.Body.Bytes, e.store.Config().MaxInputBytes)
	if endpoint.body.kind == payloadForm {
		endpoint.limits.Form.Bytes = min(endpoint.limits.Form.Bytes, e.store.Config().MaxInputBytes)
	}
	return ConfigureEndpoint(endpoint, Errors(idempotencyErrors()...))
}
func InspectIdempotent[P, Q, B, R any](e IdempotentEndpoint[P, Q, B, R]) (EndpointInfo, error) {
	if err := ValidateIdempotent(e); err != nil {
		return EndpointInfo{}, err
	}
	info, err := InspectEndpoint(idempotentEndpointDescribed(e))
	if e.signedURL != nil {
		info.Route.SignedURL = e.signedURL
	}
	return info, err
}
func idempotentEndpointContributionContract[P, Q, B, R any](e IdempotentEndpoint[P, Q, B, R]) contributionContract {
	result := endpointContributionContract(e.endpoint)
	result.idempotency = e.definition
	result.signed = e.wrap != nil
	return result
}

// Handle derives trusted scope after the endpoint's validation and authorization.
// Scope callbacks must authenticate/verify public webhook requests on every call.
func (e IdempotentEndpoint[P, Q, B, R]) Handle(scope func(context.Context, Input[P, Q, B]) (idempotency.Scope, error), handler TransactionHandler[P, Q, B, R]) RouteRegistration {
	if scope == nil || handler == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "idempotent endpoint requires scope and transaction callbacks"))
	}
	return PrepareIdempotent(e, func(ctx context.Context, in Input[P, Q, B]) (idempotency.Scope, TransactionHandler[P, Q, B, R], error) {
		identity, err := scope(ctx, in)
		return identity, handler, err
	})
}

// PrepareIdempotent is the typed composition boundary for authenticated/model-bound
// adapters. Policy and binding run before Run on both new requests and replays.
func PrepareIdempotent[P, Q, B, R any](e IdempotentEndpoint[P, Q, B, R], prepare TransactionPreparation[P, Q, B, R]) RouteRegistration {
	if err := ValidateIdempotent(e); err != nil {
		return InvalidRouteRegistration(err)
	}
	if prepare == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "idempotent endpoint requires request preparation"))
	}
	operation, err := idempotency.Define(e.store, e.definition, idempotentEndpointInputEncoding(e), idempotentEndpointOutputEncoding(e))
	if err != nil {
		return InvalidRouteRegistration(err)
	}
	endpoint := idempotentEndpointDescribed(e)
	registration := endpointHandlePrepared(endpoint, func(ctx context.Context, in Input[P, Q, B]) (preparedResponse, error) {
		identity, handler, err := prepare(ctx, in)
		if err != nil {
			return preparedResponse{}, err
		}
		if handler == nil {
			return preparedResponse{}, InternalError.WithCause(nil)
		}
		key, ok := ctx.Value(idempotencyKeyContext{}).(idempotency.Key)
		if !ok {
			return preparedResponse{}, IdempotencyBadKey
		}
		result, err := operation.Run(ctx, identity, key, in, idempotency.Handler[Input[P, Q, B], R](handler))
		if !result.Committed() {
			return preparedResponse{}, idempotencyHTTPError(err, e.store.Config().DuplicateWait)
		}
		stored, decodeErr := decodeStoredResponse(result.Encoded(), e.endpoint.response.status, e.headers != nil)
		if decodeErr != nil {
			return preparedResponse{}, IdempotencyUnavailable.WithCause(decodeErr)
		}
		return preparedResponse{data: stored.Body, headers: stored.Headers, operational: err}, nil
	})
	registration.contract = idempotentEndpointContributionContract(e)
	if e.wrap != nil {
		registration = e.wrap(registration)
		registration.handler = idempotentHTTPHandler{registration.handler}
	}
	return registration
}

var (
	IdempotencyBadKey      = DefineError("idempotency_bad_key", 400, "A valid idempotency key is required.")
	IdempotencyMismatch    = DefineError("idempotency_mismatch", 409, "This key belongs to a different submission.")
	IdempotencyInProgress  = DefineError("idempotency_in_progress", 409, "This submission is still in progress. Retry with the same key.")
	IdempotencyCapacity    = DefineError("idempotency_capacity", 429, "Idempotency admission capacity is unavailable.")
	IdempotencyUnavailable = DefineError("idempotency_unavailable", 503, "The operation outcome is unavailable. Retry with the same key.")
)

func idempotencyErrors() []ErrorDeclaration {
	return []ErrorDeclaration{IdempotencyBadKey, IdempotencyMismatch, IdempotencyInProgress, IdempotencyCapacity, IdempotencyUnavailable}
}

type idempotencyRetryError struct {
	ErrorDeclaration
	cause error
	retry time.Duration
}

func (e *idempotencyRetryError) Unwrap() error { return e.cause }
func idempotencyHTTPError(err error, wait time.Duration) error {
	switch {
	case err == nil:
		return InternalError.WithCause(nil)
	case errorgraph.Is(err, idempotency.BadKey):
		return IdempotencyBadKey.WithCause(err)
	case errorgraph.Is(err, idempotency.Mismatch):
		return IdempotencyMismatch.WithCause(err)
	case errorgraph.Is(err, idempotency.InProgress):
		return &idempotencyRetryError{IdempotencyInProgress, err, wait}
	case errorgraph.Is(err, idempotency.Capacity):
		return &idempotencyRetryError{IdempotencyCapacity, err, wait}
	case errorgraph.Is(err, idempotency.Unavailable):
		return &idempotencyRetryError{IdempotencyUnavailable, err, wait}
	default:
		return err
	}
}
