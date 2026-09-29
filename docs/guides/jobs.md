# Jobs and the worker kernel

Workers support shared observations, maintenance admission and optional
versioned trace snapshots. Default envelopes retain their legacy wire shape.
Read the [workers-first rollout](job-trace-rollout.md) before enabling propagation.
The [master roadmap](../../blueprint/00-master-architecture-and-parity.md) records
acceptance and verification evidence. The [independent consumer](../../tests/fixtures/consumer/background/)
contains compiling public API examples; acceptance results are recorded separately.

[Worker operations](jobs-operations.md) covers automatic failure logging,
failed-job inspection, explicit retry tokens, application CLI registration and
deployment ownership. The [boilerplate handoff](jobs-boilerplate-handoff.md)
collects the integration steps for a separate application repository.

## Concrete payloads and stable dispatch

Declare one named DTO and reuse its definition for registration and dispatch:

```go
type Welcome struct {
    UserID model.ID[User] `json:"user_id"`
}
var WelcomeJob = jobs.Define[Welcome]("members.welcome", 1,
    jobs.DefaultPolicy("communications"))

declaration, err := WelcomeJob.Declare(func(ctx context.Context, input Welcome) error {
    return sender.SendWelcome(ctx, input.UserID)
})
registry, err := jobs.NewRegistry(declaration)
dispatcher, err := jobs.NewDispatcher(backend, registry,
    jobs.DefaultDispatchConfig(namespace))
receipt, err := WelcomeJob.Dispatch(ctx, dispatcher,
    Welcome{UserID: userID}, jobs.Options[Welcome]{})
```

Handle every returned error. Different payloads, options, IDs, middleware and
uniqueness keys cannot be substituted across concrete DTO types. Registration
rejects duplicate names/versions, interfaces, models, contexts and secret wrappers
in payloads. IDs and immutable values belong in messages; services belong in the
handler constructor. Custom JSON codecs must be deterministic and cooperate.

`Capture` prepares a `Pending[P]` without I/O. Capture once and retry
`pending.Dispatch` when backend acceptance is unknown. It preserves the exact
payload, policy, attribution and dispatch identity. Repeating `Dispatch` with a
zero ID creates a different job. A receipt accompanied by an error is not proof
of acceptance. Reusing an ID for a different envelope conflicts; unchanged IDs
are deduplicated while retained. Retention is finite.

`Options.At` schedules eligibility at an absolute instant, up to one year ahead;
zero means immediately. Redis rounds sub-millisecond eligibility upward so work
never starts early because of timestamp precision. `Options.Queue` overrides routing. Retry delays come
from the policy's bounded backoff plus additive jitter proportional to the delay:
a retry adds a uniform duration in `[0, max(Jitter, delay/5)]`, so a burst of
failures with long backoff does not retry in lockstep. Zero `Jitter` disables it.
`Dispatch` admission queues briefly when `DispatchConfig.MaxInFlight` (default
256) is busy and then fails with `fault.Overloaded`, which HTTP maps to a
retryable 503. Jobs are at least once:
a process may crash after applying a side effect and before acknowledgement.
Use `WelcomeJob.CurrentID(ctx)` or `jobs.Current(ctx)` for stable idempotency.
`Current` is valid only during an actual handler attempt, not admission/preparation.

`definition.Encrypted(keyring)` opts one job into payload encryption with an
`encryption.Keyring`: `Capture` seals the payload JSON with the active key
(authenticated to the job name and version) and workers decrypt it before
decoding, so queues, outbox rows, archives and inspection hold only ciphertext;
the envelope reports `Encrypted()` and uses format 3. Producers and workers need
the same keyring (keep rotated keys for decryption); a worker whose definition
lacks it retries the job instead of treating the payload as invalid. Envelopes
captured before encryption was enabled still decode as plaintext.
`Definition.Payload(ctx, envelope)` decodes and decrypts one envelope's payload
for operator and test tooling (`testkit/jobs` assertions use it). The ciphertext
counts toward the payload bound.

Payloads have a 960 KiB bound inside a 1 MiB envelope; shared JSON depth/node
bounds apply to the complete envelope. Payloads are immutable captured JSON.
The Redis authority treats that JSON as opaque text so large integers and exact
keys never pass through Lua floating-point conversion.

