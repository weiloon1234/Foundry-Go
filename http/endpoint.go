package http

import (
	"context"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
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
	headers       *ResponseHeaders[R]
	idempotency   *IdempotencyInfo
	examples      endpointExamples
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
	if e.body.kind == payloadRaw {
		if err := e.limits.Raw.Validate(); err != nil {
			return err
		}
	}
	if e.preparation != nil && *e.preparation == nil || e.authorization != nil && *e.authorization == nil {
		return fault.New(fault.Invalid, "request lifecycle callback is missing")
	}
	if e.examples.err != nil {
		return e.examples.err
	}
	for _, err := range []error{e.route.Validate(), e.query.Validate(), e.body.Validate(), e.response.Validate(), e.limits.Validate()} {
		if err != nil {
			return err
		}
	}
	if err := e.query.urlPresentation(); err != nil {
		return err
	}
	if e.validation != nil {
		if err := e.validation.Validate(); err != nil {
			return err
		}
	}
	if _, err := e.errorDefinitions(); err != nil {
		return err
	}
	if err := e.validateHeaders(); err != nil {
		return err
	}
	if e.response.credentials && e.route.Method() != POST {
		return fault.New(fault.Invalid, "credential delivery requires a POST endpoint")
	}
	if err := e.validateRefreshCookie(); err != nil {
		return err
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
// Neither an incoming host nor an arbitrary query string participates. Only
// the route and query declarations it uses are validated here, so link
// generation stays cheap; Handle validates the complete endpoint.
func (e Endpoint[P, Q, B, R]) URL(ctx context.Context, path P, query Q) (string, error) {
	if err := e.query.Validate(); err != nil {
		return "", err
	}
	if err := e.limits.Query.Validate(); err != nil {
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
//
// A handler's outcome is authoritative. A returned error, including a declared
// application error, is published as returned. A successful result is prepared
// and written even if the request deadline expired after the handler returned;
// only an actual write failure can then prevent delivery.
func (e Endpoint[P, Q, B, R]) Handle(handler Handler[P, Q, B, R]) RouteRegistration {
	if handler == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "endpoint requires a handler"))
	}
	return e.handlePrepared(e.complete(handler))
}

// HandleBound runs bind after request authorization and before validation, then
// the handler it returned. The lifecycle is otherwise that of Handle: decode,
// prohibited-input check, preparation, request authorization, binding,
// validation, handler.
func (e Endpoint[P, Q, B, R]) HandleBound(bind Binding[P, Q, B, R]) RouteRegistration {
	if bind == nil {
		return InvalidRouteRegistration(fault.New(fault.Invalid, "endpoint requires a binding"))
	}
	return e.handleStaged(func(ctx context.Context, input Input[P, Q, B]) (func(context.Context, Input[P, Q, B]) (preparedResponse, error), error) {
		handler, err := bind(ctx, input)
		if err != nil {
			return nil, err
		}
		if handler == nil {
			return nil, InternalError.WithCause(fault.New(fault.Internal, "endpoint binding returned no handler"))
		}
		return e.complete(handler), nil
	})
}

// complete runs a typed handler and prepares its response.
func (e Endpoint[P, Q, B, R]) complete(handler Handler[P, Q, B, R]) func(context.Context, Input[P, Q, B]) (preparedResponse, error) {
	return func(ctx context.Context, input Input[P, Q, B]) (preparedResponse, error) {
		result, err := handler(ctx, input)
		if err != nil {
			return preparedResponse{}, err
		}
		// Headers are derived before encoding, so a failure opens no source.
		headers, err := e.responseHeaders(ctx, result)
		if err != nil {
			return preparedResponse{}, err
		}
		prepared, err := e.response.prepare(ctx, result, e.limits)
		prepared.headers = headers
		return prepared, err
	}
}

// handlePrepared shares all decoding, lifecycle, callback ownership and response
// publication. Transaction adapters prepare their result before outer commit.
func (e Endpoint[P, Q, B, R]) handlePrepared(handler func(context.Context, Input[P, Q, B]) (preparedResponse, error)) RouteRegistration {
	return e.register(nil, handler)
}

// handleStaged selects the completing handler at the binding stage.
func (e Endpoint[P, Q, B, R]) handleStaged(stage preparedStage[P, Q, B]) RouteRegistration {
	return e.register(stage, nil)
}

type preparedStage[P, Q, B any] func(context.Context, Input[P, Q, B]) (func(context.Context, Input[P, Q, B]) (preparedResponse, error), error)

func (e Endpoint[P, Q, B, R]) register(stage preparedStage[P, Q, B], handler func(context.Context, Input[P, Q, B]) (preparedResponse, error)) RouteRegistration {
	if err := e.Validate(); err != nil {
		return InvalidRouteRegistration(err)
	}
	if err := e.response.outputPresentation(); err != nil {
		return InvalidRouteRegistration(err)
	}
	registration := e.route.handle(func(w stdhttp.ResponseWriter, r *stdhttp.Request, path P) { e.serve(w, r, path, stage, handler) }, false)
	registration.contract = e.contributionContract()
	info := registration.info.clone()
	registration.endpoint = func() EndpointInfo { return e.snapshot(info) }
	registration.errors, _ = e.errorDefinitions()
	if e.idempotency != nil {
		registration.handler = idempotentHTTPHandler{registration.handler}
	}
	return registration
}

func (e Endpoint[P, Q, B, R]) serve(w stdhttp.ResponseWriter, r *stdhttp.Request, path P, stage preparedStage[P, Q, B], handler func(context.Context, Input[P, Q, B]) (preparedResponse, error)) {
	if e.response.kind == payloadEvents {
		r = withLastEventID(r)
	}
	ctx := r.Context()
	cookie := e.refreshCookie()
	if (e.response.credentials || cookie != nil) && !checkCredentialRequest(w, r) {
		return
	}
	if cookie != nil {
		// Origin protection precedes reading, so a cross-site request can
		// neither use nor clear the cookie.
		if err := cookie.csrf.check(r); err != nil {
			writeRoutingError(w, r, err)
			return
		}
		if e.body.kind == payloadRefreshCookie || e.response.refreshCookieClears {
			clear, err := cookie.clear()
			if err != nil {
				writeRoutingError(w, r, InternalError.WithCause(err))
				return
			}
			w = &refreshClearingWriter{ResponseWriter: w, clear: clear}
		}
	}
	if err := bindBrowserResponse(ctx, e.idempotency == nil && (e.response.kind == payloadJSON || e.response.kind == payloadEmpty || e.response.kind == payloadRedirect)); err != nil {
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
	query, err := e.query.Decode(ctx, e.query.withoutConsumed(ctx, rawQuery), e.limits.Query)
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
		if err := e.validation.Check(ctx, input, e.limits.Validation); err != nil {
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
	// Framework hot paths (codecs, body reads, file chunks, URL generation and
	// error classification) use callback.Invoke: same goroutine, panics
	// contained with their frames, no per-call goroutine.
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
	// A returned error is authoritative: a later deadline never replaces it.
	if returned != nil {
		writeRoutingError(w, r, returned)
		return
	}
	// The handler succeeded and its response is prepared. A deadline that
	// expired afterwards does not turn completed work into a timeout, but a
	// browser-session credential is never published after the request ended.
	// Wrappers outside the router now deliver it unless the client left.
	completeRoute(ctx)
	if err := publishBrowserSession(ctx, w, data.statusOr(e.response.status)); err != nil {
		writeRoutingError(w, r, authenticationError(err))
		return
	}
	if data.setCookie != "" {
		w.Header().Add("Set-Cookie", data.setCookie)
	}
	if e.response.refreshCookieClears {
		clear, err := cookie.clear()
		if err != nil {
			writeRoutingError(w, r, InternalError.WithCause(err))
			return
		}
		w.Header().Add("Set-Cookie", clear)
	}
	if err := e.response.write(w, r, data); err != nil {
		// The response is committed, so no error document can replace it. The
		// failure still reaches the request observation as a redacted diagnostic.
		recordRequestDiagnostic(r.Context(), errordiag.Describe(err))
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
