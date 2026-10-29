package http

import (
	"context"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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

// Binding is a typed stage that runs after request authorization and before
// validation. It resolves request state, such as a route model, and returns the
// handler that completes the request, closing over that state so the handler
// never repeats the work. Model binding (http/modelbinding) uses it, so a
// missing bound resource is a 404, or a resource policy denial a 403, before
// body validation can report 422. A returned error is published as returned.
type Binding[P, Q, B, R any] func(context.Context, Input[P, Q, B]) (Handler[P, Q, B, R], error)

// NoQuery is the concrete type of an endpoint that accepts no query parameters.
type NoQuery struct{}

// EmptyQuery rejects any undeclared query key through the ordinary query codec.
func EmptyQuery() Query[NoQuery] { return DefineQuery[NoQuery]() }

type endpointCore struct {
	limits      EndpointLimits
	errors      []ErrorDeclaration
	idempotency *IdempotencyInfo
	examples    endpointExamples
}

// Endpoint composes the existing route, query and payload descriptors. Assembly
// performs no I/O. Handle validates the complete declaration before registration.
type Endpoint[P, Q, B, R any] struct {
	endpointCore
	route         Route[P]
	query         Query[Q]
	body          Body[B]
	response      Response[R]
	validation    *validation.Rule[Input[P, Q, B]]
	preparation   *Preparation[P, Q, B]
	authorization *RequestAuthorization[P, Q, B]
	headers       *ResponseHeaders[R]
}

func DefineEndpoint[P, Q, B, R any](route Route[P], query Query[Q], body Body[B], response Response[R], options ...EndpointOption) Endpoint[P, Q, B, R] {
	return ConfigureEndpoint(Endpoint[P, Q, B, R]{route: route, query: query, body: body, response: response, endpointCore: endpointCore{limits: DefaultEndpointLimits()}}, options...)
}

func EndpointID[P, Q, B, R any](e Endpoint[P, Q, B, R]) RouteID     { return RouteIDOf(e.route) }
func EndpointMethod[P, Q, B, R any](e Endpoint[P, Q, B, R]) Method  { return RouteMethod(e.route) }
func EndpointPattern[P, Q, B, R any](e Endpoint[P, Q, B, R]) string { return RoutePattern(e.route) }

func ValidateEndpoint[P, Q, B, R any](e Endpoint[P, Q, B, R]) error {
	if e.body.kind == payloadForm {
		if err := e.limits.Form.Validate(); err != nil {
			return err
		}
	}
	if e.body.kind == payloadRaw {
		if err := e.limits.Raw.Validate(); err != nil {
			return err
		}
	}
	if e.preparation != nil && *e.preparation == nil || e.authorization != nil && *e.authorization == nil {
		return safeerror.NewFault(fault.Invalid, "request lifecycle callback is missing")
	}
	if e.examples.err != nil {
		return e.examples.err
	}
	for _, err := range []error{ValidateRoute(e.route), ValidateQueryDescriptor(e.query), ValidateBodyDescriptor(e.body), ValidateResponse(e.response), e.limits.Validate()} {
		if err != nil {
			return err
		}
	}
	if err := queryUrlPresentation(e.query); err != nil {
		return err
	}
	if e.validation != nil {
		if err := validation.ValidateRule(*e.validation); err != nil {
			return err
		}
	}
	if _, err := endpointErrorDefinitions(e); err != nil {
		return err
	}
	if err := endpointValidateHeaders(e); err != nil {
		return err
	}
	if e.response.credentials && RouteMethod(e.route) != POST {
		return safeerror.NewFault(fault.Invalid, "credential delivery requires a POST endpoint")
	}
	if err := endpointValidateRefreshCookie(e); err != nil {
		return err
	}
	if RouteMethod(e.route) == TRACE {
		return safeerror.NewFault(fault.Invalid, "TRACE requires a raw HTTP handler")
	}
	if (RouteMethod(e.route) == GET || RouteMethod(e.route) == HEAD) && e.body.kind != payloadEmpty {
		return safeerror.NewFault(fault.Invalid, "typed GET and HEAD endpoints require an empty request body")
	}
	return nil
}

// URL generates a relative path and query from their concrete declarations.
// Neither an incoming host nor an arbitrary query string participates. Only
// the route and query declarations it uses are validated here, so link
// generation stays cheap; Handle validates the complete endpoint.
func (e Endpoint[P, Q, B, R]) URL(ctx context.Context, path P, query Q) (string, error) {
	if err := ValidateQueryDescriptor(e.query); err != nil {
		return "", err
	}
	if err := e.limits.Query.Validate(); err != nil {
		return "", err
	}
	location, err := RouteURL(e.route, path)
	if err != nil {
		return "", err
	}
	parameters, err := EncodeQuery(e.query, ctx, query, e.limits.Query)
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
//
// A handler's outcome is authoritative. A returned error, including a declared
// application error, is published as returned. A successful result is prepared
// and written even if the request deadline expired after the handler returned;
// only an actual write failure can then prevent delivery.
func (e Endpoint[P, Q, B, R]) Handle(handler Handler[P, Q, B, R]) RouteRegistration {
	if handler == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "endpoint requires a handler"))
	}
	return endpointHandlePrepared(e, endpointComplete(e, handler))
}

