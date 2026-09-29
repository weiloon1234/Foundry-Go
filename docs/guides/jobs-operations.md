# Worker operations and failed jobs

The existing [jobs engine](jobs.md) owns typed dispatch, Redis reservations,
automatic retries, backoff, leases and shutdown. These operational APIs reuse that
engine and the application's configured logger, connections and CLI lifecycle.

## Defaults and concurrency

| Setting | Default |
| --- | --- |
| Execution attempts per cycle | 5 total |
| Backoff after failures | 5 seconds, 30 seconds, 1 minute, 5 minutes |
| Additive jitter | Uniformly selected from 0 to max(1 second, delay/5) |
| Attempt timeout | 5 minutes, including preparation and middleware |
| Concurrent jobs per worker | 4 |
| Reservation lease / heartbeat | 30 seconds / 5 seconds |
| Idle polling | 100 milliseconds, doubling to 1 second while idle |
| Graceful drain on shutdown | 5 seconds |
| Backend failure backoff cap | 30 seconds |
| Retained terminal jobs | 7 days |
| History | Last 64 transitions per job |

`jobs.DefaultPolicy(queue)`, `DefaultWorkerConfig` and `DefaultQueueConfig` own
these defaults. Retry delays beyond the backoff slice reuse its last delay.
Queue capacity defaults to 4,096 live (unfinished) jobs and 64 MiB of accounted
envelope storage; a full queue returns the retryable `jobs.ErrQueueFull`
(`fault.Overloaded`). Terminal records are retained separately, up to 65,536
(`MaxRetained`) with the oldest evicted first, so retained successes and failures
never block new work. Configure both for the workload. Unfinished jobs are never
evicted.

Each worker runs a bounded number of goroutine reservation loops. Active work has
a heartbeat goroutine; channels coordinate its completion and cancellation.
Redis remains the shared queue authority across processes. The memory adapter is
local and non-durable. Multiple worker processes can subscribe to the same queues.
Redis durability depends on its persistence/failover configuration.

Timeouts cancel context; they cannot terminate a handler that ignores cancellation.
Its slot and owned dependencies remain live until actual exit. Backend failures
(including Redis client overload) never stop the worker: each is logged with a
redacted diagnostic, the loop backs off with jitter up to `FailureBackoff` and
retries. The framework does not silently switch backends. A renewal error keeps
retrying until the lease would really have expired; a lost lease cancels only its
own handler while the other reservation loops keep running.

On shutdown the worker drains: it stops reserving, lets admitted handlers finish
within `DrainTimeout` while their heartbeats continue, then cancels the rest and
releases them with a refunded attempt. Keep `DrainTimeout + OperationTimeout`
within the application's `ShutdownTimeout - StopDelay`; assembly rejects a
configuration that does not fit.

## Failure logging and hooks

Configured workers and `jobs.WorkerModule`/`Module` borrow the application's logger.
`Worker.Config.FailureLog` defaults to true. Disable it through settings or the
generated `SettingsConfigKeys().Worker.Config.FailureLog` override when supplying
another logging policy. Standalone `jobs.NewWorker` accepts
`jobs.WithWorkerLogger(logger)` and does not select a global logger implicitly.

An ordinary failed attempt emits WARN when requesting a retry, or ERROR when
requesting terminal failure. Backend/finalization failures emit ERROR. Successful
work, ordinary rate-limit release and normal cancellation are not failure logs.
An attempt interrupted by shutdown is released with a refund and stays quiet;
its handler never observed completion, so it is not counted as a failure.
A backend operation the worker survives logs `job backend operation failed` at
ERROR with `operation` (reserve, start, renew or finish), `queue` and a redacted
`diagnostic` (Go type names and framework fault codes, never error text).
Fields include job ID/name/version, queue, attempt/max attempts, manual retry cycle,
safe reason, requested state, retry delay and whether finalization was acknowledged.
`requested_state` describes the worker's requested transition: a concurrent cancel
can take precedence at the backend. `finalized=false` means no confirmed completion;
inspect the queue before making recovery decisions.

The logger never receives the payload, raw error, panic value, reservation owner
or retry token. Context-aware handlers retain framework correlation. Existing log
channels own file rotation and retention. Logs are emitted after finalization;
a custom logger panic/Goexit cannot turn completed work into another execution.
A blocking logger retains its worker slot and shutdown ownership until it returns.

For domain-specific diagnostics use `jobs.Middleware[Payload].Failed` with
`Definition.DeclareWith`, or `application.JobWith` in configured assembly. The
constructor returns a typed handler and `jobs.HandlerOptions[Payload]`. `Failed`
receives the actual error for each failing middleware/handler invocation; it is
not a once-only terminal-failure callback and does not cover malformed payloads
or unknown versions. Explicitly select safe fields rather than logging arbitrary
errors or DTOs. `jobs.Current(ctx)` exposes job identity, attempt and retry cycle.

Observation reporters and trace exporters remain optional and separately configured.
Default failure logging does not require enabling observability or adding a reporter.

