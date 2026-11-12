# Typed Content Security Policy

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

## Configure an application allowlist

`application.HTTPSettings.CSPAllowlist` contains an `http.CSPAllowlist`. The
framework default is empty. Applications opt in through config or code defaults
and pass the loaded allowlist to `http.ApplyCSPAllowlist` when composing their
policy. `SecurityHeaders` does not install CSP or apply the allowlist automatically.

For settings declared as `App application.Settings`, enable Cloudflare Web
Analytics in TOML:

```toml
[app.http.csp_allowlist]
presets = ["cloudflare-web-analytics"]
script_origins = []
connect_origins = []
image_origins = ["https://images.example.test"]
frame_origins = []
```

The generated keys are `app.http.csp_allowlist.presets`, `script_origins`,
`connect_origins`, `image_origins`, and `frame_origins` under the same prefix.
With an environment prefix of `PROJECT`, set
`PROJECT__APP__HTTP__CSP_ALLOWLIST__PRESETS='["cloudflare-web-analytics"]'`.
Collection environment values are JSON arrays. For `application.Settings`
directly, omit `app.` from keys and `__APP` from environment names. An empty
configured array clears the corresponding code default.

The equivalent code default is:

```go
settings.App.HTTP.CSPAllowlist.Presets = []foundryhttp.CSPPreset{
	foundryhttp.CSPPresetCloudflareWebAnalytics,
}
```

Apply the loaded settings before constructing the middleware:

```go
policy, err := foundryhttp.ApplyCSPAllowlist(foundryhttp.CSPPolicy{
	DefaultSrc: []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
	ScriptSrc:  []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
	ConnectSrc: []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
	ImgSrc:     []foundryhttp.CSPSource{foundryhttp.CSPSelf()},
	FrameSrc:   []foundryhttp.CSPSource{foundryhttp.CSPNone()},
}, settings.App.HTTP.CSPAllowlist)
if err != nil {
	return nil, err
}
middleware := foundryhttp.ContentSecurityPolicy(foundryhttp.CSPConfig{
	Enforce: value.Set(policy),
})
```

`CSPPresetCloudflareWebAnalytics` allows `https://static.cloudflareinsights.com`
in `script-src` and `https://cloudflareinsights.com` in `connect-src`, matching
[Cloudflare's CSP requirements](https://developers.cloudflare.com/fundamentals/reference/policies-compliances/content-security-policies/).
These origin-wide permissions cover the injected `/beacon.min.js/...` script
and `/cdn-cgi/rum` requests. The preset does not enable analytics injection,
permit inline scripts, or include other Cloudflare products.

The four origin fields accept `[]http.Origin` and add hosts to `script-src`,
`connect-src`, `img-src`, and `frame-src`, respectively. Validation reuses
`ParseOrigin` and CSP host validation. Only exact HTTPS origins are accepted.
Paths, credentials, queries, fragments, wildcards, HTTP, `data:`, `blob:`,
`'unsafe-*'`, and IPv6 literals are rejected. Host casing and default ports are
normalized for additions and duplicate detection. Unknown presets fail validation.
`application.New` validates the allowlist through its settings load before
resources are acquired, even when HTTP is disabled. Standalone schema loads only
decode the fields; direct composition validates through `ApplyCSPAllowlist`, or
call `CSPAllowlist.Validate` explicitly.

`ApplyCSPAllowlist(policy, allowlist)` returns `(CSPPolicy, error)` and copies every
policy slice. The input must be a valid policy. Presets expand before custom
origins, and repeated additions are ignored, including hosts already present.
The existing policy source and header bounds still apply. If an addition needs a
nil target directive, the function returns an error instead of changing browser fallback.
An explicitly empty directive or a sole `CSPNone()` becomes only the allowed
hosts. With no additions, nil, empty, and `'none'` keep their original meaning.

When script origins are added and both `WorkerSrc` and `ChildSrc` are nil, the
helper pins `WorkerSrc` to a separate copy of the original `ScriptSrc`, including
nonce sources. This preserves the effective worker policy instead of letting
workers inherit the added script hosts. Explicit `WorkerSrc` or `ChildSrc`,
including empty slices, remain unchanged. If `ChildSrc` is present and `WorkerSrc`
is nil, `WorkerSrc` stays nil. CSP compilation accepts nonce sources in `WorkerSrc`
so a nonce-only script policy can be pinned without a validation error.

Frame additions require an explicit `FrameSrc`; they do not change `ChildSrc`.
Neither `ImgSrc` nor `ConnectSrc` has dependent directives in the
[CSP fallback list](https://www.w3.org/TR/CSP3/#directive-fallback-list).
The remaining declarations are unchanged. An explicit `ScriptSrcElem` still controls
script elements, and a nonce policy with `CSPStrictDynamic()` can cause browsers
to ignore host allowlists. An omitted `ScriptSrcElem` intentionally inherits the
expanded script sources. Use the explicit self-based policy above for the
automatically injected beacon. The framework's maintenance page retains its
strict, separate CSP. A beacon injected there remains blocked, which is harmless.

When adopting this API, regenerate application settings, pass the loaded
`CSPAllowlist` into the application's CSP middleware factory, and apply it before
`ContentSecurityPolicy`. Move custom image origins into `ImageOrigins`.
The [configured consumer](../../tests/fixtures/consumer/httpsecurity/csp_allowlist.go)
demonstrates the complete composition.

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
