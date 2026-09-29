package http

import (
	"cmp"
	stdhttp "net/http"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// fallbackPattern is how route inspection presents a fallback: it can answer
// any unmatched GET or HEAD path, but it is never registered with the native
// router, so it cannot turn another method's 404 into a 405.
const fallbackPattern = "/{path...}"

// routeFallback is the router's last resort for unmatched GET and HEAD
// requests. Its matched state is built once, like a registered route's.
type routeFallback struct {
	handler stdhttp.Handler
	state   *matchedRoute
}

// WithFallback returns an independent router view whose unmatched GET and HEAD
// requests run handler instead of Foundry's shared not_found error, like
// Laravel's Route::fallback; use it for a custom 404 page. It runs only after
// the native router found no route and no SPA fallback served the request.
// Declared routes, method errors (405 with Allow) and endpoint error responses
// are never intercepted, and other methods keep the shared 404.
//
// The handler owns its response, including its status (normally 404). Global
// middleware around the router applies to it; compose fallback-specific
// middleware with ApplyMiddleware. MatchedRoute, request observation and audit
// attribution report its ID. Route inspection lists it as the raw route
// GET /{path...} with RouteInfo.Fallback set. A router has at most one
// fallback; the original router is unchanged.
func (r *Router) WithFallback(id RouteID, handler stdhttp.Handler) (*Router, error) {
	if r == nil || r.mux == nil {
		return nil, fault.New(fault.Invalid, "fallback requires a router")
	}
	if handler == nil || !identifier.Semantic(string(id)) {
		return nil, fault.New(fault.Invalid, "fallback requires a semantic ID and a handler")
	}
	if r.fallback != nil {
		return nil, fault.New(fault.Duplicate, "router already has a fallback")
	}
	for _, route := range r.routes {
		if route.ID == id {
			return nil, fault.New(fault.Duplicate, "fallback route ID is already registered")
		}
	}
	info := RouteInfo{ID: id, Method: GET, Path: fallbackPattern, Parameters: []string{"path"}, Access: Public, Raw: true, Fallback: true}
	result := *r
	result.routes = append(slices.Clone(r.routes), info.clone())
	slices.SortFunc(result.routes, func(a, b RouteInfo) int { return cmp.Compare(a.ID, b.ID) })
	result.fallback = &routeFallback{handler: handler, state: &matchedRoute{info: info}}
	return &result, nil
}

func (f *routeFallback) serve(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
	if f == nil || (r.Method != stdhttp.MethodGet && r.Method != stdhttp.MethodHead) {
		return false
	}
	f.handler.ServeHTTP(w, r.WithContext(matchedContext(r.Context(), f.state)))
	return true
}