## Inspect and explicitly retry

`Definition.Inspect` and `Bound.Inspect` preserve payload-specific IDs. The explicit
heterogeneous `Dispatcher.Inspect` and `Dispatcher.List` support operator tools.
Full records contain private payloads. `Record.Summary()` returns only operational
metadata and a retry token when the record is eligible.

For a retained independent failed job:

```go
found, err := WelcomeJob.Inspect(ctx, dispatcher, id, "communications")
if err != nil { return err }
record, present := found.Get()
if !present { return jobs.ErrNotRetryable }
token, err := record.RetryToken()
if err != nil { return err }
// Save token before executing the operator-approved action.
changed, err := WelcomeJob.Retry(ctx, dispatcher, id, "communications", token)
```

`changed=true` with no error means this request reopened the job. `changed=false`
with no error confirms that the same token was already applied. Any error is not
proof of acceptance. Reconcile uncertain network or output failures using the
**same saved token**, never by fetching a fresh token and blindly retrying again.
The token identifies an observed failed state; it does not authorize the operator.

The atomic transition preserves the original job ID, exact payload, envelope
version, policy, attribution, uniqueness declaration and bounded history. It clears
terminal time, resets attempts to zero, increments `Record.Retries` and makes the
job immediately eligible. `jobs.Current(ctx).Number` starts again at one;
`.Retry` identifies the manual cycle. Use the stable ID for side-effect idempotency.
No new enqueue uniqueness window is created, and existing outbox dispatch IDs
continue to deduplicate against the same retained record.

A repeated token cannot reopen that cycle's later failure. A token from an older
cycle conflicts after another manual retry has been applied. Missing/expired,
waiting/running, successful, cancelled and workflow-member jobs are ineligible.
Manual retries are capped by `jobs.MaxManualRetries`. History and terminal retention
remain bounded; retried active work is not expired using its prior terminal time.

Permanent errors suppress automatic retries, but an operator may explicitly retry
after remediation. Retrying does not prove that a previous side effect failed.
Workflow members are deliberately rejected: replay the appropriate application
workflow using its existing idempotency and authorization rules rather than
silently resurrecting cancelled dependants.

Built-in memory and Redis backends implement the optional `jobs.RetryBackend`.
Existing custom `jobs.Backend` implementations still compile; unsupported manual
retry returns an explicit error. Both built-in adapters compare the failed state
atomically; concurrent submissions of one token apply at most once. Redis does
not automatically repeat an ambiguous mutation.

## Application CLI

Register `jobs/command.Declaration` in the application's existing `cli.Registry`.
Its constructor receives `foundation.Resolver`; resolve `application.Services`
with `application.FromResolver` and return `services.Jobs`. See the executable
[consumer declarations](../../tests/fixtures/consumer/background/operations.go).
Parse before building services, register the existing `cli.Module` and run
`app.Run(ctx, foundation.CLI)`. The development `go tool foundry` binary cannot
discover or boot an application's business registrations automatically.

For an application binary exposing this declaration:

```sh
./service jobs failed --connection background --queue communications --format json
./service jobs inspect --connection background --queue communications --id JOB_ID --format json
./service jobs retry --connection background --queue communications --id JOB_ID --token RETRY_TOKEN --format json
./service jobs stats --connection background --queue communications --format json
./service jobs retry-failed --connection background --queue communications --confirm [--name welcome --version 1]
./service jobs flush-failed --connection background --queue communications --confirm
./service jobs forget --connection background --queue communications --id JOB_ID
./service jobs clear --connection background --queue communications --confirm
```

Use the actual ID and `retry_token` returned by inspection. Connection and queue
may be omitted to select their configured defaults. Unknown explicit names fail;
they do not fall back. `failed` returns one bounded page (default 20, maximum 100).
Pass its `next` cursor with `--after`; filtered pages can be empty while a next
cursor exists. `inspect` includes bounded history. Output never includes payloads,
raw errors, secrets or reservation owners.

`stats` reports queue depth (`waiting`, `delayed`, `blocked`, `leased`, `failed`,
`retained`) from indexes and counters without reading payloads
(`Dispatcher.Stats`, optional `jobs.StatsBackend`). The bulk operations walk the
whole queue in bounded pages and require `--confirm`; `--name`/`--version`
narrow them to one job schema. `retry-failed` (`Dispatcher.RetryFailed`) retries
each independent failed job with the token of the failure it observed, so a
concurrent change is skipped, never overwritten; workflow members, jobs at their
manual retry cap and names this binary does not register are skipped.
`flush-failed` (`Dispatcher.FlushFailed`) and `forget` (`Dispatcher.Forget`,
optional `jobs.ForgetBackend`) remove retained terminal independent records and
their deduplication identity, so a later dispatch or outbox republication of that
ID is accepted as new work. `clear` (`Dispatcher.Clear`) requests cancellation of
every unfinished job: waiting and blocked jobs end as cancelled immediately and
running handlers receive cancellation; records stay retained and inspectable.
Each page's changes are confirmed independently; an error leaves earlier pages
applied. Built-in memory and Redis backends implement both optional interfaces.

