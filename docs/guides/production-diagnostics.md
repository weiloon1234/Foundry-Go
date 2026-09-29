# Protected production diagnostics

Protected diagnostics passed native lifecycle, authorization, bounded-probe and
consumer acceptance. See [production acceptance](../production-acceptance.md).

`diagnostics.Runtime` borrows the application's recorder, its maintenance gate and
an explicit `health.Registry`. It starts no listener or background task and does
not dump configuration, credentials, SQL or arbitrary error messages. Readiness and
status report the application gate's mode even when observability is disabled.

For an already built app, call `diagnostics.New(app, probes, config)`. During
normal application assembly, register `health.Module`, then `diagnostics.Module`
with the readiness service key and its provider in `requires`. Resolve the
diagnostics service in the ordinary HTTP handler factory. The module binds the
actual application lifecycle during boot; a view resolved during Build reports
`Prepared`. See the [consumer composition](../../tests/fixtures/consumer/tooling/production.go).

`diagnostics.Route` accepts only a concrete authenticated GET route with no path
parameters. Apply `WithPermissions` and any required token scopes using the normal
typed authentication API before binding. Anonymous/public/optional-auth route
types cannot be substituted. Authentication and permission checks run before any
readiness probe or diagnostics capacity acquisition. HEAD uses the same policy
and status while omitting the body.

| Endpoint | Behavior |
| --- | --- |
| Liveness | Reads lifecycle state only; dependency failure and maintenance do not fail it. A stopped app returns 503. |
| Readiness | Requires a running app, serving maintenance gate and successful configured probes; otherwise returns 503. |
| Status | Returns lifecycle state and the bounded observation snapshot. |
| Metrics | Writes the recorder's Prometheus text snapshot. |
| Profile | Opt-in `runtime/pprof` capture; see below. |

All responses use `Cache-Control: no-store`. Dependency error payloads never enter
readiness output. A transition into maintenance/shutdown during a probe check
makes the final readiness result false. No dependency checks run while prepared,
starting, paused or stopping. `diagnostics.Route` never creates a public route.

## Public probes

`diagnostics.PublicProbe(runtime, id, path, endpoint, options...)` registers an
unauthenticated GET route for load balancers and orchestrators, for the Liveness
and Readiness endpoints only. Liveness performs no dependency checks. Readiness
applies the lifecycle, maintenance and dependency rules above, but because the
route is public it never runs dependency probes per request: lifecycle and
maintenance state are read live, and dependency results come from one check
reused for `diagnostics.ReadinessCache(ttl)` (default one second, 100ms to one
minute). The request that finds the result expired runs the check; others
meanwhile receive the previous answer, so a flood cannot multiply dependency I/O
or pull the instance from rotation. Public probes do not use the diagnostics
operation slots of authenticated routes. Bodies are only
`{"status":"up"}`/`{"status":"down"}` or `{"status":"ready"}`/`{"status":"unavailable"}`
with 200/503; no states, probe IDs or errors are exposed. Configured assembly
mounts them with `http.probes.liveness = true` (path `/up`) and
`http.probes.readiness = true` (path `/ready`, cached for
`http.probes.readiness_cache`); both paths are exempt from a paused gate, so a
paused instance stays live while reporting unavailable. Readiness fails during the lame-duck stop
delay because the application is no longer running.

## Profiling

The Profile endpoint serves `runtime/pprof` data through an authenticated
`diagnostics.Route`. It is disabled unless `diagnostics.Config.Profiling` is true;
binding it otherwise fails route registration. `net/http/pprof` is not imported,
so nothing is registered on `http.DefaultServeMux`. Query parameters: `name`
(`heap` by default, `goroutine`, `allocs`, `block`, `mutex`, `threadcreate`, `cpu`
or `trace`), `seconds` (1-20, default 5, for `cpu`/`trace`) and `debug=1` for the
text form of named profiles. Unknown or repeated parameters return 400. One
profile runs at a time per runtime; another request, or a process-wide profiler
already in use, returns 503 with `Retry-After`. Capture holds a diagnostics slot
for its duration, so keep `seconds` below the route's request timeout.

Diagnostics callbacks borrow dependencies, so those dependencies must remain alive
until native HTTP handler ownership drains. The readiness module additionally
owns context-ignoring probe callbacks after a timed-out readiness request returns.
The diagnostics HTTP adapter allows 8 simultaneous operations by default, with a
configurable maximum of 64. Exhaustion returns 503 without blocking. Probe slots,
recorder series/queues and HTTP connection/request ceilings have separate bounds.

## Access during maintenance

Add protected diagnostics URLs to `ServerConfig.MaintenanceReadPaths` so operators
can inspect a paused application through its existing listener. Derive the URLs
from the same route descriptors used for registration. Up to 16 exact canonical,
unescaped static paths are allowed. This exception accepts only GET/HEAD and
continues to enforce auth, policies, body/header limits and request capacity.
It does not permit writes or override actual server shutdown. Prefixes, encoded
aliases and caller-controlled headers do not create an exception.

Readiness and liveness answer different questions: a database outage should remove
an instance from serving traffic without continually restarting a healthy process.
Inspect explicit dependency diagnostics under the same operational authorization
when deciding whether a failed readiness dependency needs repair.

See [readiness and maintenance](readiness-and-maintenance.md) and
[shared observations](observability.md) for callback ownership and export budgets.
