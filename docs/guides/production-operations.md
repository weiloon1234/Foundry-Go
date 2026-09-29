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

Every application owns one maintenance gate, with or without observability:
`services.Maintenance()` (or `App.Maintenance()`) returns it. `Set(true)` pauses
new work and publishes non-ready status. Existing HTTP handlers, jobs, heartbeats
and owner cleanup continue. Workers pause before reservation; schedules retain
pending occurrence times and use their bounded catch-up policy after resume.
WebSockets reject new upgrades/messages/subscriptions while allowing unsubscribe
cleanup. CLI admission fails before constructing the command handler. `Set(false)`
resumes a paused gate. `Drain()` is terminal and cannot be resumed.
`features.observability.maintenance = true` starts an instance paused.

For a fleet, name a configured cache store (Redis, or PostgreSQL with its explicit
cache migration) in `maintenance.store`. Each instance applies the shared record at
boot and polls it every `maintenance.poll_interval` (5s by default, 100ms-10m);
requests only read the local gate, and a store outage keeps the last applied state
and logs one warning until it recovers. Register `application.MaintenanceCommands()`
in the CLI registry for the operator commands:

```sh
app down --retry 60 --message "Upgrading, back soon" --with-secret \
  --allow 203.0.113.0/24 --except "POST /hooks/*"
app up
```

`down` publishes the record; paused responses are 503 with the standard error
envelope, the operator `message` and `Retry-After`. Operator messages are not
logged as server failures. `--with-secret` prints a generated secret (or pass
`--secret`); only its SHA-256 digest is stored. Visiting `/<secret>` sets an
HTTP-only `foundry_maintenance` cookie (12 hours by default, `maintenance.bypass_ttl`)
sealed with the application encryption keys (`encryption.key_id`/`key`), so it
admits that browser on every instance sharing those keys until the secret
changes, and the stored digest alone cannot mint one. Without application keys the
cookie is valid only on the instance that issued it. The cookie is `Secure` for
native TLS or a trusted https scheme. `--allow` admits client addresses or CIDRs.
With a global `http.TrustedProxy` the allow list matches the client it resolves;
without one it matches the socket peer, which behind a load balancer is the
balancer, so allowing its subnet would admit everyone. `--except "[METHOD ]/path[/*]"` exempts exact paths or path
prefixes for any or one method. `maintenance.exempt` and `maintenance.allow`
configure permanent exemptions. Both commands are declared with
`AllowDuringMaintenance()`; run any other command during maintenance, such as a
migration, with the explicit leading flag `app --during-maintenance migrate`.

Keep protected GET/HEAD diagnostics reachable during maintenance using exact
descriptor-derived `MaintenanceReadPaths`, or narrow exemption rules. Exemptions
preserve route auth and permissions and never reopen a draining (shutting-down)
listener. Do not exempt broad write endpoints. Start a rolling replacement only
after it is ready, remove the old instance from admission, and allow its owned
work to finish.