Retry output reports `acceptance=confirmed` only when the backend confirms the
operation, including a deduplicated retry. An output failure may follow successful
mutation. Operator permissions, shell access and audit of who approved replay
belong to the application's deployment; tokens are concurrency guards only.

## Queue depth metrics

`jobs.NewDepthMonitor(interval, targets...)` samples `Dispatcher.Stats` for each
`jobs.DepthTarget{Connection, Queue, Dispatcher}` in the background (`Run`) and
serves the latest `jobs.QueueDepth` readings without I/O (`Snapshot`): the
connection, queue, `QueueStats`, sample time and whether the last sample
succeeded. Configured applications with observability enabled run one monitor
for each job connection's default queue and export it as the `foundry.jobs`
collector (see [observability](observability.md)). Only processes running the
worker or scheduler kernel (or started without selecting a kernel) sample, so
HTTP replicas and CLI commands add no queue-authority load.

## Durable failed-job archive

`jobs/archive` keeps terminal failures beyond queue retention. `archive.New(db,
schema, clock)` is a `jobs.FailureSink`: attach it with `jobs.WithFailureSink`
(standalone workers) or `jobs.RegisterFailureSink` (every worker of a dispatcher),
or enable `worker.archive.enabled` with `worker.archive.database` and
`worker.archive.schema` in a configured application. Apply `archive.Migrations()`
(table `foundry_failed_jobs`) to that schema; `000002_store_original_envelopes`
stores each envelope as its original transport bytes (bounded by
`jobs.MaxEnvelopeBytes` before writing), so `Store.Retry` decodes exactly what
the queue carried. After the queue confirms a terminal
failure the worker records the complete envelope (payload included), queue,
reason, attempts, exceptions and manual-retry cycle; one entry per execution and
cycle. Recording is bounded by `OperationTimeout`, logged as a backend failure
when it fails and never changes the job's outcome; a crash between finalization
and recording can miss an entry.

`Store.List` pages entries newest first by `(failed_at, id)` (safe metadata,
never payloads). `Page.Next` is an `archive.Cursor` carrying the last entry's
position, so continuing never repeats or skips entries even when that entry was
pruned meanwhile; pass it as `ListOptions.After` with the same `Name` filter
(`archive.ParseCursor` restores a printed cursor).
`Store.Retry` re-dispatches an entry as a new job through
`Dispatcher.Redispatch` (fresh execution ID, immediate availability, no
uniqueness window or retry deadline) and records `RetriedAt`, and `Store.Prune` deletes entries
older than a cutoff in bounded batches. Register
`jobs/archive/command.Declaration` for the `failed-jobs list|retry|prune`
commands; configured applications resolve the store with
`services.JobArchive()`, include `archive.Migrations()` in `app.Migrations()`, and
can prune it on a schedule through the [housekeeping schedule](production-operations.md#housekeeping-schedule).

## Redis queue layout upgrade

This release stores Redis queues in layout 2. It never migrates a queue written
by the previous release (layout 1) implicitly: every operation on such a queue
returns `jobs.ErrLegacyLayout` ("stop or drain every process of the previous
release, then run `jobs migrate-layout`") and changes nothing, so the previous
release keeps working on it. The previous release cannot read a migrated queue,
so migrate each queue explicitly:

1. Stop or drain every worker, dispatcher and operator tool of the previous
   release that uses the queue (drain: stop dispatching, let workers finish,
   then stop them).
2. Take a Redis snapshot (for example `BGSAVE`) if you may need to roll back.
3. Run `./service jobs migrate-layout --connection NAME --queue QUEUE --confirm`
   (or `Dispatcher.MigrateLayout`). It is one atomic script that keeps every
   record's ID, state and history; `migrated=false` means the queue was already
   migrated or empty, and repeating is safe.
4. Start the new release.

Rollback to the previous release requires restoring the pre-migration snapshot,
or draining the migrated queue completely (no unfinished or retained jobs you
still need) before the previous release uses it. New queues are created in
layout 2 directly.

## Rollout and limits

The envelope format is unchanged unless a job opts into the format-3 fields. The
Redis queue configuration identity now includes `MaxRetained`, and existing
queues need the explicit layout upgrade above. Stored records gain optional
retry metadata; readers treat older
records as cycle zero. Upgrade
workers and operator tools together before enabling manual retries so all history
writers preserve cycle metadata. Regenerate configuration with the selected
framework version to obtain the new logging key.

There is no queue dashboard, autoscaling controller or automatic failed-workflow
recovery. The framework owns queue execution and the reusable operator commands;
the boilerplate supplies domain declarations, configuration and the CLI entry point.

The [2026-09-28 verification record](../evidence/jobs-operations-20260928.json)
captures the accepted source, full gate, real Redis/race checks, typed consumer,
compiler and editor evidence. Use the [boilerplate handoff](jobs-boilerplate-handoff.md)
for application integration and deployment acceptance.