## Backend and resource ownership

`jobs/memory.New(memory.DefaultConfig())` is an explicit bounded local authority.
It owns no goroutines, retains deterministic history and accepts an injected clock.
It is not durable and cannot supply an outbox publication route. A job dispatched
through it by a process that runs no worker kernel (for example an HTTP replica)
is never executed and is lost when that process exits. Configured job connections
therefore reject `driver = "memory"` outside the `local`, `development`, `dev`,
`test` and `testing` namespace environments unless the connection sets
`allow_memory = true`; boot fails with an actionable error instead. Use the
`redis` driver in production.

`jobs/inline.New(memory.DefaultConfig())` (configured as `driver = "sync"`) is the
sync driver for tests and local development. Its dispatcher runs each accepted
job synchronously in the dispatching caller, through the same admission,
middleware, overlap, retry and history path as a worker; no worker kernel is
needed. It is not durable and follows the memory driver's environment guard.
Handler failures are recorded on the job and never returned from `Dispatch`. A
job released with a delay (admission, rate limit, retry backoff) runs on a later
dispatch through the same dispatcher, or on `Dispatcher.RunPending(ctx, queue)`,
once due. A dispatch made by a running handler executes after that handler
returns and before the outer dispatch returns; one inline run executes at most
`jobs.MaxInlineJobs` attempts.

`redis.NewJobBackend(existingClient, jobs.DefaultQueueConfig())` borrows the
existing framework Redis client. It creates no separate driver or connection
owner. Queue limits must match across processes; incompatible policy fails.
Redis acceptance depends on the deployment's persistence/failover configuration,
not merely the existence of a queue key. Scripts are atomic but not automatically
retried after network ambiguity. Queue keys must be protected from unrelated writes.

Redis queues use storage layout 2: each job's immutable envelope is stored apart
from its small mutable state, so heartbeats and transitions never decode or
rewrite the payload, and a reservation that finds no work writes nothing. A
queue written by an earlier release (layout 1) is never migrated implicitly:
every operation on it returns `jobs.ErrLegacyLayout` until an operator migrates
it explicitly with `jobs migrate-layout` (`Dispatcher.MigrateLayout`). See the
[upgrade procedure](jobs-operations.md#redis-queue-layout-upgrade).

`QueueConfig.MaxEntries` and `MaxBytes` bound live (unfinished) jobs. A full
queue rejects enqueue with `jobs.ErrQueueFull` (`fault.Overloaded`, retryable)
and never evicts unfinished work. Terminal records kept for deduplication and
inspection are bounded separately by `MaxRetained` (default 65536) and their own
`MaxBytes` budget; beyond either bound the oldest terminal records (then the
oldest finished workflows) are evicted, and until `Retention` after completion
otherwise. Retained succeeded or failed jobs never make enqueue fail. Redis
limits apply per queue; the memory authority's limits apply to its whole
instance. Workflow/uniqueness metadata is also bounded. Redis reports an identity
conflict (`fault.Conflict`), a full queue (`fault.Overloaded`) and a queue policy
mismatch between processes (`jobs.ErrQueuePolicy`, an operator problem that the
outbox keeps retrying) distinctly. Redis maintenance is
incremental; idle queues are reaped on subsequent operations, not by a global
background scanner.

Stop and drain workers/publication before closing the borrowed backend. The
`jobs.Module` factory freezes a registry and exposes a typed dispatcher service;
only `App.Run(ctx, foundation.Worker)` starts reservations. Its provider dependencies
identify the owners of borrowed infrastructure. The foundation keeps these alive
until the kernel and all owned callbacks exit. See the consumer's `WorkerModule`.

## Execution and cancellation

Workers have bounded concurrency and weighted queue subscriptions. Each positive
weight receives service under steady load; running jobs are not preempted. There
is no cross-process strict priority ordering. Eligible jobs are ordered locally
by availability and insertion, and in Redis by availability and stable ID.

Reservations, renewals, starts and finishes compare a live random owner. An old
owner cannot acknowledge a redelivery. Start is idempotent for the current owner
and increments attempts once. Expired attempts remain counted. Malformed payloads
become retained failures. An unknown job name/version is treated as transient by
default (`WorkerConfig.RetryUnregistered`), because another replica of a rolling
deploy may already declare it: it consumes attempts with the envelope's own
backoff and then fails as `unregistered`. A reservation that repeatedly expires
before its attempt starts (for example a payload that crashes its process during
decoding) fails as `delivery_limit` after `max(Attempts, 3)` expiries instead of
being redelivered forever.

A handler that returned nil succeeded, even if the worker began stopping or the
job deadline passed after its side effects; `PreventRetry` with nil is also a
success. Only non-nil errors are classified by cause (cancellation, timeout,
panic, permanent marker or retryable failure).

Timeout cancels the complete preparation/handler/middleware context. Go cannot
kill a context-ignoring callback. The worker keeps its slot and renews ownership
until the callback exits, including during bounded `Stop(ctx)` waits. `Done`
closes only after actual exit. A failed renewal leaves ownership unknown, so the
heartbeat keeps retrying until the lease would really have expired; only a lost
lease cancels that one handler. Other reservation loops keep running, and
idempotency remains necessary after process/network/lease loss.

`WelcomeJob.Cancel` uses a typed ID and checks name/version. Waiting jobs terminate
immediately; running jobs receive cancellation and keep ownership while draining.
`Workflow.Cancel` applies this rule to all members.

Shutdown is a graceful drain. When the worker kernel's context ends (or `Drain`
is called) the worker stops reserving new jobs while admitted handlers keep their
heartbeats and run for up to `WorkerConfig.DrainTimeout` (default 5s); the
deadline then cancels them like `Stop`. An attempt interrupted by `Stop` or by
the drain deadline is released with a refund, so it does not consume its retry
budget on the built-in memory and Redis backends (`Result.Refund`; a custom
backend that ignores it consumes the attempt as before). Application assembly
rejects a `DrainTimeout + OperationTimeout` that does not fit within
`ShutdownTimeout - StopDelay`. Self-wait through the active handler context is
rejected.

