# Application bootstrap

C03 passed its [native acceptance gate](../evidence/consumer-startup-c03.json).
C04 supporting services and [C05 final acceptance](consumer-startup-acceptance.md) also passed.
A [compact runnable example](consumer-startup-acceptance.md) needs no external service.
`application.New(settings)` assembles the configured providers, router and selected
HTTP kernel. The existing `foundry.New().Register(...).Build(...)` remains the
narrow advanced entry point.

The independent [executable consumer](../../tests/fixtures/consumer/bootstrap/cmd/serve/main.go)
loads concrete settings and runs this ordinary path:

```go
settings, err := bootstrap.Load(configPath)
if err != nil { return err }
app, err := bootstrap.Build(ctx, settings)
if err != nil { return err }
return app.Run(ctx, foundation.HTTP)
```

A signal cancels `ctx`; `Run` then drains within `Settings.ShutdownTimeout` and
returns nil for a clean graceful stop, so the executable needs no second
`Shutdown` call. `ShutdownTimeout` (default 25s) is the whole budget: the optional
`StopDelay` lame-duck period (readiness fails while listeners keep serving), the
HTTP `Server.ShutdownTimeout` grace (default 10s) and every cleanup share it.
Build rejects a budget where `StopDelay` plus an enabled listener's grace (plus the
WebSocket drain for a dedicated realtime listener) is not shorter than
`ShutdownTimeout`. `StartupTimeout` optionally bounds provider boot. Lifecycle
events (starting, ready, shutdown started, cleanup failures, stopped) are logged
through the default logger with redacted diagnostics.

