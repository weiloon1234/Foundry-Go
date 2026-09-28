# Production operations

Production behavior passed the [recorded acceptance and audit](../production-acceptance.md).
Configure one application lifecycle owner, explicit providers and typed
registries. Bind operational routes through the same HTTP server, authentication
and permission system as ordinary application routes.

## Startup and readiness

Constructors and provider factories validate declarations without performing
application I/O. Boot dependencies in their declared graph order. Do not report
ready merely because a listener bound successfully: `Server.Ready` reports that
one binding attempt, while `diagnostics.Readiness` additionally checks application
state, maintenance admission and declared dependency probes.

Register only dependencies required for this instance's work. A worker and an
HTTP-only service may need different probes. A configured read pool is separately
checked and must start successfully alongside its primary; read failure does not
silently reroute its traffic. For consistency-sensitive reads, select
`DB.Primary()` or the actual transaction. See [database routing](database-routing.md).

When readiness fails, keep liveness independent. Check lifecycle and typed probe
states under the operator permission, then inspect the selected dependency's
protected diagnostics. Repair endpoint/credentials/network capacity through the
normal operational process. Avoid a restart loop for a healthy process whose
database is temporarily unavailable. Probe reports omit arbitrary error payloads;
do not put credentials, SQL, request bodies or user data in probe IDs.

## Maintenance and rolling termination

Use the application's recorder `Gate().Set(true)` to pause new work and publish
non-ready status. Existing HTTP handlers, jobs, heartbeats and owner cleanup
continue. Workers pause before reservation; schedules retain pending occurrence
times and use their bounded catch-up policy after resume. WebSockets reject new
upgrades/messages/subscriptions while allowing unsubscribe cleanup. CLI admission
fails before constructing the command handler. `Set(false)` resumes a paused
gate. `Drain()` is terminal and cannot be resumed.

Keep protected GET/HEAD diagnostics reachable during maintenance using exact
descriptor-derived `MaintenanceReadPaths`. That exception preserves auth and
permissions and cannot reopen a shutting-down listener. Do not add broad path
prefixes or write endpoints. Start a rolling replacement only after it is ready,
remove the old instance from admission, and allow its owned work to finish.

Application shutdown drains admission before canceling managed work. HTTP grants
its configured grace, then cancels remaining handler contexts and closes ordinary
connections. Kernel/resource owners continue to account for callbacks that have
not returned. A shutdown timeout is an incomplete drain: record it, inspect active
owners and retain dependency ownership. `Done` means actual completion; elapsed
grace alone does not. Raw hijacked connections and background work launched after
a handler returns require an explicit application owner.

Registry/provider shutdown follows reverse dependencies. Readiness probes drain
before the dependencies they borrow. Error/trace export runs through application
cleanup so cleanup failures can be reported; exporters must own transports that
remain usable then, independently of already-closed ordinary providers. Bound
export I/O and honor its context. A callback that ignores cancellation retains
its slot and can keep shutdown incomplete; Go cannot safely terminate arbitrary
callback code.

Error `Is`, `As` and `Unwrap` methods are callbacks too. Database/storage outcome
classification and worker/pub-sub error inspection keep their operation owners
until those methods return. Panic or Goexit becomes a conservative failure;
shutdown never treats an unfinished inspection as released work. Worker backend
reserve/start/renew/finish calls follow the same rule. An uncertain backend write
still requires reconciliation, without an automatic retry. Database drivers must
also obey database/sql's own driver contract, including error methods invoked
inside that package; Foundry cannot repair internal driver state after a driver
panics or calls Goexit.

## Resource pressure

| Shared resource | Default | Exhaustion/lifetime behavior |
| --- | --- | --- |
| HTTP accepted connections | 4096 | Capacity is reserved before Accept; additional peers wait in the OS backlog. A hijacked connection retains its slot until Close. |
| HTTP active requests | 1024 | Excess requests receive 503; cancellation does not free a still-running handler's slot. |
| HTTP maintenance read paths | At most 16 configured paths | Exact unescaped GET/HEAD matches only. |
| Readiness callbacks | 8 across a registry, at most 64 probes by default | Requests wait within their deadlines; an unfinished callback retains capacity. |
| Readiness time budgets | 3 seconds total, 1 second per probe | Dependency failure/cancellation changes readiness, not liveness. |
| Diagnostics HTTP operations | 8 | Capacity exhaustion returns 503 before probes start. |
| Active observation spans | 65536 | New observations are dropped and counted; domain work is not rejected solely for missing telemetry. |
| Metric series / recent entries | 512 / 128 | New excess series are counted as dropped; recent entries overwrite the oldest ring entry. |
| Error queue / trace queue | 128 / 512 | Separate nonblocking queues drop and count excess metadata. |
| Export workers / callback timeout | 2 and 3 seconds per family | Ignored cancellation retains the actual callback owner; queues remain bounded. |

These are configurable finite defaults, not workload sizing claims. Combined
database connection limits are explicitly selected with routing configuration;
pool, job, schedule, socket, storage and client limits retain their subsystem
configuration. Account for all instances, pools and independent registries when
sizing a deployment. Use [native measurements](developer-resource-measurements.md)
for developer-tool working sets, separately from application load evidence.

Inspect dropped series/spans/reports/traces, export failures, active owners and
dependency state together. A blocked telemetry sink must not grow application
queues or inject raw payloads into metrics. Repair a sink before increasing its
budget. A high-cardinality name is a declaration problem: use a stable registered
operation name, keeping request/trace IDs only in bounded entries and correlated
logs. Do not label metrics by raw URL, model key or user identity.

## Trace trust and data minimization

HTTP requests begin new roots by default. Enable `TrustTraceContext` only at an
explicitly trusted ingress; malformed or duplicate parents produce a new root,
and invalid vendor state is discarded independently. Enabling sampling controls
trace export, not authentication. Keep `httpclient.PropagateTrace` disabled for
destinations that should not receive trace metadata; enabled propagation may send
vendor state. Logs, error reports and recent entries omit that vendor state.

Queued propagation is a separate opt-in transport upgrade. Deploy compatible
workers/outbox/workflow readers first and follow [job trace rollout](job-trace-rollout.md).
Retain old application payload handlers while old work remains. Do not regenerate
an ambiguous outbox operation under the same ID with different captured trace or
payload bytes; reconcile and retry the original captured operation.

Protected diagnostics do not expose a profiler, environment dump or unrestricted
configuration/error output. They expose bounded operational metadata. Use normal
log redaction for custom sinks and `logging.Correlate` for context-aware custom
handlers; `logging.JSON` already applies correlation. Scope log access and
retention like other operational data.

## Recovery evidence

After a fault, record the failed operation, lifecycle/admission state, queue or
transaction outcome and actual owner completion. For storage and outbox, preserve
their explicit applied/unknown outcome and reconciliation references privately.
Do not equate an error with a safe retry. Apply reviewed forward database
migrations and scoped repairs; never reset an existing database as a recovery
shortcut. Rerun affected integration/contract checks after a repair and retain
their real results. See [release checks](../release-checklist.md) for final
verification and compatibility evidence.
