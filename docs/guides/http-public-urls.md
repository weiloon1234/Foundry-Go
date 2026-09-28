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
through the shared `ParseOrigin` implementation. The list must be nonempty and
contains at most 256 exact origins. Repeated normalized origins, opaque `null`
origins and invalid authorities reject assembly. A canonical origin must also
appear in the allowed list. It changes generated links for accepted aliases,
without redirecting requests or asserting that a connection used HTTPS.

Unlisted or malformed request origins receive a shared 400 response before route
execution. Outside this middleware, `PublicOrigin(ctx)` reports absence and
`PublicURL(ctx, relative)` returns an error. It never silently trusts `Host`.

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

## Transport security remains separate from URL configuration

`IsSecure(request)` uses the validated forwarded scheme when present, otherwise
native TLS. An HTTPS canonical origin or an untrusted forwarding header cannot
make a plain request secure. Conversely, an HTTP client request forwarded over
internal TLS remains public HTTP when a trusted origin source declares it.

HSTS uses this determination. Place `TrustedProxy` before `SecurityHeaders` when
TLS terminates at the proxy; later secure-cookie/session policies use the same
boundary. The middleware preserves native `RemoteAddr`, `Host`, `URL`, headers,
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