Idle loops poll at `PollInterval` and double the wait up to `MaxPollInterval`
(default 1s). An enqueue, workflow or manual retry accepted by the same memory or
Redis backend instance wakes idle local loops immediately; other processes rely
on the adaptive polling. Each cycle tries every distinct subscribed queue at most
once, starting from the weighted queue, so weights never repeat backend calls.
Backend failures (reserve, renew, start, finish, including `fault.Overloaded`
from the Redis client) are logged as `job backend operation failed` with a
redacted diagnostic; the loop backs off with jitter up to `FailureBackoff`
(default 30s) and keeps running.

## Middleware, rate limits and uniqueness

`DeclareWith(handler, jobs.HandlerOptions[P]{...})` attaches concrete middleware
and optional admission. `Before` runs in declaration order; `After` and `Failed`
unwind in reverse order. Failures are joined. Panic/Goexit are isolated, including
cleanup; callback errors/payloads never enter stored history.
Error-marker searches also have bounded traversal (256 nodes and 64 unwrap levels
per search). An unclassifiable non-nil handler/admission error still consumes its
normal retry budget; it cannot keep the reservation heartbeat alive indefinitely
through a cyclic unwrap graph. A reached `Permanent` marker remains terminal.
Backend errors are logged and retried by the worker, never stopping it. Custom error methods must return;
these bounds do not limit arbitrary work inside an extension method.

`HandlerOptions.Overlap = jobs.WithoutOverlapping(leases, keyFunc, options)`
holds a typed lease named by the decoded payload from before the attempt starts
until the handler and its middleware actually exit, renewing it at a third of
`OverlapOptions.TTL`; losing it cancels the handler. Ownership is
owner-token conditional through the shared lease layer, so bind the leases to a
backend every worker shares (Redis in production). A job whose key is held is
released for `Delay` without consuming an attempt (`ReleaseOnOverlap`, the
default) or completed without running (`SkipOnOverlap`).

