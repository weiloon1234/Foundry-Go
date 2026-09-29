# HTTP middleware

Foundry composes native `net/http` wrappers with typed middleware identities.
Declare dependencies in a constructor, then attach the result to a route or
scope. This example is exercised in the independent `httpmiddleware` consumer:

```go
const RouteTrace foundryhttp.MiddlewareID = "app.route-trace"

trace := foundryhttp.DefineMiddleware(RouteTrace,
    func(next http.Handler) (http.Handler, error) {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if route, ok := foundryhttp.MatchedRoute(r.Context()); ok {
                w.Header().Set("X-Matched-Route", string(route.ID))
            }
            next.ServeHTTP(w, r)
        }), nil
    },
)

api := foundryhttp.DefineScope("/api", "api").WithMiddleware(trace)
update := Update.Within(api)
router, err := foundryhttp.NewRouter(update.Handle(service.Update))
```

The returned endpoint retains the same concrete path/query/body/response types.
Use it for named URLs as well as registration, so prefixes have one source of
truth. `Route.WithMiddleware` and `Endpoint.WithMiddleware` append to the route's
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

## Typed rate limiting

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
