# Static assets and SPA routing

<a id="agent-entry"></a>
## Agent entry

- [Serve assets and client navigation](#recipe-serve-assets)
- [Declare a configured SPA](#recipe-declare-spa)
- [Inject public runtime data](#recipe-spa-runtime)
- [Wrap a development proxy](#recipe-runtime-proxy)

<a id="recipe-serve-assets"></a>
## Serve static assets and SPA navigation

<!-- compiled-source: tests/fixtures/consumer/httpassets/assets.go#asset-lifecycle -->
```go
func Build(ctx context.Context, source foundryhttp.AssetSource) (*foundation.App, error) {
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	return foundry.New(foundation.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Register(
		foundryhttp.AssetsModule("assets", AssetsKey, foundryhttp.DefaultAssetsConfig(source)),
		foundryhttp.Module("http", ServerKey, config, func(resolver foundation.Resolver) (stdhttp.Handler, error) {
			assets, err := foundation.Resolve(resolver, AssetsKey)
			if err != nil {
				return nil, err
			}
			mount := assets.Mount("web.assets", "/assets").WithNavigationFallback(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(stdhttp.StatusNotFound)
				if r.Method != stdhttp.MethodHead {
					_, _ = io.WriteString(w, "<main>Page not found</main>")
				}
			}))
			health := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "api.health", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/api/health")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
			router, err := foundryhttp.NewRouter(mount.Register(), health.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
				return foundryhttp.NoContent{}, nil
			}))
			if err != nil {
				return nil, err
			}
			spa := foundryhttp.DefaultSPAConfig()
			spa.Exclude = []string{"/api", "/assets"}
			return router.WithSPA("web.spa", assets, spa)
		}),
	).Build(ctx)
}
```

Foundry owns static-directory startup and shutdown through `AssetsModule`.
Declare `DirectoryAssets(path)` or `FilesystemAssets(fsys)` in
`DefaultAssetsConfig`, resolve the typed `*Assets` service while constructing
HTTP routes, and register `assets.Mount(routeID, prefix).Register()`.
`AssetMount.URL(AssetPath)` uses that same prefix for escaped file/directory URLs.
The [independent consumer](../../tests/fixtures/consumer/httpassets/assets.go)
shows complete application assembly with embedded or local assets; it is a
framework fixture, not a starter project.

`AssetsModule` opens the local `os.Root` only during provider boot and closes it
after managed kernels and requests exit. Build validates configuration without
opening directories. `OpenAssets(ctx, config)` and `Close(ctx)` support explicit
standalone use. Close stops new lookups and waits for acquired files; if its
waiting context is canceled, the last owner still finishes closure and a later
Close observes the result. Consumers do not manage directory handles themselves.

Local roots confine path and symlink resolution. A caller-provided filesystem,
including `embed.FS`, keeps its own confinement and concurrency contract; Foundry
does not invent a symlink guarantee for arbitrary `fs.FS`. The caller owns that
filesystem while Foundry closes files it opens. Files must support seeking;
unsupported custom filesystems fail without whole-file buffering. Error matching
runs in an owned callback with [bounded traversal](http-requests.md#typed-public-errors).
Cyclic chains, panic and Goexit cannot retain a completed lookup/open; unknown
failures remain server failures instead of triggering SPA navigation fallback.

Mounts declare public assets. Domain-authorized file responses can use
`Assets.Download` inside an ordinary typed endpoint. Mounts serve GET and HEAD through the shared download machinery, including
ranges, modification-time conditions, bounded transfer and safe error responses.
No directory listing is exposed. A configured index serves directories, with
relative redirects preserving query strings and never trusting an incoming host.
Hidden path components are denied by default, except the first `.well-known`
component; `AllowHidden` is an explicit source policy. Invalid structural paths
and backslashes are rejected. Unknown extensions use application/octet-stream.
Media mappings are typed, bounded and snapshotted; host MIME files and global
MIME registration do not alter the declaration.

Use `router.WithSPA(routeID, assets, config)` with `DefaultSPAConfig()` for a
separate fallback. It returns an independent router view and only runs after a
native unmatched 404. Existing API handler errors and native method errors
remain unchanged. Configure absolute `Exclude` prefixes for API namespaces.
Existing assets serve normally, but a missing extensionless path falls back to
the configured entry only when Accept explicitly permits text/html. Missing
JavaScript/CSS/images and hidden files retain shared errors. Missing paths
also retain errors when HTML is not accepted or their namespace is excluded. HTML fallback responses vary on Accept. A prefix such as
`/app` redirects its root to `/app/` so relative frontend assets resolve correctly.
Multiple SPAs can use distinct prefixes on the same router. The most specific
prefix wins; an excluded API or missing asset cannot fall into an outer SPA.
Duplicate prefixes and route IDs are rejected. A SPA fallback already serves
existing files under its prefix, so it needs no asset mount of its own.

SPAs compose with asset mounts by prefix. Declared routes and more specific
mounts, such as `/admin/assets` for content-hashed bundles with an immutable
`CacheControl`, keep precedence: their misses stay 404. A SPA whose prefix is
more specific than a matching mount owns its subtree below it, so a root
`public/` mount at `/` does not answer `/admin/login` under an `/admin` SPA, and
what that SPA declines (a missing script, an excluded path) is 404 rather than a
lookup in `public/`. A SPA with the same prefix as a mount answers the mount's
misses: a portal at `/` beside a root `public/` mount serves `robots.txt` from
`public/`, its own bundles from its build and its entry for client routes. A more
specific SPA answers outside the matching mount, as it answers an unmatched
request: the mount's route middleware, budget and route metadata do not apply,
and `MatchedRoute` reports the SPA. A SPA with the same prefix extends the mount,
so it answers the mount's misses inside that mount's route middleware; keep that
middleware free of assumptions about which directory answers.

For an application-rendered not-found page, register
`assets.Mount("web.public", "/").WithNavigationFallback(siteLost).Register()`,
where `siteLost` is a `net/http.Handler`. It runs inside the mount's middleware
only for a missing, allowed asset path that passes
`http.IsHTMLNavigation(request, name)`: GET/HEAD, an extensionless asset name,
and a bounded Accept header explicitly permitting `text/html` with positive
quality. Existing files, hidden paths, missing images/scripts, JSON requests
and lookup failures retain their existing behavior. A nil handler disables it;
the original mount value is unchanged.

The handler owns its status, body and headers, including cache policy and HEAD
body handling; the mount adds no asset cache headers. Declared routes and equal
or more specific SPAs retain precedence. A covering SPA's exclusions and misses
remain JSON 404s rather than falling through to this handler. Reserve API
namespaces through declared routes or SPA exclusions as before; this hook does
not define a new reserved-prefix list. The exported predicate is also the SPA's
single navigation rule; it does not itself check asset existence or exclusions.

Successful asset lookups are cached for one second (at most 4,096 entries), so a
replaced file in a directory source is observed within that window; misses are
never cached. Files that report no modification time, such as `embed.FS`
entries, receive a strong SHA-256 content `ETag` computed once per file and
reused (a size change recomputes it), so `If-None-Match` revalidation of an
embedded SPA shell returns 304. Directory assets transfer through the native
`sendfile` path described in [downloads](http-downloads.md).

A SPA entry must resolve to a regular file; a directory cannot cause repeated
redirects. Asset metadata includes file media, prefix, index and fallback
exclusions, without exposing local paths or fabricating a JSON DTO. Download
and stream response guides describe the shared transfer and cleanup behavior.

Runtime, lifecycle, independent-consumer, compiler and actual-gopls checks passed.
The [master acceptance record](../../blueprint/00-master-architecture-and-parity.md#milestone-08-static-assets-and-spa-focused-acceptance)
tracks verification and the remaining milestone scope.

<a id="recipe-declare-spa"></a>
## SPA fallbacks with application.New

`application.New` builds the router itself, so declare SPAs on the builder with
the same `SPAConfig`, naming the `*Assets` key an `AssetsModule` provides:

<!-- compiled-source: tests/fixtures/consumer/httpassets/runtime.go#configured-spa -->
```go
func DeclareSPA(builder *application.Builder, source foundryhttp.AssetSource, runtimeData foundryhttp.SPARuntime) *application.Builder {
	spa := foundryhttp.DefaultSPAConfig()
	spa.Prefix, spa.Exclude = "/admin", []string{"/admin/api"}
	spa.Runtime = runtimeData
	return builder.Register(foundryhttp.AssetsModule("web.admin", AssetsKey, foundryhttp.DefaultAssetsConfig(source))).
		SPA("portal.admin", AssetsKey, spa)
}
```

Build checks each declaration before any asset directory opens: an unknown
assets key, an invalid configuration, or a route ID or prefix that repeats
another SPA or a declared route fails Build. Route inspection lists each SPA with
its prefix, index and exclusions; contract export is unchanged because SPAs and
mounts are not operations. Applications assembling their own router from
contributions use `foundryhttp.RegisterSPA(registrar, routerKey, id, assetsKey,
config)`, which `Builder.SPA` uses too. The
[portal consumer](../../tests/fixtures/consumer/spaportals/portals.go) exercises
two portals, a root portal, hashed bundles and API routes with `application.New`.

<a id="recipe-spa-runtime"></a>
## Inject public runtime data

Use one `SPARuntime` descriptor for built documents and development proxies.
The application supplies its public DTO, generated `contract.JSON[T]` codec and
loader. The framework owns document reads, encoding, escaping and response headers.
Keep secrets and server configuration out of this DTO.

<!-- compiled-source: tests/fixtures/consumer/httpassets/runtime.go#portal-runtime-dto -->
```go
//foundry:dto
type PortalRuntime struct {
	SiteName string `json:"site_name"`
}
```

<!-- compiled-source: tests/fixtures/consumer/httpassets/runtime.go#portal-runtime -->
```go
func Runtime(siteName string) foundryhttp.SPARuntime {
	config := foundryhttp.DefaultSPARuntimeConfig()
	return foundryhttp.DefineSPARuntime(PortalRuntimeJSON(),
		func(ctx context.Context, request *stdhttp.Request) (PortalRuntime, error) {
			if err := ctx.Err(); err != nil {
				return PortalRuntime{}, err
			}
			return PortalRuntime{SiteName: siteName}, nil
		}, config)
}
```

Here `stdhttp.Request` is the standard library request. Bind service dependencies
before serving requests. The loader receives the original method, headers and
request context, including an existing CSP nonce. It must be concurrency-safe,
honor cancellation, and neither mutate nor retain the borrowed request. Register
the DTO in the application's contract export as before; this hook creates no API
endpoint and does not add the DTO to exports automatically.

`SPAConfig.Validate` validates an enabled descriptor. A zero `Runtime` retains the
existing static behavior. Configuration is copied by `DefineSPARuntime`; descriptor
copies share one admission limit. Construct separate descriptors for independent
applications.

### Document and transport policy

The hook applies to successful `text/html` files selected by the SPA, including
its entry and directory indexes. It reads the file directly under the asset
lease, without a response-capture writer or static content hashing. Redirects,
exclusions, explicit routes, asset-mount precedence and non-HTML files keep their
existing routing behavior. An equal asset mount still owns its hits; attach the
runtime descriptor to the SPA that actually serves the document.

The source must contain an explicit opening `head` element before document
content. The tokenizer supports case and attributes and ignores tags in comments
and script text. The hook inserts immediately after the opening tag, before
existing scripts, without reserializing the source. A missing head or an existing
element with the configured runtime ID is an error. Remove the old injection
middleware when adopting this hook.

The script uses `id="foundry-portal-runtime"` by default and always uses
`type="application/json"`. Set `SPARuntimeConfig.ElementID` to change the ID.
Encoding uses the declared contract codec. HTML escaping covers `<`, `>`, `&`,
`</script>`, `<!--`, U+2028 and U+2029; attributes are escaped separately.

Runtime documents always return a complete 200 representation, even with
conditional or range request headers. They carry `Cache-Control: no-store` and
a recomputed `Content-Length`, without static validators, range headers or stale
digests. HEAD executes the loader and computes the same representation headers
as GET, then omits the body. A request-dependent loader can deliberately return
different data for different methods; use the same DTO for GET and HEAD when
matching lengths matter.

Default bounds are 2 MiB of source HTML, 128 KiB of JSON, and 32 concurrent document
operations. `JSON.Bytes` applies both before and after HTML escaping. The other
JSON limits bound depth, nodes, validation steps and issues. Configuration permits
at most 32 MiB HTML, 1 MiB JSON and 1,024 concurrent operations. The final document
contains the bounded HTML plus escaped JSON and bounded tag attributes. Reading,
tokenizing and transforming can temporarily retain multiple copies within those
bounds; application allocations inside a loader remain application-owned.

Admission uses the shared framework semaphore with no wait. An exhausted limit,
loader or codec failure, panic, Goexit, canceled request, invalid document, or
exceeded bound takes the framework's safe 503 error path with `no-store`. Error
bodies and diagnostics do not expose arbitrary loader error text. No partially
transformed HTML is sent. A canceled wait never releases a still-running loader's
capacity; the hook waits for actual callback exit. Built responses retain capacity
through writing, and proxy responses retain it until their body closes. The proxy
adapter also closes the transformed body when downstream writing or a buffer-pool
callback panics or calls Goexit; downstream panics still propagate. On rejection,
upstream Close runs in callback isolation and finishes before admission is released.

<a id="recipe-runtime-proxy"></a>
## Wrap an existing development proxy

Keep the application's `httputil.ReverseProxy`, upstream URL and route exclusions.
Wrap the proxy once during route construction and propagate construction errors:

<!-- compiled-source: tests/fixtures/consumer/httpassets/runtime.go#runtime-proxy -->
```go
func DevelopmentProxy(proxy *httputil.ReverseProxy, runtimeData foundryhttp.SPARuntime, navigation func(*stdhttp.Request) bool) (stdhttp.Handler, error) {
	return runtimeData.WrapProxy(proxy, navigation)
}
```

The predicate selects possible documents, including direct HTML paths if the
application serves them.
Exclude APIs and assets that need their original conditional and range semantics.
The adapter calls the predicate only for GET and HEAD without an Upgrade header.
Unsafe methods and upgrades use the original proxy behavior. Predicate panic or
Goexit uses the safe 503 response.

For selected requests, the adapter clones the request, removes conditional and
range headers, requests `Accept-Encoding: identity`, and forwards HEAD as GET.
It invokes an existing `ModifyResponse` first. A successful HTML response is read
through an HTML-size-limited reader and transformed before final headers are sent.
This avoids a general-purpose response buffer and preserves non-HTML streaming.
Selected non-HTML responses still reflect the normalized upstream request; use
an accurate predicate to preserve asset revalidation and ranges. An upstream HTML or untyped 304
or 206 for a selected navigation fails safely rather than serving stale document
bytes. Responses explicitly typed as non-HTML retain their status. Encoded HTML and HTML with trailers also fail safely. Trailer checks run before
reading, at EOF, and after upstream Close, including trailers omitted from the
upstream declaration. Other statuses pass
through. Existing proxy error handlers retain upstream error behavior; injection
failures bypass them and use the framework's safe error response.

The adapter copies proxy configuration and borrows the transport. Do not mutate
the proxy's shared dependencies after wrapping it. It neither starts nor closes
the upstream server or transport.

## Preserve CSP and middleware behavior

Place CSP middleware outside the router or proxy adapter so the loader and renderer
see the request nonce. The hook uses `CSPNonceFromContext`; it validates the nonce
as bounded standard base64 and attribute-escapes it before placing it on the
runtime script. It does not create a nonce or change the CSP policy.

Set `runtimeConfig.StyleNonceCarrier = true` for client libraries that read a
style nonce from the DOM. When a nonce exists, the renderer inserts an empty
`<style nonce="…"></style>` before the runtime script. Without a nonce it emits
no carrier. This option is off by default.

Keep compression, ETags, cache and other route middleware in their existing chains.
Place response compression outside the proxy adapter. Foundry's automatic ETags
and compression respect `no-store`. The adapter preserves surrounding headers,
including CSP and cookies, and runs the proxy once. The built hook does not wrap
API responses, and the proxy adapter bypasses unsafe methods. Neither adds a
middleware declaration or changes idempotency metadata. Existing custom
middleware annotated with `PreservesIdempotentResponses()` retains that contract;
removing the old global portal-runtime wrapper needs no replacement attestation.

## Application integration

Keep portal inventories, browser bases, build commands, SSR ownership, public-file
policy and runtime allowlists in the application. Share the runtime descriptor
between its built SPA declaration and development proxy. Register the public DTO
in the application's contract export and supply its generated strict decoder to
the browser reader. This hook does not register a client contract automatically.
