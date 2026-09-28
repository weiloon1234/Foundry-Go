# Protected production diagnostics

Protected diagnostics passed native lifecycle, authorization, bounded-probe and
consumer acceptance. See [production acceptance](../production-acceptance.md).

`diagnostics.Runtime` borrows the application's recorder and an explicit
`health.Registry`. It starts no listener, profiler or background task and does
not dump configuration, credentials, SQL or arbitrary error messages.

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

All responses use `Cache-Control: no-store`. Dependency error payloads never enter
readiness output. A transition into maintenance/shutdown during a probe check
makes the final readiness result false. No dependency checks run while prepared,
starting, paused or stopping. Hosts that deliberately need public minimal health
signals can expose selected report booleans through their own explicit public
route; the diagnostics route constructor does not create one implicitly.

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