`HandlerOptions.Throttle = jobs.ThrottleExceptions(limiter, keyFunc, backoff)`
uses an ordinary typed rate limiter as an exception budget: its `Limit` is the
number of exceptions allowed per window (for example `ratelimit.PerMinute(10)`).
Each attempt that returns a non-cancellation error records one exception. While
the budget is exhausted, jobs with that key are released for
`max(backoff, retry-after)` without consuming an attempt. Recording is best
effort and never changes an attempt's outcome; the limiter backend must support
`Peek` (memory and Redis do). Releases by admission, throttle and overlap are
recorded with reason `rate_limited`.

`Policy.MaxExceptions` fails a job with reason `exception_limit` once that many
attempts ended with a handler error, panic or timeout, even while attempts
remain; the memory and Redis backends count exceptions per record
(`Record.Exceptions`, reset by a manual retry). `Options.RetryUntil` (or
`Policy.RetryUntil`) is an absolute deadline: a job reserved after it fails with
reason `retry_expired` without starting, and a failed attempt whose retry would
become available after it is final. `Unique[P].UntilProcessing` releases the
uniqueness window when the job's first attempt starts, so another job with the
same key can be queued while it runs; the window still bounds a job that never
starts.

These fields travel in envelope format 3 (`jobs.ExtendedEnvelope`), selected
automatically only when an envelope uses one of them; other envelopes keep the
legacy or traced format. Roll out by upgrading every worker, outbox publisher
and inspection tool first, then start dispatching with the new fields. Older
readers reject format-3 envelopes instead of ignoring the fields.

`HandlerOptions.Skip` runs after admission; `true` completes the attempt
successfully without running middleware or the handler, for work that became
unnecessary. An error consumes the attempt like an admission error.

`jobs.RateLimit[P,K](typedLimiter, keyResolver)` reuses the ordinary typed
`ratelimit` service. A denial releases the reservation with a delay and consumes
no execution attempt. Admission errors/timeouts follow the attempt budget.
Shared quotas can span job types by reusing the limiter and key declaration.

`NewUniqueKey[P]` creates a concrete business identity. Set
`Options[P].Unique = jobs.Unique[P]{Key: key, For: duration}` to suppress different
dispatch IDs during a fixed window after enqueue. Only a digest is persisted;
name, version and queue scope the window. Completion does not release it.
Conflicting admission returns `ErrNotUnique`; it is not an accepted receipt.
Uniqueness does not prevent crash redelivery of an already accepted job.

## Chains, batches and operational history

Convert captured values with `pending.Step()`, then call `NewChain(steps...)` or
`NewBatch(steps...)`. Optional `batch.WithCompletion(step)` adds a success-only
final job. A workflow is an immutable retryable capture; `workflow.Dispatch`
atomically accepts every member or none. Maximum 256 members plus one batch
completion, 8 MiB total member envelopes, one queue authority. Members must have
distinct IDs and no independent uniqueness window. Cross-queue workflows are
not part of this contract; select a common workflow queue at capture time.

A successful chain member atomically releases its successor. Terminal failure or
cancellation cancels dependent members. Batch failures retain the other members'
execution and suppress completion. Group/member history remains until the entire
workflow finishes and its retention expires. No application callback is executed
inside queue transitions.

`workflow.WithCatch(step)` and `workflow.WithFinally(step)` add callback jobs to a
chain or batch. They are ordinary registered jobs with their own retries and are
released only once every member (and the completion) is terminal: the catch job
if any member failed or was cancelled by a failure, the finally job always. An
untriggered catch ends cancelled with reason `not_triggered`; an explicit
workflow cancellation cancels both callbacks. The workflow is finished only when
its callbacks are terminal too. Workflows with callbacks use new transport
fields, so upgrade every worker and publisher before dispatching them.

`workflow.Status(ctx, dispatcher)` (or `Dispatcher.WorkflowStatus`) reports
`jobs.WorkflowStatus`: `Total`, `Pending`, `Processed`, `Succeeded`, `FailedJobs`
and `Cancelled` counts over the steps and completion, `Failed`, `Cancelling`,
`FinishedAt`, `Finished()` and `Progress()`. It never reads payloads. A workflow
retired after its retention is absent. The memory and Redis backends implement
the optional `jobs.WorkflowStatusBackend`.

