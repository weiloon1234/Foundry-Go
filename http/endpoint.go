package http

import (
	"context"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// Input retains distinct, concrete parameter sources. A body field cannot
// replace a path or query field with the same name. Alias this instantiated type
// to give a domain handler a short request name.
type Input[P, Q, B any] struct {
	Path  P
	Query Q
	Body  B
}

// Handler receives decoded input and returns a declared response value. It owns
// domain operations and their transaction boundaries; it never receives a writer.
type Handler[P, Q, B, R any] func(context.Context, Input[P, Q, B]) (R, error)

// NoQuery is the concrete type of an endpoint that accepts no query parameters.
type NoQuery struct{}

// EmptyQuery rejects any undeclared query key through the ordinary query codec.
func EmptyQuery() Query[NoQuery] { return DefineQuery[NoQuery]() }

// Endpoint composes the existing route, query and payload descriptors. Assembly
// performs no I/O. Handle validates the complete declaration before registration.
type Endpoint[P, Q, B, R any] struct {
	route         Route[P]
	query         Query[Q]
	body          Body[B]
	response      Response[R]
	limits        EndpointLimits
	validation    *validation.Rule[Input[P, Q, B]]
	errors        []ErrorDeclaration
	preparation   *Preparation[P, Q, B]
	authorization *RequestAuthorization[P, Q, B]
	idempotency   *IdempotencyInfo
}

func DefineEndpoint[P, Q, B, R any](route Route[P], query Query[Q], body Body[B], response Response[R]) Endpoint[P, Q, B, R] {
	return Endpoint[P, Q, B, R]{route: route, query: query, body: body, response: response, limits: DefaultEndpointLimits()}
}

func (e Endpoint[P, Q, B, R]) ID() RouteID     { return e.route.ID() }
func (e Endpoint[P, Q, B, R]) Method() Method  { return e.route.Method() }
func (e Endpoint[P, Q, B, R]) Pattern() string { return e.route.Pattern() }

// WithLimits returns an independent declaration with explicit resource limits.
func (e Endpoint[P, Q, B, R]) WithLimits(limits EndpointLimits) Endpoint[P, Q, B, R] {
	e.limits = limits
	return e
}

// Within reuses route scope naming and path-prefix rules.
func (e Endpoint[P, Q, B, R]) Within(scope Scope) Endpoint[P, Q, B, R] {
	e.route = e.route.Within(scope)
	return e
}

func (e Endpoint[P, Q, B, R]) Validate() error {
	if e.body.kind == payloadForm {
		if err := e.limits.Form.Validate(); err != nil {
			return err
		}
	}
	if e.preparation != nil && *e.preparation == nil || e.authorization != nil && *e.authorization == nil {
		return fault.New(fault.Invalid, "request lifecycle callback is missing")
	}
	for _, err := range []error{e.route.Validate(), e.query.Validate(), e.body.Validate(), e.response.Validate(), e.limits.Validate()} {
		if err != nil {
			return err
		}
	}
	if e.validation != nil {
		if err := e.validation.Validate(); err != nil {
			return err
		}
	}
	if _, err := e.errorDefinitions(); err != nil {
		return err
	}
	if e.response.credentials && e.route.Method() != POST {
		return fault.New(fault.Invalid, "credential delivery requires a POST endpoint")
	}
	if e.route.Method() == TRACE {
		return fault.New(fault.Invalid, "TRACE requires a raw HTTP handler")
	}
	if (e.route.Method() == GET || e.route.Method() == HEAD) && e.body.kind != payloadEmpty {
		return fault.New(fault.Invalid, "typed GET and HEAD endpoints require an empty request body")
	}
	return nil
}

// URL generates a relative path and query from their concrete declarations.
// Neither an incoming host nor an arbitrary query string participates.
func (e Endpoint[P, Q, B, R]) URL(ctx context.Context, path P, query Q) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	location, err := e.route.URL(path)
	if err != nil {
		return "", err
	}
	parameters, err := e.query.Encode(ctx, query, e.limits.Query)
	if err != nil {
		return "", err
	}
	if parameters != "" {
		location += "?" + parameters
	}
	return location, nil
}

// Handle binds a handler with exactly these path/query/body/response types.
// Parsing, codec execution and handler callbacks finish before a response is
// committed. Cancellation never abandons an active callback or its resources.
func (e Endpoint[P, Q, B, R]) Handle(handler Handler[P, Q, B, R]) RouteRegistration {
	if handler == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "endpoint requires a handler"))
	}
	return e.handlePrepared(func(ctx context.Context, input Input[P, Q, B]) (preparedResponse, error) {
		result, err := handler(ctx, input)
		if canceled := ctx.Err(); canceled != nil {
			return preparedResponse{}, RequestTimeout.WithCause(canceled)
		}
		if err != nil {
			return preparedResponse{}, err
		}
		return e.response.prepare(ctx, result, e.limits)
	})
}