Its [Build function](../../tests/fixtures/consumer/bootstrap/settings.go) calls
`application.New(settings.App).HTTP(...)`. The HTTP callback constructs domain
handlers from `application.Services`; it contains no pool, router/server, adapter
factory, SDK import or shutdown registration. Additional `.HTTP` callbacks add
route declarations; duplicate/ambiguous routes fail together. `.Register` and
`.RegisterPlugin` retain the existing extension graph. Plugins may contribute
routes through `application.RouterKey`; ordinary and contributed routes use one
router and the same validation. `.SPA(id, assetsKey, config)` adds a browser
portal's client-route fallback to that router; see
[SPA fallbacks with application.New](http-assets.md#spa-fallbacks-with-applicationnew).

## Configuration and dependencies

`Settings.TimeZone` defaults to `temporal.UTC`. The [application timezone guide](application-timezone.md)
covers `s.Time()`, `s.Calendar()`, log timestamps/rollover and export defaults.

Handwritten Go settings are the type/default source. `//foundry:config` generates
schemas and concrete override keys. TOML/environment provide deployment values;
loading is explicit through the existing `config/toml.LoadFile` or schema loader.
No Go file is executed from a runtime config path, and no model path is scanned.
`bootstrap.Load` demonstrates optional TOML, explicit `os.LookupEnv`, an environment
prefix and generated typed overrides. Configuration is copied when the builder
is created. Build validates without opening a listener, file or database pool.
Run/Start uses the existing reverse-order application lifecycle; migrations remain
explicit. `app.Migrations()` exposes configured cache and enabled feature contributions,
followed by the application's own `Builder.Migrations` targets, for tooling.
`app.RunDatabaseCommand` runs one parsed `migrate`, `seed` or `prune` command against
the selected database without starting the application; see
[application migration targets](migrations-and-seeding.md#application-migration-targets).

`services.Database()`, `.Cache()`, `.Disk()` and `.RedisConnection()` resolve their
configured defaults. Named alternatives remain under `.Databases`, `.Caches`,
`.Storage` and `.Redis`. `.Image()` returns the configured default image engine;
`Image.Enabled` controls its ownership. `application.Resolve(services, domainKey)`
is the explicit escape hatch for a registered domain service. Custom application
settings remain concrete constructor arguments, as `bootstrap.Routes` demonstrates.

Operational features (probes, fleet maintenance, housekeeping schedules, sticky
reads, the failed-job archive, queued listeners, MFA and encryption keys, metrics
and logging sinks) are enabled from settings alone; see the
[configured operational features](supporting-services.md#configured-operational-features)
reference.

Handlers receive `context.Context` for cancellation and attribution, their typed
input, and their concrete authenticated model. Shared config/services enter the
constructor. There is no mutable `ctx.global` or dynamic `ctx.session.actor` bag.

## Request policies and typed actors

`HTTP.Server` reuses the existing timeouts, body, request and connection limits.
The standard security headers are on by default. `.Use` wraps the whole router,
including 404/405 responses; custom middleware runs in declaration order. The
server establishes request identity, observation, admission and body/context
limits outside that chain. When a database connection with a read pool sets
`sticky_read_window`, `database.StickyReadsHandler` wraps the whole chain so each
request reads its own writes. Default security headers precede custom middleware;
put an explicitly configured trusted-proxy policy before CORS/browser policies
when operating behind a trusted edge. No forwarding headers are trusted by default.

Bind a model-specific default once per route group:

```go
members, err := http.BindGuard(cookieAuthentication, memberGuard)
if err != nil { return err }
route := http.Authenticated(profileEndpoint, members).Handle(
    func(ctx context.Context, actor Member, input ProfileInput) (Profile, error) {
        return service.Profile(ctx, actor, input)
    },
)
```

Another actor model gets its own `GuardBinding[Operator]`. `members.Select(guard)`
returns an alternative binding of the same model without mutating the default.
The compiler rejects another actor type. Existing access-scope and resource
permission APIs remain separate from authentication.

`http.NewCookieAuthentication(registry, csrfConfig, sources...)` protects every
selected cookie guard, including routes named `/api/...`. Existing browser-session
adapters already enforce CSRF and cookie ownership. Ordinary application assembly
rejects cookie routes lacking that protection or explicit CSRF middleware.
Bearer-only `http.NewAuthentication` needs no browser-session state or Origin on
unsafe calls. CORS does not grant CSRF trust. The executable fixture's explicit
static credential verifier is acceptance-only; use persistent token/session
services for production authentication.

## Realtime assembly

`Realtime.Enabled` constructs the hub from `.Realtime(...)` declarations. By default
it owns a dedicated listener (`Realtime.HTTP`) and the WebSocket kernel. Set
`Realtime.Shared` to serve upgrades at `Realtime.Path` on the application HTTP
listener instead: boot confirms the distributed subscription, `foundation.HTTP`
serves both, `App.RealtimeReady` reports the HTTP address, and sockets drain when
the application lifetime ends. Sockets then hold HTTP request/connection capacity
for their lifetime, so size `HTTP.Server.MaxConcurrentRequests`/`MaxConnections`
accordingly. `App.RunKernels(ctx, foundation.HTTP, foundation.Worker)` adds a
worker to the same process.

`Services.RealtimePublisher()` returns a `websocket.PublisherSource` for HTTP
handlers and jobs: the hub in a realtime process, or, with `Realtime.Publisher`
enabled (and `Realtime.Enabled` off, for example in a worker), a managed
cross-process publisher built from the same declarations and cluster connection
and closed at shutdown. A local realtime connection cannot publish across processes.

## Access logging and completion hooks

`HTTP.Server.AccessLog` defaults to true in application settings. `Log.Default`
selects one of `Log.Channels`; leaf settings select `logging.Stderr`,
`logging.Stdout` or `logging.File`, level and source annotation. Named stacks and
supporting services are described in [the C04 guide](supporting-services.md).
File paths are absolute; the parent must already exist. Startup opens an append
file with private permissions for a new file. Build never touches it; application
shutdown closes it after borrowers. File sinks provide [automatic rotation and
retention](logging.md), with daily/20 MiB rollover and 14-file/14-day retention.
`application.WithLogger` borrows the default logger; other configured channels
retain their owned lifecycle.
No process-global logger changes.

Automatic events contain request ID, declared route ID, bounded method, status,
outcome, duration, response byte count and HTTP hijack status. They exclude raw
URLs/queries, headers, bodies, credentials and error payloads. An empty route ID
means no declaration matched. HTTP hijack completion describes handoff, not the
later WebSocket connection lifetime.

`.ObserveHTTP(http.RequestObserverFunc(...))` receives the same typed completion
snapshot, including early rejection. It runs synchronously within request
ownership; cancellation cannot release dependencies before it returns. Its context
may already be cancelled. Panics/Goexit are isolated and later observers still
run. Keep callbacks bounded; use middleware for request behavior and existing
provider lifecycle hooks for startup/shutdown. Requests arriving after shutdown
has sealed handler ownership are rejected without invoking completion extensions.

The [consumer test](../../tests/fixtures/consumer/bootstrap/bootstrap_test.go)
exercises two actor models over native HTTP, explicit PostgreSQL tables, default
cache, multipart upload, an image plan and the configured storage disk. These are
framework acceptance fixtures, not a generated starter application.

## Measured build cost

The native [C03 measurement](../evidence/consumer-startup-c03-cost.json) compared
independent copies with empty build caches, existing dependencies and downloads
disabled. One cold build measured 3.31s for the accepted smaller query consumer
and 9.97s for the configured executable. Unchanged builds measured 0.24s for both;
a small main-package edit measured 0.24s and 1.41s respectively. Imported package
counts were 159 and 502; linked binary sizes were about 5.1MB and 35.3MB.

These are different capability profiles, not a framework-overhead isolation
benchmark. Configured assembly imports its supported database, cloud, image and
HTTP implementations even when deployment settings disable them. Runtime-disabled
features acquire no resources, but still contribute to Go compilation. Keep using
narrow feature imports and the advanced builder when that build-size tradeoff
matters. These samples do not establish an SLA; power/background load was not
controlled. [C05 packaged-consumer and runtime results](developer-resource-measurements.md#consumer-startup-c05-native-results) now provide the final evidence.
