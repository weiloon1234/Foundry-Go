# Readiness and maintenance

Readiness checks and maintenance admission share the application lifecycle.
The [master roadmap](../../blueprint/00-master-architecture-and-parity.md) records
acceptance and verification evidence.

`health.Registry` owns typed dependency probes. `NewRegistry` validates IDs,
duplicates and resource bounds without running checks. `Check(ctx)` returns a
report in declaration order, with `Up`, `Down`, `TimedOut`, `Cancelled`,
`CallbackFailed` or `Closed` states. A dependency error makes readiness false;
its message and unwrapped cause are absent from reports. A cancelled whole
check returns its context error and the result states gathered so far.

Probes within one check run concurrently, each under its own `ProbeTimeout` and
all under one `CheckTimeout`; results keep declaration order, so one slow
dependency no longer delays the others. `MaxConcurrent` bounds accepted callbacks
across every concurrent readiness request. A probe that ignores its timeout keeps
its slot until it actually exits. Other probes wait for a slot within their own
deadlines. `Close` seals admission; its context bounds the caller's wait. `Done`
closes only after all callbacks have exited.

`health.Module` resolves probes through the normal service graph. Declare the
probed providers in `requires` so readiness callbacks drain before those shared
resources close. It creates no separate server. Database
`ReadinessProbes(primaryID, readID)` supplies checks for the primary and configured
read endpoint. See the [consumer composition](../../tests/fixtures/consumer/tooling/routing.go).

`health/checks` adapts other services using only their public APIs:
`checks.Disk(id, disk, checks.DiskProbeKey())` stats one (normally absent) object
key through the disk's read path and never writes or lists. A missing key or a
403 counts as answered: without `s3:ListBucket`, S3 answers 403 for a missing
key, and a HEAD response carries no error code to tell that apart from rejected
credentials, so the probe proves the endpoint and bucket answer (a missing bucket
or network failure is Down) while credential failures surface through ordinary
operations. Grant `s3:GetObject` on the probe key plus `s3:ListBucket` (it may be
limited to the key's prefix) to receive 404s instead. `checks.Mailer(id,
mailer)` reports whether a mailer still admits submissions without contacting its
transport, because mail drivers expose submission only. The realtime hub's
`Hub.Probe` reports its local serving/cluster-stream state. Configured assembly
adds database/Redis probes and, when realtime is enabled, the `realtime` hub probe
with `features.health.configured_connections`; `configured_storage` and
`configured_mail` add disk and mailer probes.

Readiness reports describe dependency availability. Process liveness and the
application's startup/draining state are separate inputs to the final transport
response; a dependency outage alone must not trigger a process restart loop.

`maintenance.Gate` controls new work admission. Its zero value serves normally.
`Set(true)` pauses, `Set(false)` resumes, and `Drain()` permanently closes
admission. `Apply(state)` applies an operator `maintenance.State` (down flag,
Retry-After, public message, bypass secret digest, allowed networks and
exemption rules); `maintenance.New(policy)` adds configured exemptions and
networks. `Admit()` returns immediately for request transports; `Wait(ctx)`
allows workers to wait for resumed admission without polling. A gate does not
cancel existing work, dispose resources, create goroutines or bypass auth.
Every application owns exactly one gate, whether or not observability is enabled;
`foundation.WithMaintenance` supplies it, the runtime context carries it
(`maintenance.FromContext`), and a configured recorder shares it. Managed kernels
consult it. HTTP rejects new requests with
503; workers wait before reserving jobs; schedules stop admitting occurrences
without advancing their pending time, then apply their existing catch-up policy
on resume. CLI rejects execution before constructing a command handler unless the
command is declared with `AllowDuringMaintenance()` or invoked with the leading
`--during-maintenance` flag (never while draining). WebSockets
reject new upgrades and domain messages while allowing existing peers to
unsubscribe and release resources. These checks preserve admitted work and do not
retroactively revoke a reservation or active callback.

A `maintenance.Store` shares one State across a fleet. `maintenance/cachestore`
stores it in any configured cache store (Redis or PostgreSQL for several hosts;
the `null` driver is rejected because it retains nothing);
`maintenance.Watch` polls it into the local gate, which remains the only thing
admission reads. An absent record leaves the local gate unchanged, and a store
outage keeps the last applied state. `maintenance.Publish` saves and applies a
State in the issuing process. Exemptions, allowed networks and bypass cookies only
affect a paused gate: draining still rejects everything.

HTTP admission runs before the middleware chain. With
`http.WithAdmissionProxy(trustedProxy)` (configured applications pass their global
`TrustedProxy` automatically) allowed networks match the client address that
`TrustedProxy` resolves and the bypass cookie is `Secure` when the trusted public
scheme is https; without it they match the socket peer and native TLS, so behind
a load balancer an allowed private subnet would admit every client. A bypass
cookie is sealed with `Policy.Keys` (configured applications use their encryption
key ring) and bound to the secret's digest, so reading the shared store, which
holds only the digest, never lets anyone mint one, and every instance sharing the
keys accepts it. Without keys a cookie is signed with a key local to the issuing
process and is valid only there.

Application shutdown drains the gate before cancelling managed work. A manually
drained standalone scheduler admits nothing further; its owner still calls Stop.
Pausing does not suspend lease renewal, heartbeats, response consumption or other
infrastructure needed by already admitted work. HTTP diagnostics exceptions are
explicit GET/HEAD paths and preserve normal authorization. See
[protected diagnostics](production-diagnostics.md) and
[shared observations](observability.md).
