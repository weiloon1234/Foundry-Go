package http

import (
	"cmp"
	"context"
	stdhttp "net/http"
	"net/url"
	"slices"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Router is an immutable, concurrency-safe native net/http handler. All route
// validation completes before it is returned; startup never partially registers
// into a shared/global ServeMux. Registration order does not resolve ambiguity.
type Router struct {
	spas      []*spaFallback
	fallback  *routeFallback
	mux       *stdhttp.ServeMux
	routes    []RouteInfo
	endpoints map[RouteID]func() EndpointInfo
	errors    []ErrorDefinition
	// bodyCeiling is the largest declared route body limit; see WithBodyLimit.
	bodyCeiling int64
}

// matchedRoute is the immutable per-route state published to a matched
// request. It is built once at registration, so matching allocates no metadata.
type matchedRoute struct {
	info   RouteInfo
	errors []ErrorDefinition
	typed  bool
	budget routeBudget
}

// matchedContext publishes a matched route to the request: its observation
// label, MatchedRoute metadata and audit attribution (never the raw URL).
func matchedContext(ctx context.Context, state *matchedRoute) context.Context {
	if scope := requestScopeFrom(ctx); scope != nil && scope.observation != nil {
		scope.observation.route.Store(state)
	}
	ctx = context.WithValue(ctx, matchedRouteKey{}, state)
	if attributed, err := attribution.WithRoute(ctx, attribution.Route{Method: string(state.info.Method), Name: string(state.info.ID)}); err == nil {
		ctx = attributed
	}
	return ctx
}

// NewRouter rejects duplicate IDs, invalid bindings and ambiguous native routing
// patterns. It performs no I/O. The same result can be passed to HTTP Module or
// Prepare, or used directly by net/http testing tools.
func NewRouter(registrations ...RouteRegistration) (*Router, error) {
	if !nativeRoutingAvailable() {
		return nil, fault.New(fault.Invalid, "Foundry routing requires modern net/http ServeMux behavior")
	}
	router := &Router{mux: stdhttp.NewServeMux(), endpoints: make(map[RouteID]func() EndpointInfo)}
	ids := make(map[RouteID]bool, len(registrations))
	errorCatalog := make(map[ErrorCode]ErrorDefinition)
	for _, definition := range ErrorDefinitions() {
		errorCatalog[definition.Code] = definition
	}
	for _, registration := range registrations {
		if registration.err != nil {
			return nil, registration.err
		}
		if registration.handler == nil {
			return nil, fault.New(fault.Invalid, "empty route registration")
		}
		if ids[registration.info.ID] {
			return nil, fault.New(fault.Duplicate, "route ID is already registered: "+string(registration.info.ID))
		}
		ids[registration.info.ID] = true
		handler, err := ApplyMiddleware(registration.handler, registration.middlewares...)
		if err != nil {
			return nil, err
		}
		info := registration.info.clone()
		declaredErrors := slices.Clone(registration.errors)
		for _, definition := range declaredErrors {
			if prior, exists := errorCatalog[definition.Code]; exists && prior != definition {
				return nil, fault.New(fault.Conflict, "HTTP error code has conflicting declarations")
			}
			errorCatalog[definition.Code] = definition
		}
		state := &matchedRoute{info: info, errors: declaredErrors, typed: registration.endpoint != nil, budget: routeBudget{timeout: info.Timeout, bodyBytes: info.MaxBodyBytes}}
		router.bodyCeiling = max(router.bodyCeiling, info.MaxBodyBytes)
		handoff := registration.handoff
		matched := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
			// The native mux received the router's fallback probe. A matched
			// route continues with the original writer and its capabilities.
			if probe, ok := w.(*routingResponse); ok {
				probe.matched = true
				w = probe.native
			}
			// A more specific owner answers outside this route, as an unmatched
			// request would reach it: no route state, budget or middleware.
			if handoff != nil && handoff(w, request) {
				return
			}
			var kernel *requestBudget
			if scope := requestScopeFrom(request.Context()); scope != nil {
				kernel = scope.budget
			}
			ctx, release := state.budget.context(matchedContext(request.Context(), state), w, kernel)
			defer release()
			// Wrappers outside the router observe this route's effective
			// context, not the kernel deadline it may have replaced.
			defer publishRoute(ctx)()
			request = request.WithContext(ctx)
			if !state.budget.body(w, request, kernel) {
				return
			}
			handler.ServeHTTP(w, request)
		})
		if err := callback.Invoke("HTTP route registration", func() error {
			router.mux.Handle(registration.pattern, matched)
			return nil
		}); err != nil {
			return nil, fault.Wrap(fault.Conflict, "route conflicts with an existing pattern: "+string(registration.info.ID), err)
		}
		router.routes = append(router.routes, registration.info.clone())
		if registration.endpoint != nil {
			router.endpoints[registration.info.ID] = registration.endpoint
		}
	}
	slices.SortFunc(router.routes, func(a, b RouteInfo) int { return cmp.Compare(a.ID, b.ID) })
	for _, definition := range errorCatalog {
		router.errors = append(router.errors, definition)
	}
	slices.SortFunc(router.errors, func(a, b ErrorDefinition) int { return cmp.Compare(a.Code, b.Code) })
	return router, nil
}

