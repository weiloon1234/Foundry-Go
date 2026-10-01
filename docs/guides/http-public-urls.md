# Public URLs and HTTPS behind proxies

`PublicURLs` binds an approved origin to a request. `PublicURL` combines that
origin with the relative URL produced by a typed route or endpoint. Workers and
other request-independent kernels use `AbsoluteURL` with an explicit configured
`Origin`; there is no global application URL or guessed request Host.

```go
public := foundryhttp.PublicURLConfig{
    AllowedOrigins: []foundryhttp.Origin{
        "https://app.example.test",
        "http://alias.example.test",
    },
    Canonical: value.Set(foundryhttp.Origin("https://app.example.test")),
}
handler, err := foundryhttp.ApplyMiddleware(router,
    foundryhttp.TrustedProxy(proxy),
    foundryhttp.PublicURLs(public),
    foundryhttp.SecurityHeaders(security),
)
```

Origins include the scheme and port. Hostname casing and default ports normalize
through the shared `ParseOrigin` implementation. At least one origin or pattern
is required; each list contains at most 256 entries. Repeated normalized origins, opaque `null`
origins and invalid authorities reject assembly. A canonical origin must also
appear in the allowed list. It changes generated links for accepted aliases,
without redirecting requests or asserting that a connection used HTTPS.

Unlisted or malformed request origins receive a shared 400 response before route
execution. Outside this middleware, `PublicOrigin(ctx)` reports absence and
`PublicURL(ctx, relative)` returns an error. It never silently trusts `Host`.

### Tenant subdomains and probes

`AllowedPatterns` admits HTTP(S) subdomain wildcards using the shared
[`OriginPattern`](http-cors.md) grammar, for example
`"https://*.tenants.example.com"`. A wildcard matches one or more complete labels
with the same scheme and port, never the bare suffix; non-HTTP(S) patterns are
rejected here. The admitted request origin itself becomes the URL base (unless
`Canonical` is set, which must be an allowed origin or match a pattern), so
typed links and [signed URLs](http-signed-urls.md) stay on the tenant's host. A
signature binds that exact tenant origin: another tenant cannot replay it.

`ExemptPaths` lists exact request paths served without host admission, such as
load-balancer or Kubernetes probes that arrive with `Host: <pod-ip>`:

```go
public := foundryhttp.PublicURLConfig{
    AllowedOrigins:  []foundryhttp.Origin{"https://app.example.com"},
    AllowedPatterns: []foundryhttp.OriginPattern{"https://*.tenants.example.com"},
    ExemptPaths:     []string{"/healthz", "/readyz"},
}
```

Exempt paths are exact, clean absolute paths (at most 64); no public origin is
bound for them, so they cannot generate public or signed URLs.

### Routes that generate public URLs

Signed routes and pagination endpoints with `PublicLinks` require a bound
public origin. `ApplyMiddleware` rejects a router whose such routes are not
covered by `PublicURLs` in the same chain (or on the route itself), so the
misconfiguration fails at assembly instead of as a runtime 500. Mark other raw
routes that call `PublicURL` with the `RequirePublicURLs()` route middleware to
get the same check. A router served directly, without `ApplyMiddleware`, still
answers such routes with a shared 500 instead of guessing an origin.

```go
relative, err := endpoint.URL(ctx, path, query)
if err != nil {
    return err
}
absolute, err := foundryhttp.PublicURL(ctx, relative)
```

`AbsoluteURL(origin, relative)` is also available for explicitly configured
background links. Absolute URLs, `//` network paths, fragments, backslashes,
invalid escapes and literal control/space bytes are rejected as relative input.
Typed route path/query escaping is preserved.

## Declaring proxy origin sources

The existing [proxy trust configuration](http-trusted-proxy.md) owns both client-IP
and origin trust. `Headers` selects IP sources; `OriginHeaders` selects scheme and
authority sources. Neither is automatic. Socket peer membership is checked before
any forwarding data is accepted.

