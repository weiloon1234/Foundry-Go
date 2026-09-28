package http

// WithMiddleware appends middleware inside this scope's existing chain. When
// nested, parent scope middleware runs before child scope and route middleware.
func (s Scope) WithMiddleware(middlewares ...Middleware) Scope {
	if s.err == nil {
		s.middlewares, s.err = appendMiddlewares(s.middlewares, middlewares)
	}
	return s
}

// WithMiddleware appends native transport middleware to this route. It runs
// after native route matching and before path/query/body decoding. MatchedRoute
// is available, including for short-circuited requests. Middleware must not
// replace framework request identity or bypass the declared access contract.
func (r Route[P]) WithMiddleware(middlewares ...Middleware) Route[P] {
	if r.err == nil {
		r.middlewares, r.err = appendMiddlewares(r.middlewares, middlewares)
	}
	return r
}

// WithMiddleware preserves the endpoint's concrete request and response types.
// Standard wrappers own only the native HTTP transport boundary.
func (e Endpoint[P, Q, B, R]) WithMiddleware(middlewares ...Middleware) Endpoint[P, Q, B, R] {
	e.route = e.route.WithMiddleware(middlewares...)
	return e
}
