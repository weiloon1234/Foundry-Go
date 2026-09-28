# Typed Content Security Policy

**Full verification passed.** Complete repository acceptance supplements focused
runtime/consumer races, source-parser fuzzing and compiler/editor checks.

Use `http.ContentSecurityPolicy` alongside [security headers](http-security-headers.md).
It compiles explicit Go declarations at assembly and preserves native HTTP
writers. Apply it around the router to cover fallback responses too.

```go
policy := foundryhttp.CSPPolicy{
    DefaultSrc:     []foundryhttp.CSPSource{foundryhttp.CSPNone()},
    ScriptSrc:      []foundryhttp.CSPSource{foundryhttp.CSPNonceSource(), foundryhttp.CSPStrictDynamic()},
    ImgSrc:         []foundryhttp.CSPSource{foundryhttp.CSPSelf(), foundryhttp.CSPSchemeSource("data")},
    ConnectSrc:     []foundryhttp.CSPSource{foundryhttp.CSPHostSource("wss://events.example.test")},
    BaseURI:        []foundryhttp.CSPSource{foundryhttp.CSPNone()},
    FrameAncestors: []foundryhttp.CSPSource{foundryhttp.CSPNone()},
}
middleware := foundryhttp.ContentSecurityPolicy(foundryhttp.CSPConfig{
    Enforce: value.Set(policy),
})
```

`CSPConfig.ReportOnly` accepts an independent policy for monitoring. Neither policy
is implicit. A zero configuration adds no CSP headers; an explicitly present but
empty policy is an assembly error. A nil source slice omits that directive, while
an explicitly empty slice emits `'none'`. Browser fallback between directives
remains browser behavior; declarations do not copy `DefaultSrc` into other fields.

Sources have a distinct Go type. Use `CSPSelf`, `CSPNone`, `CSPAny`,
`CSPSchemeSource`, `CSPHostSource`, the explicit keyword constructors, or a hash
constructor. `CSPSHA256`, `CSPSHA384`, and `CSPSHA512` accept fixed-size digest
arrays, so mixing digest lengths fails compilation. Hash the exact script/style
bytes, including their whitespace. Host patterns support wildcard subdomains,
optional schemes/ports and encoded paths. Use ASCII/punycode hostnames; IPv6
literals are outside CSP's host-source grammar. Host strings cannot inject an
additional directive or keyword.

`Sandbox` is a slice of `CSPSandboxToken`: nil omits it, while an empty slice
enables the full sandbox. Permissions are explicit typed constants. Sandbox and
`UpgradeInsecureRequests` reject report-only placement. `ReportTo` names a
separately configured Reporting-Endpoints group; `ReportURI` declares legacy
HTTP(S) destinations or absolute local paths. The framework does not register a
report receiver automatically. Configure and protect that endpoint explicitly.

## Rendering with a nonce

`CSPNonceSource()` requests a nonce for script/style element directives. The
middleware generates 32 cryptographically random bytes per request and exposes
the base64 value through a concrete context accessor:

```go
nonce, ok := foundryhttp.CSPNonceFromContext(request.Context())
if !ok {
    // This handler requires a configured nonce policy.
    return
}
data := struct{ Nonce string }{Nonce: nonce.String()}
// Pass data to a native html/template containing nonce="{{.Nonce}}".
```

The [consumer example](../../tests/fixtures/consumer/httpsecurity/csp.go) includes
the complete native handler and template. One request shares its nonce across
script/style directives, enforced/report-only headers and nested CSP wrappers.
Separate requests receive new values. Foundry does not accept static configured
nonces, rewrite HTML, mark strings as trusted markup, or bless untrusted content.

Nonce responses receive `Cache-Control: no-store`. Keep the rendered page and its
policy together; do not cache a generated nonce or reuse a captured request
context for another response. Static policies preserve existing cache headers.
These middleware headers are applied before downstream execution: trusted native
code can deliberately replace them, and no committed response is rewritten.
Use one composed policy configuration for each response wherever possible.

Policies permit at most 256 sources, 2,048 bytes per source, and 8,192 bytes per
rendered policy header, including nonce expansion. Sandbox/report declarations
also have bounds. Duplicate sources/tokens/destinations, mixed `'none'`, malformed
dynamic inputs and unsupported nonce placement fail assembly. Configuration is
copied, so later edits to the declaration slices cannot change a live handler.

The [CSP specification](https://www.w3.org/TR/CSP3/) defines browser syntax,
directive behavior, reporting and nonce requirements. Browser support remains a
runtime compatibility boundary; Go typing cannot prove a browser will enforce a
policy. Native custom-header extensions remain explicit for other directives.