// Routes returns deterministic snapshots ordered by semantic route ID.
func (r *Router) Routes() []RouteInfo {
	if r == nil {
		return nil
	}
	result := make([]RouteInfo, len(r.routes))
	for i, info := range r.routes {
		result[i] = info.clone()
	}
	return result
}

func (r *Router) ServeHTTP(w stdhttp.ResponseWriter, request *stdhttp.Request) {
	if r == nil || r.mux == nil {
		writeRoutingError(w, request, Unavailable)
		return
	}
	if ambiguousEncodedPath(request.URL.EscapedPath()) {
		writeRoutingError(w, request, BadRequest)
		return
	}
	// Match once. A registered route unwraps this probe and continues with the
	// original writer. Otherwise the native fallback retains its method
	// matching/Allow source of truth: 404 and 405 become Foundry's shared error,
	// and a native canonical redirect passes through unchanged.
	if len(r.spas) != 0 {
		request = request.WithContext(context.WithValue(request.Context(), spaRoutesKey{}, r.spas))
	}
	probe := &routingResponse{native: w}
	r.mux.ServeHTTP(probe, request)
	if probe.matched || probe.passthrough {
		return
	}
	switch probe.status {
	case stdhttp.StatusNotFound:
		if r.serveSPA(w, request) || r.fallback.serve(w, request) {
			return
		}
		writeRoutingError(w, request, NotFound)
	case stdhttp.StatusMethodNotAllowed:
		w.Header().Set("Allow", probe.header.Get("Allow"))
		writeRoutingError(w, request, MethodNotAllowed)
	default:
		writeRoutingError(w, request, NotFound)
	}
}

func writeRoutingError(w stdhttp.ResponseWriter, r *stdhttp.Request, err error) {
	if writeErr := WriteError(w, r, err); writeErr != nil {
		logRouteFailure(r, "HTTP routing error response failed", writeErr)
		panic(stdhttp.ErrAbortHandler)
	}
}

// routingResponse receives only the native mux's own fallback responses. A
// matched route replaces it with the original writer before any application
// code runs. Missing-route and method errors are held for Foundry's shared error;
// any other native response, such as a canonical redirect, passes through.
type routingResponse struct {
	native      stdhttp.ResponseWriter
	header      stdhttp.Header
	status      int
	matched     bool
	passthrough bool
}

func (r *routingResponse) Header() stdhttp.Header {
	if r.passthrough {
		return r.native.Header()
	}
	if r.header == nil {
		r.header = make(stdhttp.Header)
	}
	return r.header
}
func (r *routingResponse) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	if status == stdhttp.StatusNotFound || status == stdhttp.StatusMethodNotAllowed {
		return
	}
	r.passthrough = true
	header := r.native.Header()
	for name, values := range r.header {
		header[name] = values
	}
	r.native.WriteHeader(status)
}
func (r *routingResponse) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(stdhttp.StatusOK)
	}
	if !r.passthrough {
		return len(body), nil
	}
	return r.native.Write(body)
}

func (r *Router) routeBodyCeiling() int64 {
	if r == nil {
		return 0
	}
	return r.bodyCeiling
}

func nativeRoutingAvailable() bool {
	// GODEBUG=httpmuxgo121=1 disables method and wildcard support even on a new
	// Go SDK. Detect it before accepting registrations that would never match.
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("GET /{foundry_probe}", func(stdhttp.ResponseWriter, *stdhttp.Request) {})
	_, pattern := mux.Handler(&stdhttp.Request{Method: stdhttp.MethodGet, URL: &url.URL{Path: "/probe"}})
	return pattern != ""
}
