# Trusted proxy client addresses

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
a comma-separated chain of IP addresses; entries may carry a port, as some load
balancers append (`203.0.113.9:51234`, `[2001:db8::9]:443`), and bare IPv6 is
accepted. `ClientIPHeader("CF-Connecting-IP")` or `ClientIPHeader("X-Real-IP")`
reads exactly one address, which a trusted peer must overwrite. Custom
single-address headers use the same typed constructor. Chain headers require
their corresponding chain descriptor.

The first present declared source wins. A missing header moves to the next
source; a present source never falls back to a lower-priority header. Headers
from an untrusted or unknown socket peer are ignored. Hostnames are never
resolved to establish trust. Client-address data never rejects a request.

Chain traversal starts with the socket peer and works right to left. It accepts
the previous address only while the current address belongs to an explicitly
trusted network. The first untrusted address becomes the resolved client IP;
earlier values cannot override it. Entries are parsed only as the walk consumes
them, so client-supplied prefixes beyond the trust boundary (`unknown`, garbage,
oversized values, empty list slots) are never inspected. An unknown, obfuscated,
missing or malformed entry that the walk does consume stops traversal: the
request retains the nearest trusted address, which may be a proxy, and does not
claim an unknowable original IP. An empty `X-Forwarded-For` resolves to the peer.
A repeated or unparseable single-address header also resolves to the peer.
Proxy configuration must ensure that trusted peers append reliable chain entries
or replace single-address headers.

Middleware that reads the resolved address or public origin must run inside
`TrustedProxy`: `ApplyMiddleware` rejects a chain in which rate limits, CSRF,
`PublicURLs`, browser sessions or credential-request checks precede it, and a
router whose route installs `TrustedProxy` beneath such a global policy.

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
[public URLs and HTTPS behind proxies](http-public-urls.md), including
[the origin descriptor for common proxies](http-public-urls.md#declaring-proxy-origin-sources).
[Signed URLs](http-signed-urls.md) use the explicit public-origin policy.

IPv4-mapped addresses are normalized to IPv4. IPv4 and IPv6 networks are explicit;
an IPv6 range does not implicitly trust IPv4. Native socket interface zones are
removed from attribution; forwarded addresses with zones are unusable hops. RFC
7239 quoted IPv6 nodes and numeric/obfuscated ports are understood, while
attribution retains only the IP. A `Forwarded` element with repeated parameters
or malformed quoting is an unusable hop. Each header line is tokenized
separately, and elements split at every comma, even inside quotes: no `for`,
`by`, `proto` or `host` value contains one, so an unterminated client quote
cannot swallow a hop that a trusted proxy appended to the same or a later line.
A quoted comma only produces malformed halves, which stop the trusted walk.

Bounds are 256 configured networks, eight ordered header sources, 64 consumed
forwarding hops and 256 bytes per consumed hop address. Parameter names reuse the
existing typed HTTP token validator. Parsing never exposes submitted values in
public responses. Origin headers (`OriginHeaders`) remain strict: malformed
scheme/authority data at the trusted boundary returns the shared 400 response.

This preserves Rust Foundry's configurable header sources while changing its
default CDN trust and leftmost-XFF selection to explicit peers and chain-aware
resolution. Protocol references are [RFC 7239](https://www.rfc-editor.org/rfc/rfc7239.html)
and Go's [proxy forwarding documentation](https://pkg.go.dev/net/http/httputil#ProxyRequest.SetXForwarded).
The [HTTP blueprint](../../blueprint/08-http-validation-and-responses.md) records current milestone acceptance.