// handlePrepared shares all decoding, lifecycle, callback ownership and response
// publication. Transaction adapters prepare their result before outer commit.
func (e Endpoint[P, Q, B, R]) handlePrepared(handler func(context.Context, Input[P, Q, B]) (preparedResponse, error)) RouteRegistration {
	if err := e.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	registration := e.route.handle(func(w stdhttp.ResponseWriter, r *stdhttp.Request, path P) { e.serve(w, r, path, handler) }, false)
	registration.contract = e.contributionContract()
	info := registration.info.clone()
	registration.endpoint = func() EndpointInfo { return e.snapshot(info) }
	registration.errors, _ = e.errorDefinitions()
	if e.idempotency != nil {
		registration.handler = idempotentHTTPHandler{registration.handler}
	}
	return registration
}

func (e Endpoint[P, Q, B, R]) serve(w stdhttp.ResponseWriter, r *stdhttp.Request, path P, handler func(context.Context, Input[P, Q, B]) (preparedResponse, error)) {
	ctx := r.Context()
	if e.response.credentials && !checkCredentialRequest(w, r) {
		return
	}
	if err := bindBrowserResponse(ctx, e.idempotency == nil && (e.response.kind == payloadJSON || e.response.kind == payloadEmpty)); err != nil {
		writeRoutingError(w, r, err)
		return
	}
	if e.response.kind == payloadDownload {
		if err := e.limits.Files.checkRange(r.Header.Values("Range")); err != nil {
			writeRoutingError(w, r, err)
			return
		}
	}
	rawQuery := r.URL.RawQuery
	if verified, ok := ctx.Value(signedEndpointQueryKey{}).(string); ok {
		rawQuery = verified
	}
	query, err := e.query.Decode(ctx, rawQuery, e.limits.Query)
	if err != nil {
		writeRoutingError(w, r, endpointInputError(ctx, "query", err))
		return
	}
	body, cleanup, err := e.body.readOwned(w, r, e.limits)
	defer cleanupEndpointResource(r, "body", cleanup)
	if err != nil {
		writeRoutingError(w, r, err)
		return
	}
	if err := ctx.Err(); err != nil {
		writeRoutingError(w, r, RequestTimeout.WithCause(err))
		return
	}
	var data preparedResponse
	var returned error
	input := Input[P, Q, B]{Path: path, Query: query, Body: body}
	if e.preparation != nil {
		// Prohibitions on decoded input cannot be erased by normalization.
		if e.validation != nil {
			if err := e.validation.CheckProhibitions(ctx, input, e.limits.Validation); err != nil {
				writeRoutingError(w, r, endpointValidationError(ctx, err))
				return
			}
		}
		var preparedQuery Q
		var preparedBody B
		if err := requestHook(ctx, "HTTP request preparation", func() error {
			var returned error
			preparedQuery, preparedBody, returned = (*e.preparation)(ctx, input)
			return returned
		}); err != nil {
			writeRoutingError(w, r, err)
			return
		}
		input.Query, input.Body = preparedQuery, preparedBody
	}
	if e.validation != nil {
		if err := e.validation.Check(ctx, input, e.limits.Validation); err != nil {
			writeRoutingError(w, r, endpointValidationError(ctx, err))
			return
		}
	}
	if e.authorization != nil {
		if err := requestHook(ctx, "HTTP request authorization", func() error { return (*e.authorization)(ctx, input) }); err != nil {
			writeRoutingError(w, r, err)
			return
		}
	}
	if e.idempotency != nil {
		key, err := requestIdempotencyKey(r, e.idempotency.MaxKeyBytes)
		if err != nil {
			writeRoutingError(w, r, err)
			return
		}
		ctx = context.WithValue(ctx, idempotencyKeyContext{}, key)
		r = r.WithContext(ctx)
	}
	callbackErr := callback.Isolated("HTTP endpoint handler", func() error {
		data, returned = handler(ctx, input)
		return nil
	})
	defer cleanupEndpointResource(r, "response", data.cleanup())
	if data.operational != nil {
		logRouteFailure(r, "HTTP idempotent outcome committed with operational failure", data.operational)
	}
	if callbackErr != nil {
		logRouteFailure(r, "HTTP endpoint handler failed", callbackErr)
		writeRoutingError(w, r, InternalError.WithCause(callbackErr))
		return
	}
	// Preparation owns classification of codec/file-source failures. In particular,
	// cancellation must not mask a panic already contained by that boundary.
	if returned != nil {
		writeRoutingError(w, r, returned)
		return
	}
	if err := ctx.Err(); err != nil {
		writeRoutingError(w, r, RequestTimeout.WithCause(err))
		return
	}
	if err := publishBrowserSession(ctx, w, e.response.status); err != nil {
		writeRoutingError(w, r, authenticationError(err))
		return
	}
	if err := e.response.write(w, r, data); err != nil {
		logRouteFailure(r, "HTTP endpoint response failed", err)
		panic(stdhttp.ErrAbortHandler)
	}
}

func endpointInputError(ctx context.Context, source string, err error) error {
	if canceled := ctx.Err(); canceled != nil && err == canceled {
		return RequestTimeout.WithCause(err)
	}
	var issues []contract.Issue
	switch failure := err.(type) {
	case *QueryError:
		issues = failure.Issues()
	case *MultipartError:
		issues = failure.Issues()
	case *contract.DecodeError:
		issues = failure.Issues()
	default:
		return InternalError.WithCause(err)
	}
	for i := range issues {
		issues[i].Path = "/" + source + issues[i].Path
	}
	return &responseError{code: BadRequest, cause: err, issues: issues}
}
