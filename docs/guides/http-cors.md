# Browser response sharing

**Full verification passed.** Complete repository acceptance supplements focused
runtime/consumer races, bounded fuzzing and compiler/editor checks.

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
Allowlist entries match complete origins, with no suffix or subdomain expansion.
`NullOrigin` explicitly permits the browser's opaque-origin serialization; its
meaning is broader than one particular website.

`AnyOrigin` returns `*` and cannot combine with `Origins` or `Credentials`.
`AnyMethod` permits supported framework methods, and `AnyHeaders` reflects
validated requested names, including Authorization. Their explicit-list
counterparts must be empty when these flags are set. Exposed headers always use
explicit names. Methods apply to preflight decisions; ordinary HTTP method
routing remains owned by the router. Every permitted method, including HEAD,
must be declared or covered by `AnyMethod`.

Accepted preflights return 204 without invoking the endpoint. Malformed requests
return the shared 400 envelope; disallowed preflight origins, methods or headers
return 403. Ordinary requests with a valid but disallowed origin still execute,
with no sharing headers. Missing Origin also passes through; ordinary OPTIONS
without a requested method is left to routing. Authentication and session/CSRF
policy remain separate from CORS.

Existing `Vary` values are preserved and duplicates avoided. Responses vary on
Origin, including requests without it and wildcard policies whose malformed
input can be rejected. OPTIONS responses additionally vary on the requested
method and headers, including ordinary OPTIONS. `MaxAge` is an integer number of
seconds from zero through 24 hours; zero sends an explicit zero cache duration.
Preflight output and ordinary response headers are kept separate.

Declaration lists are bounded to 256 items. An origin is limited to 2,048 bytes;
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
