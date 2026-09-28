# Jobs and the worker kernel

Workers support shared observations, maintenance admission and optional
versioned trace snapshots. Default envelopes retain their legacy wire shape.
Read the [workers-first rollout](job-trace-rollout.md) before enabling propagation.
The [master roadmap](../../blueprint/00-master-architecture-and-parity.md) records
acceptance and verification evidence. The [independent consumer](../../tests/fixtures/consumer/background/)
contains compiling public API examples; acceptance results are recorded separately.

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
from the policy's bounded backoff with additive jitter. Jobs are at least once:
a process may crash after applying a side effect and before acknowledgement.
Use `WelcomeJob.CurrentID(ctx)` or `jobs.Current(ctx)` for stable idempotency.
`Current` is valid only during an actual handler attempt, not admission/preparation.

Payloads have a 960 KiB bound inside a 1 MiB envelope; shared JSON depth/node
bounds apply to the complete envelope. Payloads are immutable captured JSON.
The Redis authority treats that JSON as opaque text so large integers and exact
keys never pass through Lua floating-point conversion.

## Backend and resource ownership

`jobs/memory.New(memory.DefaultConfig())` is an explicit bounded local authority.
It owns no goroutines, retains deterministic history and accepts an injected clock.
It is not durable and cannot supply an outbox publication route.

`redis.NewJobBackend(existingClient, jobs.DefaultQueueConfig())` borrows the
existing framework Redis client. It creates no separate driver or connection
owner. Queue limits must match across processes; incompatible policy fails.
Redis acceptance depends on the deployment's persistence/failover configuration,
not merely the existence of a queue key. Scripts are atomic but not automatically
retried after network ambiguity. Queue keys must be protected from unrelated writes.

Limits cover retained jobs, serialized payload bytes, history and terminal
retention. Redis limits apply per queue; the memory authority's limits apply to
its whole instance. Workflow/uniqueness metadata is also bounded. Unfinished work
is never evicted for capacity. Redis maintenance is incremental; idle queues are
reaped on subsequent operations, not by a global background scanner.

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
and increments attempts once. Expired attempts remain counted. Unknown versions
and malformed payloads become retained failures.

Timeout cancels the complete preparation/handler/middleware context. Go cannot
kill a context-ignoring callback. The worker keeps its slot and renews ownership
until the callback exits, including during bounded `Stop(ctx)` waits. `Done`
closes only after actual exit. Losing the authority cancels the handler and stops
the worker; idempotency remains necessary after process/network/lease loss.

`WelcomeJob.Cancel` uses a typed ID and checks name/version. Waiting jobs terminate
immediately; running jobs receive cancellation and keep ownership while draining.
`Workflow.Cancel` applies this rule to all members. Shutdown stops reservation,
cancels work and records retries/failures after callbacks exit. Self-wait through
the active handler context is rejected.

## Middleware, rate limits and uniqueness

`DeclareWith(handler, jobs.HandlerOptions[P]{...})` attaches concrete middleware
and optional admission. `Before` runs in declaration order; `After` and `Failed`
unwind in reverse order. Failures are joined. Panic/Goexit are isolated, including
cleanup; callback errors/payloads never enter stored history.
Error-marker searches also have bounded traversal (256 nodes and 64 unwrap levels
per search). An unclassifiable non-nil handler/admission error still consumes its
normal retry budget; it cannot keep the reservation heartbeat alive indefinitely
through a cyclic unwrap graph. A reached `Permanent` marker remains terminal.
Backend errors still stop and drain the worker. Custom error methods must return;
these bounds do not limit arbitrary work inside an extension method.

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

`Definition.Inspect` selects one typed job identity. `Dispatcher.List` exposes
bounded operational pages with name/version/state filters and an ID cursor.
Filtered pages can be empty while `Next` is nonzero. Pagination is weakly
consistent during mutation. Returned payload/history snapshots are explicit
private-data boundaries; authorize access before exposing them through HTTP.
History uses bounded classifications rather than arbitrary error or panic text.

## Transactional publication

Prepare a `jobs.Outbox` with a destination and dispatcher. Inside a business
transaction use `Definition.Enqueue`, or capture once and use `Pending.Enqueue`
when preserving execution identity across uncertain outcomes is necessary.
Rollback suppresses publication. The returned typed outbox ID does not establish
outer commit, queue acceptance or handler success.

Apply `outbox.Migrations()` explicitly. Its second additive migration introduces
publication progress while preserving the original migration. Configure
`outbox/publisher.New` with an outer transaction authority and the job producer's
`PublicationRoute`. The publisher locks one due row with `FOR UPDATE SKIP LOCKED`,
publishes to the durable queue, then records acceptance. Callbacks remain owned
through actual exit. SQL cancellation or connection loss can release the row lock
earlier, so stable destination identities must also deduplicate overlapping
publication attempts. Multiple publishers normally skip each other's locked rows.

A crash after queue acceptance but before SQL commit republishes the same execution
identity. Set queue retention beyond the maximum publication/recovery window.
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
