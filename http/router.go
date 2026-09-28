package http

import (
	"cmp"
	"context"
	stdhttp "net/http"
	"net/url"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Router is an immutable, concurrency-safe native net/http handler. All route
// validation completes before it is returned; startup never partially registers
// into a shared/global ServeMux. Registration order does not resolve ambiguity.
type Router struct {
	spas      []*spaFallback
	mux       *stdhttp.ServeMux
	routes    []RouteInfo
	endpoints map[RouteID]func() EndpointInfo
	errors    []ErrorDefinition
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
		typedEndpoint := registration.endpoint != nil
		matched := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
			recordMatchedRoute(request.Context(), info.ID)
			ctx := context.WithValue(request.Context(), matchedRouteKey{}, info)
			if typedEndpoint {
				ctx = context.WithValue(ctx, endpointErrorsKey{}, declaredErrors)
			}
			request = request.WithContext(ctx)
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
	handler, pattern := r.mux.Handler(request)
	if pattern != "" {
		// ServeHTTP populates PathValue and Request.Pattern; Handler alone does
		// not. The original ResponseWriter retains native optional capabilities.
		r.mux.ServeHTTP(w, request)
		return
	}
	// Empty pattern means a native fallback. Inspect only that native handler
	// to retain its method matching/Allow source of truth, then encode Foundry's
	// shared error. No application callback can run through this probe writer.
	probe := &routingResponse{header: make(stdhttp.Header)}
	handler.ServeHTTP(probe, request)
	switch probe.status {
	case stdhttp.StatusNotFound:
		if r.serveSPA(w, request) {
			return
		}
		writeRoutingError(w, request, NotFound)
	case stdhttp.StatusMethodNotAllowed:
		w.Header().Set("Allow", probe.header.Get("Allow"))
		writeRoutingError(w, request, MethodNotAllowed)
	default:
		// The native router can also canonicalize a path without a matching
		// endpoint. Preserve its relative redirect and method semantics.
		handler.ServeHTTP(w, request)
	}
}

func writeRoutingError(w stdhttp.ResponseWriter, r *stdhttp.Request, err error) {
	if writeErr := WriteError(w, r, err); writeErr != nil {
		logRouteFailure(r, "HTTP routing error response failed", writeErr)
		panic(stdhttp.ErrAbortHandler)
	}
}

type routingResponse struct {
	header stdhttp.Header
	status int
}

func (r *routingResponse) Header() stdhttp.Header { return r.header }
func (r *routingResponse) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}
func (r *routingResponse) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = stdhttp.StatusOK
	}
	return len(body), nil
}

func nativeRoutingAvailable() bool {
	// GODEBUG=httpmuxgo121=1 disables method and wildcard support even on a new
	// Go SDK. Detect it before accepting registrations that would never match.
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("GET /{foundry_probe}", func(stdhttp.ResponseWriter, *stdhttp.Request) {})
	_, pattern := mux.Handler(&stdhttp.Request{Method: stdhttp.MethodGet, URL: &url.URL{Path: "/probe"}})
	return pattern != ""
}
