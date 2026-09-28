# Worker operations and failed jobs

The existing [jobs engine](jobs.md) owns typed dispatch, Redis reservations,
automatic retries, backoff, leases and shutdown. These operational APIs reuse that
engine and the application's configured logger, connections and CLI lifecycle.

## Defaults and concurrency

| Setting | Default |
| --- | --- |
| Execution attempts per cycle | 5 total |
| Backoff after failures | 5 seconds, 30 seconds, 1 minute, 5 minutes |
| Additive jitter | Uniformly selected from 0 to 1 second |
| Attempt timeout | 5 minutes, including preparation and middleware |
| Concurrent jobs per worker | 4 |
| Reservation lease / heartbeat | 30 seconds / 5 seconds |
| Idle polling | 100 milliseconds |
| Retained terminal jobs | 7 days |
| History | Last 64 transitions per job |

`jobs.DefaultPolicy(queue)`, `DefaultWorkerConfig` and `DefaultQueueConfig` own
these defaults. Retry delays beyond the backoff slice reuse its last delay.
Queue capacity defaults to 4,096 retained jobs and 64 MiB of accounted envelope
storage; configure it for the workload. Unfinished jobs are never evicted.

Each worker runs a bounded number of goroutine reservation loops. Active work has
a heartbeat goroutine; channels coordinate its completion and cancellation.
Redis remains the shared queue authority across processes. The memory adapter is
local and non-durable. Multiple worker processes can subscribe to the same queues.
Redis durability depends on its persistence/failover configuration.

Timeouts cancel context; they cannot terminate a handler that ignores cancellation.
Its slot and owned dependencies remain live until actual exit. Backend failures
stop admission and drain the worker. The deployment's process supervisor owns
restarts; the framework does not silently switch backends or restart the process.
Lease loss or a renewal error cancels all reservation loops immediately, even
while the affected handler is still draining after cancellation.

## Failure logging and hooks

Configured workers and `jobs.WorkerModule`/`Module` borrow the application's logger.
`Worker.Config.FailureLog` defaults to true. Disable it through settings or the
generated `SettingsConfigKeys().Worker.Config.FailureLog` override when supplying
another logging policy. Standalone `jobs.NewWorker` accepts
`jobs.WithWorkerLogger(logger)` and does not select a global logger implicitly.

An ordinary failed attempt emits WARN when requesting a retry, or ERROR when
requesting terminal failure. Backend/finalization failures emit ERROR. Successful
work, ordinary rate-limit release and normal cancellation are not failure logs.
If shutdown exhausts the attempt budget, the terminal failure is logged at ERROR
and counted as a failed observation; a release with attempts remaining stays quiet.
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
```

Use the actual ID and `retry_token` returned by inspection. Connection and queue
may be omitted to select their configured defaults. Unknown explicit names fail;
they do not fall back. `failed` returns one bounded page (default 20, maximum 100).
Pass its `next` cursor with `--after`; filtered pages can be empty while a next
cursor exists. `inspect` includes bounded history. Output never includes payloads,
raw errors, secrets or reservation owners. Commands do not clear/delete queues.

Retry output reports `acceptance=confirmed` only when the backend confirms the
operation, including a deduplicated retry. An output failure may follow successful
mutation. Operator permissions, shell access and audit of who approved replay
belong to the application's deployment; tokens are concurrency guards only.

## Rollout and limits

The envelope format and queue configuration identity are unchanged. Stored records
gain optional retry metadata; readers treat older records as cycle zero. Upgrade
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
