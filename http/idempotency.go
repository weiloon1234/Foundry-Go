package http

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
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
func (e Endpoint[P, Q, B, R]) Idempotent(store *idempotency.Store, definition idempotency.Definition) IdempotentEndpoint[P, Q, B, R] {
	return IdempotentEndpoint[P, Q, B, R]{endpoint: e, store: store, definition: definition}
}
func (e SignedEndpoint[P, Q, B, R]) Idempotent(store *idempotency.Store, definition idempotency.Definition) IdempotentEndpoint[P, Q, B, R] {
	bound := e.endpoint.Idempotent(store, definition)
	bound.err = e.Validate()
	bound.signedURL = signedURLInfo(e.signer.policy)
	bound.wrap = func(r RouteRegistration) RouteRegistration {
		return signedRegistration(r, e.signer, e.endpoint.route.spec, e.endpoint.Pattern(), true)
	}
	return bound
}
func (e IdempotentEndpoint[P, Q, B, R]) ID() RouteID { return e.endpoint.ID() }

// WithHeaders prepares bounded application headers inside the transaction.
// Only Location, Content-Language and ETag are replayable. Cookies, credentials,
// security, framing and request/trace headers never enter the stored outcome.
func (e IdempotentEndpoint[P, Q, B, R]) WithHeaders(prepare func(context.Context, R) ([]ResponseHeader, error)) IdempotentEndpoint[P, Q, B, R] {
	if prepare == nil || e.headers != nil {
		e.err = fault.New(fault.Invalid, "idempotent response headers need one callback")
	}
	e.headers = prepare
	return e
}
func (e IdempotentEndpoint[P, Q, B, R]) Validate() error {
	if e.endpoint.headers != nil {
		// Replays must reproduce the stored outcome exactly.
		return fault.New(fault.Invalid, "idempotent endpoints declare replayable headers with IdempotentEndpoint.WithHeaders")
	}
	for _, err := range []error{e.err, e.endpoint.Validate(), e.store.Validate(), e.definition.Validate()} {
		if err != nil {
			return err
		}
	}
	if e.endpoint.body.kind != payloadJSON && e.endpoint.body.kind != payloadEmpty && e.endpoint.body.kind != payloadForm {
		return fault.New(fault.Invalid, "idempotent endpoints require JSON, form or empty input")
	}
	if e.endpoint.response.credentials || e.endpoint.response.kind != payloadJSON && e.endpoint.response.kind != payloadEmpty {
		return fault.New(fault.Invalid, "idempotent endpoints require a non-credential JSON or empty response")
	}
	switch e.endpoint.Method() {
	case POST, PUT, PATCH, DELETE:
	default:
		return fault.New(fault.Invalid, "idempotency requires an unsafe operation method")
	}
	if err := validateIdempotentMiddlewares(e.endpoint.route.middlewares); err != nil {
		return err
	}
	if e.endpoint.response.kind == payloadJSON {
		if _, ok := e.endpoint.response.json.(replayJSON[R]); !ok {
			return fault.New(fault.Invalid, "idempotent response codec cannot restore its concrete result")
		}
	}
	return nil
}
func (e IdempotentEndpoint[P, Q, B, R]) described() Endpoint[P, Q, B, R] {
	endpoint := e.endpoint
	info := idempotencyInfo(e.definition, e.store.Config(), e.headers != nil)
	endpoint.idempotency = &info
	endpoint.limits.Body.Bytes = min(endpoint.limits.Body.Bytes, e.store.Config().MaxInputBytes)
	if endpoint.body.kind == payloadForm {
		endpoint.limits.Form.Bytes = min(endpoint.limits.Form.Bytes, e.store.Config().MaxInputBytes)
	}
	return endpoint.WithErrors(idempotencyErrors()...)
}
func (e IdempotentEndpoint[P, Q, B, R]) Description() (EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return EndpointInfo{}, err
	}
	info, err := e.described().Description()
	if e.signedURL != nil {
		info.Route.SignedURL = e.signedURL
	}
	return info, err
}
func (e IdempotentEndpoint[P, Q, B, R]) contributionContract() contributionContract {
	result := e.endpoint.contributionContract()
	result.idempotency = e.definition
	result.signed = e.wrap != nil
	return result
}

// Handle derives trusted scope after the endpoint's validation and authorization.
// Scope callbacks must authenticate/verify public webhook requests on every call.
func (e IdempotentEndpoint[P, Q, B, R]) Handle(scope func(context.Context, Input[P, Q, B]) (idempotency.Scope, error), handler TransactionHandler[P, Q, B, R]) RouteRegistration {
	if scope == nil || handler == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "idempotent endpoint requires scope and transaction callbacks"))
	}
	return e.Prepare(func(ctx context.Context, in Input[P, Q, B]) (idempotency.Scope, TransactionHandler[P, Q, B, R], error) {
		identity, err := scope(ctx, in)
		return identity, handler, err
	})
}

// Prepare is the typed composition boundary for authenticated/model-bound
// adapters. Policy and binding run before Run on both new requests and replays.
func (e IdempotentEndpoint[P, Q, B, R]) Prepare(prepare TransactionPreparation[P, Q, B, R]) RouteRegistration {
	if err := e.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	if prepare == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "idempotent endpoint requires request preparation"))
	}
	operation, err := idempotency.Define(e.store, e.definition, e.inputEncoding(), e.outputEncoding())
	if err != nil {
		return InvalidRouteRegistration(err)
	}
	endpoint := e.described()
	registration := endpoint.handlePrepared(func(ctx context.Context, in Input[P, Q, B]) (preparedResponse, error) {
		identity, handler, err := prepare(ctx, in)
		if err != nil {
			return preparedResponse{}, err
		}
		if handler == nil {
			return preparedResponse{}, InternalError
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
	registration.contract = e.contributionContract()
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
		return InternalError
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
