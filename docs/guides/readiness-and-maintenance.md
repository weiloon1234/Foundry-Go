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

Checks run in declaration order within one total deadline. `MaxConcurrent`
bounds accepted callbacks across every concurrent readiness request. A probe
that ignores its timeout keeps its slot until it actually exits. Later checks
wait within their own deadlines. `Close` seals admission; its context bounds
the caller's wait. `Done` closes only after all callbacks have exited.

`health.Module` resolves probes through the normal service graph. Declare the
probed providers in `requires` so readiness callbacks drain before those shared
resources close. It creates no separate server. Database
`ReadinessProbes(primaryID, readID)` supplies checks for the primary and configured
read endpoint. See the [consumer composition](../../tests/fixtures/consumer/tooling/routing.go).

Readiness reports describe dependency availability. Process liveness and the
application's startup/draining state are separate inputs to the final transport
response; a dependency outage alone must not trigger a process restart loop.

`maintenance.Gate` controls new work admission. Its zero value serves normally.
`Set(true)` pauses, `Set(false)` resumes, and `Drain()` permanently closes
admission. `Admit()` returns immediately for request transports; `Wait(ctx)`
allows workers to wait for resumed admission without polling. A gate does not
cancel existing work, dispose resources, create goroutines or bypass auth.
The recorder's gate is shared by managed kernels. HTTP rejects new requests with
503; workers wait before reserving jobs; schedules stop admitting occurrences
without advancing their pending time, then apply their existing catch-up policy
on resume. CLI rejects execution before constructing a command handler. WebSockets
reject new upgrades and domain messages while allowing existing peers to
unsubscribe and release resources. These checks preserve admitted work and do not
retroactively revoke a reservation or active callback.

Application shutdown drains the gate before cancelling managed work. A manually
drained standalone scheduler admits nothing further; its owner still calls Stop.
Pausing does not suspend lease renewal, heartbeats, response consumption or other
infrastructure needed by already admitted work. HTTP diagnostics exceptions are
explicit GET/HEAD paths and preserve normal authorization. See
[protected diagnostics](production-diagnostics.md) and
[shared observations](observability.md).
