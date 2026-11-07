# HTTP middleware

<a id="agent-entry"></a>
## Agent entry

- [Declare and attach middleware](#recipe-attach-middleware)
- [Attach a credential quota](#recipe-credential-quota)
- [Protect credential stores](authentication.md#recipe-credential-protection)
- [Make an operation replayable](idempotent-operations.md#recipe-idempotent-endpoint)
- [Configure request locales](validation-messages.md#recipe-configure-messages)

<a id="recipe-attach-middleware"></a>
## Declare and attach middleware

Foundry composes native `net/http` wrappers with typed middleware identities.
Declare dependencies in a constructor, then attach the result to a route or
scope. This example is exercised in the independent `httpmiddleware` consumer:

<!-- compiled-source: tests/fixtures/consumer/httpmiddleware/router.go#middleware-scope -->
```go
const RouteTrace foundryhttp.MiddlewareID = "fixture.route-trace"

var trace = foundryhttp.DefineMiddleware(RouteTrace, func(next http.Handler) (http.Handler, error) {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if matched, ok := foundryhttp.MatchedRoute(r.Context()); ok {
			w.Header().Set("X-Matched-Route", string(matched.ID))
		}
		next.ServeHTTP(w, r)
	}), nil
})

var API = foundryhttp.DefineScope("/api", "api").WithMiddleware(trace)
var Update = foundryhttp.ConfigureEndpoint(httpendpoints.Update, foundryhttp.Within(API))

func Router(service httpendpoints.Service) (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(Update.Handle(service.Update))
}
```

For one route, configure its policy before declaring its endpoint:

<!-- compiled-source: tests/fixtures/consumer/httpmiddleware/router.go#middleware-route -->
```go
func Probe(policy foundryhttp.Middleware) foundryhttp.RouteRegistration {
	route := foundryhttp.ConfigureRoute(foundryhttp.DefineRoute(foundryhttp.RouteSpec{
		ID: "probe", Method: foundryhttp.GET, Access: foundryhttp.Public,
	}, foundryhttp.StaticPath("/probe")), foundryhttp.Use(policy))
	endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	return endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	})
}
```

The returned endpoint retains the same concrete path/query/body/response types.
Use it for named URLs as well as registration, so prefixes have one source of
truth. `Use(...)` in `ConfigureRoute` or `ConfigureEndpoint` appends to the route's
chain; nested scopes compose parent, child and route chains in that order.

The first middleware is outermost: request execution follows declaration order
and response execution unwinds in reverse. There is no hidden priority sorting.
Factories run once, inside-out, during assembly and may return configuration
errors. They must not start I/O or background goroutines. Duplicate IDs in one
composed chain, invalid declarations, constructor failures and empty handlers
fail assembly before a router is returned.

Route middleware executes after native route matching and before parameter/body
decoding. `MatchedRoute` is available even when middleware short-circuits. It
contains owned route metadata and middleware IDs. A matched route is a routing
fact, not authentication or authorization. A wrapper cannot change the endpoint's
declared access contract merely by adding a header or context value.

For configured global policy, use the application builder:

<!-- compiled-source: tests/fixtures/consumer/httpmiddleware/router.go#middleware-global -->
```go
func GlobalApplication(ctx context.Context, settings application.Settings, policies ...foundryhttp.Middleware) (*application.App, error) {
	return application.New(settings).Use(policies...).Build(ctx)
}
```

Use `ApplyMiddleware(router, ...)` for global transport policy that must also
cover redirects, missing routes and method errors. Global wrappers run before
route matching, so they do not yet have `MatchedRoute`. Route inspection lists
route/scope middleware; it does not infer arbitrary wrappers outside the router.

Assembly rejects two ordering hazards with an actionable error:

- A policy that reads [trusted proxy](http-trusted-proxy.md) results — rate
  limits, CSRF, `PublicURLs`, browser sessions and credential-request checks —
  declared before `TrustedProxy` in one chain, or a route that installs
  `TrustedProxy` beneath such a global policy. Declare `TrustedProxy` first.
- A router whose signed routes, `PublicLinks` pagination or `RequirePublicURLs()`
  routes are not covered by [`PublicURLs`](http-public-urls.md) in the same
  `ApplyMiddleware` call or on the route itself.

The checks inspect the chain applied directly around a router and each route's
own chain; wrap the router once with the complete global chain.

Foundry passes the native writer through unchanged. A custom wrapper that
replaces it must preserve the optional capabilities it needs, such as flushing
or hijacking. Request handling is synchronous: do not use the writer after
`ServeHTTP` returns. Kernel request deadlines, body limits and draining remain
outside application middleware, and middleware does not open implicit database
transactions. Built-in [CORS](http-cors.md), [trusted proxies](http-trusted-proxy.md)
and [security headers](http-security-headers.md) use this same assembly API.

Configured locale selection belongs to [validation messages](validation-messages.md#recipe-configure-messages).
Cookie-authenticated unsafe requests also need [origin and CSRF protection](browser-sessions.md); CORS does not provide that protection.
Use the [typed idempotency adapter](idempotent-operations.md#recipe-idempotent-endpoint) for supported replayable operations. Middleware must not decode the request body or implement a second replay store.

<a id="recipe-credential-quota"></a>
## Typed rate limiting

<!-- compiled-source: tests/fixtures/consumer/httpmiddleware/router.go#middleware-credential-group -->
```go
func CredentialGroup(parent foundryhttp.Scope, limiter ratelimit.Limiter[netip.Addr]) foundryhttp.Scope {
	return foundryhttp.DefineScope("/credentials", "credentials").Within(parent).
		WithMiddleware(foundryhttp.RateLimitByIP(limiter))
}
```

Use [RateLimit or RateLimitByIP](rate-limiting.md) with a bound typed quota. The
framework reuses trusted attribution, emits shared 429/503 error contracts, and
keeps the native handler inputs after admission. Install TrustedProxy first when
forwarded IP attribution is required; assembly rejects the reverse order.

`RateLimitByIP` keys IPv4 clients per address and IPv6 clients per /64 network,
because one IPv6 subscriber usually controls a whole /64. Use
`RateLimitByIPWith(limiter, foundryhttp.IPRateLimitOptions{IPv6Prefix: 56})` to
change the grouping (IPv4 1–32, IPv6 1–128; zero selects the default).

Every 429 from `RateLimit` or `RateLimitByIP` carries `Retry-After` (at least one
second) together with `X-RateLimit-Limit`, `X-RateLimit-Remaining` and
`X-RateLimit-Reset`. A handler that calls a limiter itself can return
`foundryhttp.RateLimitExceeded(decision)` (optionally wrapped); the shared error
writer then sends the same headers from that denied decision. An allowed
decision passed to it is an internal error. A bare `RateLimited` error carries
no retry information, so its 429 has no `Retry-After`.