// HandleBound runs bind after request authorization and before validation, then
// the handler it returned. The lifecycle is otherwise that of Handle: decode,
// prohibited-input check, preparation, request authorization, binding,
// validation, handler.
func HandleBound[P, Q, B, R any](e Endpoint[P, Q, B, R], bind Binding[P, Q, B, R]) RouteRegistration {
	if bind == nil {
		return InvalidRouteRegistration(safeerror.NewFault(fault.Invalid, "endpoint requires a binding"))
	}
	return endpointHandleStaged(e, func(ctx context.Context, input Input[P, Q, B]) (func(context.Context, Input[P, Q, B]) (preparedResponse, error), error) {
		handler, err := bind(ctx, input)
		if err != nil {
			return nil, err
		}
		if handler == nil {
			return nil, InternalError.WithCause(safeerror.NewFault(fault.Internal, "endpoint binding returned no handler"))
		}
		return endpointComplete(e, handler), nil
	})
}

// complete runs a typed handler and prepares its response.
func endpointComplete[P, Q, B, R any](e Endpoint[P, Q, B, R], handler Handler[P, Q, B, R]) func(context.Context, Input[P, Q, B]) (preparedResponse, error) {
	return func(ctx context.Context, input Input[P, Q, B]) (preparedResponse, error) {
		result, err := handler(ctx, input)
		if err != nil {
			return preparedResponse{}, err
		}
		// Headers are derived before encoding, so a failure opens no source.
		headers, err := endpointResponseHeaders(e, ctx, result)
		if err != nil {
			return preparedResponse{}, err
		}
		prepared, err := responsePrepare(e.response, ctx, result, e.limits)
		prepared.headers = headers
		return prepared, err
	}
}

// handlePrepared shares all decoding, lifecycle, callback ownership and response
// publication. Transaction adapters prepare their result before outer commit.
func endpointHandlePrepared[P, Q, B, R any](e Endpoint[P, Q, B, R], handler func(context.Context, Input[P, Q, B]) (preparedResponse, error)) RouteRegistration {
	return endpointRegister(e, nil, handler)
}

// handleStaged selects the completing handler at the binding stage.
func endpointHandleStaged[P, Q, B, R any](e Endpoint[P, Q, B, R], stage preparedStage[P, Q, B]) RouteRegistration {
	return endpointRegister(e, stage, nil)
}

type preparedStage[P, Q, B any] func(context.Context, Input[P, Q, B]) (func(context.Context, Input[P, Q, B]) (preparedResponse, error), error)