```go
proxy := foundryhttp.TrustedProxyConfig{
    Proxies: []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")},
    Headers: []foundryhttp.ProxyHeader{foundryhttp.ForwardedHeader()},
    OriginHeaders: []foundryhttp.ProxyOriginHeader{
        foundryhttp.ForwardedOriginHeader(),
    },
}
```

`ForwardedOriginHeader` reads `host` and `proto` from the same RFC 7239 element
at the trusted chain boundary. Both are required there. Walking stops at the
first untrusted address or unknown/obfuscated/missing `for`; it never skips that
element to borrow an earlier host or scheme. The shared bounded Forwarded parser
supplies both IP and origin processing.

For a proxy that overwrites single headers, use `XForwardedOriginHeaders()` for
`X-Forwarded-Proto` plus `X-Forwarded-Host`, or `ProxyOriginHeaders(scheme, host)`
for custom names. Both values are required and repeated fields/comma lists are
rejected. Independently appended legacy lists do not prove that host, scheme and
IP entries describe the same hop. Use RFC Forwarded for origin chains.

If the proxy preserves the native public Host and only supplies a scheme, use
`ProxySchemeHeader("X-Forwarded-Proto")`. The first present declared source wins;
malformed or incomplete values return 400 instead of falling back to another
source. An untrusted peer's forwarding headers are ignored. Only HTTP and HTTPS
schemes are accepted, and authorities share ordinary origin validation.

Choose the descriptor from what the trusted edge actually overwrites:

| Proxy | Forwards | Origin descriptor |
| --- | --- | --- |
| Laravel Herd and Valet | the public `Host` and `X-Forwarded-Proto`, no `X-Forwarded-Host` | `ProxySchemeHeader("X-Forwarded-Proto")` |
| Go `httputil.ReverseProxy` with `ProxyRequest.SetXForwarded` | `X-Forwarded-Proto` and `X-Forwarded-Host`; `SetURL` rewrites `Host` to the upstream | `XForwardedOriginHeaders()` |
| nginx `proxy_pass` | by default no forwarding headers, and `Host` becomes `$proxy_host` (the upstream) | set `X-Forwarded-Proto $scheme` and `X-Forwarded-Host $host` for `XForwardedOriginHeaders()`, or `Host $host` and `X-Forwarded-Proto $scheme` for `ProxySchemeHeader` |
| An RFC 7239 proxy | `Forwarded` with `proto` and `host` | `ForwardedOriginHeader()` |

With `XForwardedOriginHeaders()`, a request carrying `X-Forwarded-Proto` but no
`X-Forwarded-Host` is a 400: the pair is incomplete. Declaring
`ProxySchemeHeader("X-Forwarded-Proto")` beside it is rejected as a repeated
source, so a missing host never falls back to `Host`; pick the one descriptor that
matches the proxy.

## Transport security remains separate from URL configuration

`IsSecure(request)` uses the validated forwarded scheme when present, otherwise
native TLS. An HTTPS canonical origin or an untrusted forwarding header cannot
make a plain request secure. Conversely, an HTTP client request forwarded over
internal TLS remains public HTTP when a trusted origin source declares it.

HSTS uses this determination. Place `TrustedProxy` before `SecurityHeaders` when
TLS terminates at the proxy; later secure-cookie/session policies use the same
boundary. `ApplyMiddleware` rejects `PublicURLs`, CSRF, rate limits, browser
sessions and credential-request checks placed before `TrustedProxy`. The middleware preserves native `RemoteAddr`, `Host`, `URL`, headers,
TLS state, writer capabilities, attribution and cancellation. Trusted origin
metadata lives in request context and does not mutate the caller's request.

The [independent consumer](../../tests/fixtures/consumer/httpsecurity/public_urls.go)
wraps an existing typed endpoint. Its service builds an absolute link from that
endpoint's generated path/query arguments. The accompanying HTTP test covers
proxy TLS termination, an HTTP alias, canonical links and rejected host spoofing.

[RFC 7239](https://www.rfc-editor.org/rfc/rfc7239.html) defines Forwarded host/proto
semantics. Foundry's explicit peer policy is still a deployment boundary: the
trusted proxy must append truthful Forwarded elements or overwrite its declared
single headers. Configuration cannot prove that external infrastructure does so.
