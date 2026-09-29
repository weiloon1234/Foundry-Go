# Browser response sharing

CORS uses the shared [middleware assembly](http-middleware.md). Keep its policy
around the router so preflights reach it before method matching, decoding or
route authentication. The [consumer fixture](../../tests/fixtures/consumer/httpcors/router.go)
applies this to an existing generated endpoint; it declares no OPTIONS route.

```go
handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.CORS(foundryhttp.CORSConfig{
    Origins:       []foundryhttp.Origin{"https://console.example.test"},
    Methods:       []foundryhttp.Method{foundryhttp.PATCH},
    Headers:       []foundryhttp.HeaderName{"Content-Type", "Authorization"},
    ExposeHeaders: []foundryhttp.HeaderName{"X-Request-Id"},
    Credentials:   true,
    MaxAge:        10 * time.Minute,
}))
```

Configuration is copied when `CORS` is declared. Assembly rejects invalid
configuration before serving; `CORSConfig.Validate` also checks it directly.
Typed methods, origins and header names remain distinct in completion and at
compile time. External strings still require validation. The zero policy shares
with no origins and accepts no preflight methods.

`Origin` accepts an HTTP(S) authority without path, user information, query or
fragment. `ParseOrigin` normalizes host casing, IP literals and default ports;
it does not look up DNS or perform IDNA conversion. Use ASCII/punycode hostnames.
`Origins` entries match complete origins, with no suffix or subdomain expansion.
`NullOrigin` explicitly permits the browser's opaque-origin serialization; its
meaning is broader than one particular website.

`OriginPatterns` adds two further, validated forms:

- exact non-web origins serialized by embedded webviews and extensions, such as
  `capacitor://localhost`, `tauri://localhost`, `ionic://localhost` or
  `chrome-extension://<id>` (scheme and host are compared case-insensitively);
- HTTP(S) subdomain wildcards such as `https://*.example.com` or
  `https://*.example.com:8443`. A wildcard matches one or more complete labels
  before the suffix (`a.example.com`, `a.b.example.com`), never the suffix itself,
  and requires the same scheme and port. Bare `*`, partial labels (`a*.example.com`),
  nested wildcards, single-label suffixes (`*.com`) and IP suffixes are rejected.
  Public-suffix knowledge is not built in, so never declare `*.co.uk`-style suffixes.

```go
foundryhttp.CORSConfig{
    Origins:        []foundryhttp.Origin{"https://console.example.com"},
    OriginPatterns: []foundryhttp.OriginPattern{"capacitor://localhost", "https://*.tenants.example.com"},
    Methods:        []foundryhttp.Method{foundryhttp.PATCH},
}
```

A matching request Origin is reflected exactly in `Access-Control-Allow-Origin`.

`Paths` selects a complete replacement policy for a path prefix; the longest
matching prefix wins and other requests use the enclosing policy. A prefix is a
clean absolute path without a trailing slash and matches at a segment boundary
(`/public` matches `/public` and `/public/x`, not `/publicity`). Path policies
cannot nest further `Paths`:

```go
foundryhttp.CORSConfig{
    Origins: []foundryhttp.Origin{"https://console.example.com"},
    Paths: []foundryhttp.CORSPath{
        {Prefix: "/public", Policy: foundryhttp.CORSConfig{AnyOrigin: true, AnyMethod: true}},
    },
}
```

`AnyOrigin` returns `*` and cannot combine with `Origins` or `Credentials`.
`AnyMethod` permits supported framework methods, and `AnyHeaders` reflects
validated requested names, including Authorization. Their explicit-list
counterparts must be empty when these flags are set. Exposed headers always use
explicit names. Methods apply to preflight decisions; ordinary HTTP method
routing remains owned by the router. Every permitted method, including HEAD,
must be declared or covered by `AnyMethod`.

Accepted preflights return 204 without invoking the endpoint. The Origin header
is browser-owned metadata: an unlisted, unknown-scheme, repeated or malformed
Origin is simply not allowed. Ordinary requests with such an origin still
execute, with no sharing headers; a preflight from it returns 403. A preflight
from an allowed origin with a malformed requested method or header list returns
the shared 400 envelope, and a disallowed method or header returns 403. Missing
Origin also passes through, and OPTIONS without Origin or without a requested
method is left to routing. Authentication and session/CSRF policy remain
separate from CORS.

Existing `Vary` values are preserved and duplicates avoided. Responses vary on
Origin, including requests without it. OPTIONS responses additionally vary on the requested
method and headers, including ordinary OPTIONS. `MaxAge` is an integer number of
seconds from zero through 24 hours; zero sends an explicit zero cache duration.
Preflight output and ordinary response headers are kept separate.

Declaration lists are bounded to 256 items and path policies to 64. An origin is limited to 2,048 bytes;
header names to 256 bytes; requested header lists to 8,192 bytes and 256 entries
before deduplication. Invalid bytes are rejected before header canonicalization.
The policy shares immutable state across requests and preserves the original
ResponseWriter, including streaming/upgrade capabilities. Keep one authoritative
CORS policy in the chain; custom handlers and proxies remain responsible for
any response headers they modify afterward.

The [Fetch Standard](https://fetch.spec.whatwg.org/#http-cors-protocol) owns browser
CORS behavior. Foundry's bounded declarations and explicit middleware ordering
are framework choices; client contract generation does not infer authentication
from these sharing settings. The [HTTP blueprint](../../blueprint/08-http-validation-and-responses.md)
records the delivered transport policies and milestone acceptance.