func endpointRegister[P, Q, B, R any](e Endpoint[P, Q, B, R], stage preparedStage[P, Q, B], handler func(context.Context, Input[P, Q, B]) (preparedResponse, error)) RouteRegistration {
	if err := ValidateEndpoint(e); err != nil {
		return InvalidRouteRegistration(err)
	}
	if err := responseOutputPresentation(e.response); err != nil {
		return InvalidRouteRegistration(err)
	}
	transport := endpointTransport{
		responseKind:        e.response.kind,
		responseStatus:      e.response.status,
		credentials:         e.response.credentials,
		clearsRefreshCookie: e.response.refreshCookieClears,
		readsRefreshCookie:  e.body.kind == payloadRefreshCookie,
		cookie:              endpointRefreshCookie(e),
		files:               e.limits.Files,
		browserResponse:     e.idempotency == nil && (e.response.kind == payloadJSON || e.response.kind == payloadEmpty || e.response.kind == payloadRedirect),
	}
	registration := routeHandle(e.route, func(w stdhttp.ResponseWriter, r *stdhttp.Request, path P) {
		endpointServe(e, w, r, path, stage, handler, transport)
	}, false)
	registration.contract = endpointContributionContract(e)
	info := registration.info.clone()
	registration.endpoint = func() EndpointInfo { return endpointSnapshot(e, info) }
	registration.errors, _ = endpointErrorDefinitions(e)
	if e.idempotency != nil {
		registration.handler = idempotentHTTPHandler{registration.handler}
	}
	return registration
}

func endpointServe[P, Q, B, R any](e Endpoint[P, Q, B, R], w stdhttp.ResponseWriter, r *stdhttp.Request, path P, stage preparedStage[P, Q, B], handler func(context.Context, Input[P, Q, B]) (preparedResponse, error), transport endpointTransport) {
	var ready bool
	w, r, ready = transport.preflight(w, r)
	if !ready {
		return
	}
	ctx := r.Context()
	rawQuery := r.URL.RawQuery
	if verified, ok := ctx.Value(signedEndpointQueryKey{}).(string); ok {
		rawQuery = verified
	}
	query, err := DecodeQuery(e.query, ctx, queryWithoutConsumed(e.query, ctx, rawQuery), e.limits.Query)
	if err != nil {
		writeRoutingError(w, r, endpointInputError(ctx, "query", err))
		return
	}
	body, cleanup, err := bodyReadOwned(e.body, w, r, e.limits)
	defer cleanupEndpointResource(r, "body", cleanup)
	if err != nil {
		writeRoutingError(w, r, err)
		return
	}
	// A deadline while the request was still being read or decoded means the
	// client was too slow: 408. Later deadlines are the server's own budget.
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
			if err := validation.CheckProhibitions(*e.validation, ctx, input, e.limits.Validation); err != nil {
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
	// Authorization precedes validation, as in a Laravel FormRequest: a denied
	// caller never reaches rules that query the database (Unique, Exists).
	if e.authorization != nil {
		if err := requestHook(ctx, "HTTP request authorization", func() error { return (*e.authorization)(ctx, input) }); err != nil {
			writeRoutingError(w, r, err)
			return
		}
	}
	// The binding stage resolves request state (for example a route model)
	// before validation, so a missing resource is 404 rather than 422.
	if stage != nil {
		if err := requestHook(ctx, "HTTP request binding", func() error {
			var returned error
			handler, returned = stage(ctx, input)
			return returned
		}); err != nil {
			writeRoutingError(w, r, err)
			return
		}
	}
	if e.validation != nil {
		if err := validation.Check(*e.validation, ctx, input, e.limits.Validation); err != nil {
			writeRoutingError(w, r, endpointValidationError(ctx, err))
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
	// Isolation rule: the application handler and application-supplied hooks
	// run through callback.Isolated, which also contains runtime.Goexit.
	// Framework hot paths (URL codecs, body reads, file chunks and URL generation)
	// use callback.Invoke: same goroutine, panics
	// contained with their frames, no per-call goroutine.
	callbackErr := callback.Isolated("HTTP endpoint handler", func() error {
		data, returned = handler(ctx, input)
		return nil
	})
	defer cleanupEndpointResource(r, "response", data.cleanup())
	transport.publish(w, r, data, returned, callbackErr)
}

func endpointInputError(ctx context.Context, source string, err error) error {
	if canceled := ctx.Err(); canceled != nil && err == canceled {
		return RequestTimeout.WithCause(err)
	}
	var issues []contract.Issue
	switch failure := safeerror.UnwrapOwned(err).(type) {
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
	return responseFailure(BadRequest, err, issues)
}