`workflow.Enqueue(ctx, tx, producer)` writes the whole workflow into a business
transaction through a `jobs.Outbox`; after commit the shared publisher submits it
as one atomic group on the job route (rollback suppresses it). Every member must
be registered with the producer's dispatcher, and the stable workflow and member
IDs deduplicate repeated publication.

`Definition.Inspect` selects one typed job identity. `Dispatcher.List` exposes
bounded operational pages with name/version/state filters and an ID cursor.
Filtered pages can be empty while `Next` is nonzero. Pagination is weakly
consistent during mutation. Returned payload/history snapshots are explicit
private-data boundaries; authorize access before exposing them through HTTP.
History uses bounded classifications rather than arbitrary error or panic text.

Independent failed jobs can be retried explicitly through `Definition.Retry`,
`Bound.Retry` or the application-owned `jobs retry` command. Retain the token
from the observed failed record before retrying. The same token is idempotent
after uncertain responses and cannot reopen a subsequent failure. IDs, envelopes
and bounded history stay intact; each manual retry starts a new attempt budget.
Workflow members cannot be reopened individually because their dependent
transitions have already been committed. See [retry semantics](jobs-operations.md).

## Transactional publication

Prepare a `jobs.Outbox` with a destination and dispatcher. Inside a business
transaction use `Definition.Enqueue`, or capture once and use `Pending.Enqueue`
when preserving execution identity across uncertain outcomes is necessary.
Rollback suppresses publication. The returned typed outbox ID does not establish
outer commit, queue acceptance or handler success.

Apply `outbox.Migrations()` explicitly. Its second additive migration introduces
publication progress while preserving the original migration. Configure
`outbox/publisher.New` with an outer transaction authority and the job producer's
`PublicationRoute`. The publisher locks a batch of due rows (up to `MaxInFlight`,
across every route) with `FOR UPDATE SKIP LOCKED`, publishes them to the durable
queue with bounded concurrency, then records each outcome; see the
[outbox guide](outbox.md#publisher-throughput-backoff-and-deployment) for backoff,
logging and kernel selection. Callbacks remain owned
through actual exit. SQL cancellation or connection loss can release the row lock
earlier, so stable destination identities must also deduplicate overlapping
publication attempts. Multiple publishers normally skip each other's locked rows.

A crash after queue acceptance but before SQL commit republishes the same execution
identity. Set queue retention (and `MaxRetained`) beyond the maximum
publication/recovery window. A job whose name/version is not yet registered in
the publishing process stays pending and retries with backoff, as during a
rolling deploy.
The publisher retains failed rows and bounded reason classifications; optional
`Observe` receives private delivery errors in process after commit. `Result.Committed`
distinguishes committed progress from a transaction error/unknown outcome.
`publisher.Module` owns its polling task through foundation shutdown.

For events, `events.NewQueuedOutbox(policy, producers...)` creates one job declaration
and a publication route per explicit event destination. Register that declaration
in the job registry. Existing typed event `Enqueue` then delivers through workers,
with fresh payloads for listeners and `Topic.OutboxID(ctx)` for stable idempotency.
Earlier successful listeners can run again if a later listener fails.

## Verification

The milestone gate includes native `make verify`, required local PostgreSQL and
Redis, job/event/consumer races, compiler rejection, actual gopls probes and current
generation. Tests use isolated keys and retained database schemas; no reset or
provider provisioning is needed. The roadmap records results only after execution.


## Terminal attempt failures

`jobs.Permanent(err)` stops automatic retries while recording a failed attempt.
A nil error remains nil. Ordinary wrapped/joined markers remain terminal, even
when a handler timeout has elapsed; cancellation still follows queue semantics.
Error classification runs inside owned callback isolation so custom `Is`, `As`
and `Unwrap` implementations cannot abandon worker ownership on panic or Goexit.
The [email integration](email.md) uses this for permanent/construction failures
and uncertain provider acceptance. These additions passed milestone 16 acceptance.

`jobs.PreventRetry(ctx)` marks a live started attempt after a known side effect.
A later timeout, middleware failure or abnormal error classifier becomes terminal
failure; successful jobs still succeed. It returns false for unowned/saved
contexts. This in-memory marker does not survive process/lease loss before the
backend records the result. Email handlers mark known provider acceptance here.
