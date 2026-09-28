# Trusted proxy client addresses

**Full verification passed.** Complete repository acceptance supplements focused
runtime/consumer races, bounded fuzzing and compiler/editor checks.

The HTTP kernel initially attributes a request to its socket peer. Apply
`TrustedProxy` through [global middleware](http-middleware.md) before rate limits,
guards or domain services consume client attribution:

```go
handler, err := foundryhttp.ApplyMiddleware(router,
    foundryhttp.TrustedProxy(foundryhttp.TrustedProxyConfig{
        Proxies: []netip.Prefix{
            netip.MustParsePrefix("10.20.0.0/24"),
        },
        Headers: []foundryhttp.ProxyHeader{
            foundryhttp.ForwardedHeader(),
            foundryhttp.XForwardedForHeader(),
        },
    }),
)
```

These networks are examples. Declare the actual proxy peers in the deployment.
The zero configuration trusts no peers; loopback, private networks and CDNs are
never trusted automatically. Invalid prefixes, repeated networks or sources and
networks without header declarations reject assembly. Configuration is copied
when the middleware is declared, and `TrustedProxyConfig.Validate` performs a
separate configuration check without I/O.

`ForwardedHeader()` reads RFC 7239 `for` parameters. `XForwardedForHeader()` reads
a comma-separated chain of bare IP addresses. `ClientIPHeader("CF-Connecting-IP")`
or `ClientIPHeader("X-Real-IP")` reads exactly one address, which a trusted peer
must overwrite. Custom single-address headers use the same typed constructor.
Chain headers require their corresponding chain descriptor.

The first present declared source wins. A missing header moves to the next
source; malformed input in a present source returns the shared 400 response and
does not fall back. Headers from an untrusted or unknown socket peer are ignored,
including malformed values. Hostnames are never resolved to establish trust.

Chain traversal starts with the socket peer and works right to left. It accepts
the previous address only while the current address belongs to an explicitly
trusted network. The first untrusted address becomes the resolved client IP;
earlier values cannot override it. `Forwarded` elements with missing, unknown or
obfuscated `for` addresses stop traversal. Such a request retains the nearest
known address, which may be a proxy; it does not claim an unknowable original IP.
Proxy configuration must ensure that trusted peers append reliable chain entries
or replace single-address headers.

```go
client := foundryhttp.ClientIP(ctx) // netip.Addr from the service's context
```

The [compiling consumer](../../tests/fixtures/consumer/httpproxy/router_test.go)
uses an existing generated endpoint and receives this IP through the service's
ordinary context. `ClientIP` reads the existing `attribution.Request.IP`; events,
jobs and audit capture that same provenance. Correlation IDs, user agents and
model/system attribution remain intact. This metadata does not authenticate a
person or grant authorization.

`PeerIP(request)` independently returns the native peer. The middleware preserves
`RemoteAddr`, `Host`, URL, TLS, headers, response-writer capabilities and request
cancellation. Explicit `OriginHeaders` now capture validated public scheme and
authority in context; native transport fields remain unchanged. See
[public URLs and HTTPS behind proxies](http-public-urls.md). This integration
passed focused and combined transport full regression checks.
[Signed URLs](http-signed-urls.md) use the explicit public-origin policy.

IPv4-mapped addresses are normalized to IPv4. IPv4 and IPv6 networks are explicit;
an IPv6 range does not implicitly trust IPv4. Native socket interface zones are
removed from attribution; forwarded addresses with zones are rejected. RFC 7239
quoted IPv6 nodes and numeric/obfuscated ports are understood, while attribution
retains only the IP. Duplicate parameter names and malformed quoting are rejected.

Bounds are 256 configured networks, eight ordered header sources, 64 header lines
or forwarding hops and 8,192 bytes for the selected header list. Parameter names
reuse the existing typed HTTP token validator and its 256-byte name bound.
Unknown extension values are parsed within the same total byte budget. Parsing
and malformed-input errors do not expose submitted values in public responses.

This preserves Rust Foundry's configurable header sources while changing its
default CDN trust and leftmost-XFF selection to explicit peers and chain-aware
resolution. Protocol references are [RFC 7239](https://www.rfc-editor.org/rfc/rfc7239.html)
and Go's [proxy forwarding documentation](https://pkg.go.dev/net/http/httputil#ProxyRequest.SetXForwarded).
The [HTTP blueprint](../../blueprint/08-http-validation-and-responses.md) records current milestone acceptance.