`http.probes.liveness`/`readiness` mount public `/up` and `/ready` routes that
report only status (see [diagnostics](production-diagnostics.md#public-probes));
they stay reachable while paused so orchestrators do not restart a paused but
healthy instance.

`ShutdownTimeout` (default 25 seconds) is one budget from the shutdown request:
the optional `StopDelay` lame-duck period (lifecycle `Stopping`, so readiness fails
while listeners keep serving and load balancers deregister the instance), kernel
drain and every cleanup share it. Keep it below the orchestrator's termination
grace (for example Kubernetes' 30-second default) and above `StopDelay` plus the
HTTP shutdown grace; configured assembly rejects budgets without room for cleanup.
A signal-initiated graceful stop returns nil from `Run`, so the process exits 0.

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

## Housekeeping schedule

Bounded stores grow until something prunes them. `features.maintenance.enabled`
(off by default) registers leader-only housekeeping schedules in each process that
runs the scheduler kernel; other processes only validate the settings. Every task
is one `foundry.maintenance.<task>` schedule with `WithoutOverlap` and a stable
name-derived offset within its interval (so tasks do not all start at the top of
the hour), runs on the scheduler leader (so on one server), pauses with the
maintenance gate, and removes at most `batch` rows per statement and
`max_batches` batches per run; a backlog continues at the next run. Runs that remove rows log `maintenance task pruned
records` with the task, count and whether work remained; failures are logged and
observed by the scheduler like any schedule.

| Task | Registered when | Removes |
| --- | --- | --- |
| `outbox` | `features.outbox.enabled` | Published rows completed before `retention` (30 days); never pending or failed rows |
| `idempotency` | Idempotency enabled with `config.prune_interval = 0` | Expired outcomes; otherwise the store's own pruner already runs, so it is not pruned twice |
| `audit` | Audit enabled with `config.retention_days > 0` | Entries of the configured area older than that retention |
| `jobs.archive` | `worker.archive.enabled` | Archived failed jobs older than `retention` (30 days) |
| `notifications.inbox` | `features.notifications.enabled` | Read inbox records older than `retention` (90 days); unread too with `notifications_unread` |
| `sessions.<guard>`, `tokens.<guard>` | Declared with `application.PruneSessions` / `PruneTokens` | Expired sessions / token families of that guard |
| `models.<connection>` | Declared with `application.PruneModels` | [Prunable models](model-pruning.md#scheduled-pruning) of one connection, selected again each run |
| declared name | Declared with `application.PruneWith` | Another store's bounded prune (challenge flows, verifications, resets, MFA factors, outbound webhook deliveries) |

Declared tasks are returned in `FeatureDeclarations.Pruning`; guards are
constructed there with `NewBrowserGuard`/`NewTokenGuard` because a guard's
provider, not configuration, identifies its credentials. Defaults run each task
hourly with 500-row batches, 20 batches and a 5-minute timeout. Override them for
all tasks under `features.maintenance.defaults` or per task under
`features.maintenance.<outbox|idempotency|audit|job_archive|notifications|sessions|tokens|models|custom>`
(`interval`, `batch`, `max_batches`, `timeout`, `retention`, `disabled`). Build
rejects intervals under one second, timeouts over a day and batches above the
store's own bound. Audit applies its batch size but not `max_batches`; one run
removes every expired batch within its timeout. Cache backends and datatable
export artifacts own their cleanup and are not scheduled. Outbound webhooks
(`webhook/outbound`) are constructed by the application rather than configured
assembly, so declare their delivery retention yourself:

```go
application.PruneWith("webhooks.deliveries", outbound.MaxPruneBatch, func(ctx context.Context, limit int) (int64, error) {
    removed, err := webhooks.PruneDeliveries(ctx, 30*24*time.Hour, limit)
    return int64(removed), err
})
```

```toml
[features.maintenance]
enabled = true
[features.maintenance.outbox]
retention = '168h'
interval = '30m'
```

## Resource pressure

| Shared resource | Default | Exhaustion/lifetime behavior |
| --- | --- | --- |
| HTTP accepted connections | 4096 | Capacity is reserved before Accept; additional peers wait in the OS backlog. A hijacked connection retains its slot until Close. |
| HTTP active requests | 1024 | Excess requests receive 503; cancellation does not free a still-running handler's slot. |
| HTTP maintenance read paths | At most 16 configured paths | Exact unescaped GET/HEAD matches only. |
| Readiness callbacks | 8 across a registry, at most 64 probes by default | Probes in one check run concurrently; requests wait within their deadlines; an unfinished callback retains capacity. |
| Readiness time budgets | 3 seconds total, 1 second per probe | Each probe has its own deadline. Dependency failure/cancellation changes readiness, not liveness. |
| Maintenance rules | 64 exemptions and 64 networks each for configuration and shared state | Message at most 512 bytes; Retry-After at most one day; poll 100ms-10m. |
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

Protected diagnostics do not expose an environment dump or unrestricted
configuration/error output. They expose bounded operational metadata, and a
`runtime/pprof` profiler only when `diagnostics.Config.Profiling` is enabled and a
Profile route is explicitly bound under operator authorization. Use normal
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
