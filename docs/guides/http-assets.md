# Static assets and SPA routing

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

## SPA fallbacks with application.New

`application.New` builds the router itself, so declare SPAs on the builder with
the same `SPAConfig`, naming the `*Assets` key an `AssetsModule` provides:

```go
admin := foundryhttp.DefaultSPAConfig()
admin.Prefix, admin.Exclude = "/admin", []string{"/admin/api"}
app, err := application.New(settings).
    Register(foundryhttp.AssetsModule("web.admin", AdminAssets, adminConfig)).
    HTTP(routes). // root public mount, /admin/assets bundles, API endpoints
    SPA("portal.admin", AdminAssets, admin).
    Build(ctx)
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
