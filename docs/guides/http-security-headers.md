# Typed security-header defaults

Use the shared [middleware assembly](http-middleware.md) to apply response policy
before the router and its error responses:

```go
config := foundryhttp.DefaultSecurityHeadersConfig()
config.Frame = foundryhttp.FrameSameOrigin
config.Referrer = foundryhttp.ReferrerNoReferrer
config.HSTS = value.Set(foundryhttp.HSTSPolicy{
    MaxAge: 24 * time.Hour,
})
handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.SecurityHeaders(config))
```

The [consumer fixture](../../tests/fixtures/consumer/httpsecurity/router.go)
wraps an existing generated endpoint. The framework owns header validation and
transport handling; the service returns its concrete response DTO as before.

`DefaultSecurityHeadersConfig` selects `nosniff`, frame denial, the
`strict-origin-when-cross-origin` referrer policy and `X-XSS-Protection: 0` to
disable legacy browser XSS filtering. HSTS is opt-in. A zero configuration emits
no policy headers; start with the default configuration when defaults are wanted.
Frame and referrer policies have distinct Go types, so assigning one to the other
fails compilation. Invalid externally supplied policy strings reject assembly.

HSTS uses an explicit `Optional[HSTSPolicy]`: omission emits no header, while
an explicitly supplied zero `MaxAge` emits `max-age=0`. Durations must be whole,
nonnegative seconds. Subdomain coverage and the preload directive are explicit;
preload requires subdomain coverage and at least one year, but does not submit
a domain to a preload list. See the [HSTS standard](https://www.rfc-editor.org/rfc/rfc6797.html)
and [preload requirements](https://hstspreload.org/).

HSTS uses `IsSecure(request)`: native TLS unless preceding `TrustedProxy`
middleware supplies an explicitly validated public scheme. Client-IP header
trust alone cannot prove HTTPS. See [public URL and proxy origin handling](http-public-urls.md)
for middleware order and typed source declarations.

Custom native response fields use separate `HeaderName` and `HeaderValue` types:

```go
config.Extra = []foundryhttp.ResponseHeader{
    {Name: "X-Build", Value: "release-1"},
}
```

Names use the shared HTTP token validation. Values reject control bytes except
horizontal tab and are bounded to 8,192 bytes. Configurations allow at most 64
extra headers and 32 KiB of declared fields. Repeated names are rejected after
canonicalization. The typed security-policy fields and HTTP framing/hop headers
cannot be supplied through `Extra`. Arbitrary header-specific languages in custom
values are explicit native extensions; they are not inferred or validated as CSP,
cookies or other higher-level contracts.

Configuration is copied when declared. `SecurityHeadersConfig.Validate` checks it
without starting a server; middleware construction rejects invalid declarations.
The middleware sets defaults before calling the next handler, preserving the
original ResponseWriter and its optional streaming/upgrade interfaces. A later
handler may deliberately replace a default; no response body is buffered and no
already-committed header is rewritten.

The [HTML frame policy](https://html.spec.whatwg.org/multipage/browsing-the-web.html#the-x-frame-options-header)
and [Referrer Policy](https://www.w3.org/TR/referrer-policy/) standards define
browser behavior. [Typed CSP and request nonces](http-content-security-policy.md) now accompany
this middleware. [Public origins and trusted-proxy HTTPS](http-public-urls.md)
supply the explicit transport policy for HSTS. Security-header/CSP and proxy
HSTS integration passed focused and combined full regression checks.
