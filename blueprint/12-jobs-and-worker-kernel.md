# 12 — Jobs and worker kernel

## Purpose and prerequisites

Prerequisites: [07](07-model-lifecycle-events-and-audit.md), [09](09-redis-cache-and-coordination.md). Deliver typed background work with explicit at-least-once semantics.

Rust references: `src/jobs`, `src/kernel/worker.rs`, `src/email/job.rs`; `tests/phase2_acceptance.rs`, `distributed_runtime_acceptance.rs`, `notification_queue_acceptance.rs`; `blueprints/13-job-system.md`.

## Public contract

Jobs are concrete Go payload structs associated with a declared job descriptor and a typed handler. Descriptor metadata owns job ID, payload version and defaults; queue envelopes serialize only that registered payload.

```go
type SendWelcomeEmail struct {
    UserID model.ID[User]
}
```

Dispatch accepts this concrete payload. Supplying a different job payload to its descriptor fails compilation. Handlers receive context and constructor-injected services, not a serialized application container. IDs and attribution are serialized; live models, transactions and credentials are not.

## Implementation slices

1. Typed registration, codecs, validation, dispatch options and deterministic in-memory test backend.
2. Atomic Redis queue transitions for ready, leased, retryable and failed work; lease ownership must guard acknowledgement/renewal.
3. Worker concurrency, deadline propagation, heartbeat, cancellation, retry/backoff with jitter, and graceful drain.
4. Outbox publisher using the schema/contract from milestone 07. Mark delivery progress only after durable backend acceptance; tolerate duplicate publication through stable dispatch identities.
5. Middleware, uniqueness, rate limits, priorities with starvation control, batches/chains, cancellation and operational history/inspection.

## Failure and delivery guarantees

Workers can crash after a side effect and before acknowledgement. Redelivery is expected; document idempotency and provide stable execution/deduplication keys. Do not claim exactly-once job execution. Expired workers cannot acknowledge or renew a newer worker's lease.

Go cannot forcibly stop a goroutine that ignores context. On timeout, cancel the handler and track it until exit; do not immediately redeliver while that same worker is still knowingly executing it. Operational recovery may still cause duplicate effects after process/lease loss, which is why idempotency remains required.

Shutdown stops new reservations, cancels/drains running jobs within bounds, and safely releases or lets leases expire. Retry unknown payload versions only if policy says they are transient; otherwise retain them as inspectable failures rather than endlessly poisoning a queue.

## Acceptance

Test wrong payload compilation, atomic reservations across workers, crash/redelivery, stale ownership, heartbeats, context-aware and context-ignoring handlers, retry limits, uniqueness expiry, rate limits, priorities, chain ordering, partial batch failure, outbox rollback/publication crash, and shutdown. Verify no durable message depends on ephemeral request state. Apply the [common gate](README.md#common-completion-gate).


## Implementation checkpoint

Milestone 12 is accepted after native verification and consumer/source review.
The [jobs guide](../docs/guides/jobs.md) owns current contracts.

Implemented: typed capture/dispatch, queue bounds, memory/Redis authorities,
ownership fencing, heartbeat/drain, typed middleware and shared quota admission,
uniqueness windows, atomic single-queue chains/batches, cancellation and bounded
history/listing. The shared outbox has an additive progress migration and a
SKIP LOCKED publisher; job IDs survive acknowledgement gaps. Event publication
uses an ordinary framework-owned job and exposes typed delivery IDs to listeners.
Modules preserve infrastructure ownership. Independent consumer fixtures,
compiler-rejection cases and actual-gopls probes are present.

Deliberate contracts: workflows use one queue authority; no cross-queue atomicity
is claimed. Redis durability is conditional on its configured persistence/failover
policy. Memory cannot publish an outbox. Unknown publication outcomes retry stable
IDs only within a documented retention window. No exactly-once effect is promised.

Native verification on 2026-09-16, Go 1.27.1 darwin/arm64:

- Full `make verify` passed with required existing PostgreSQL/Redis and real gopls:
  root/consumer vet and tests, 818 rejected invalid API cases, 313 editor probes,
  six generated behavior documentation notices, current generation and docs.
- Job, memory, event, outbox, Redis and foundation races passed, along with the
  independent background consumer race check. Final Redis races passed after the
  last eligibility precision fix.
- A final `make verify` passed in 16.4 seconds with unaffected checks cached;
  recorded implementation/test source hashes matched afterward.
- Review regressions cover batch cancellation/retention, workflow capacity reuse,
  fractional Redis corruption without mutation, large workflow transport, quota
  admission, priority fairness, lost ownership and sub-millisecond eligibility.

Private logs and source manifests are under `.cache/milestone12-jobs/`. Tests use
isolated Redis keys and retained PostgreSQL schemas; no database reset was used.
Milestones 13–24 and the final framework-wide audit remain required; 25 is deferred.

## Source parity and consumer review

Reviewed `src/jobs/mod.rs`, `src/jobs/backend.rs` and `src/kernel/worker.rs` against
the delivered Go sources. Go transport is independently versioned; applications
retain concrete payloads and constructor-injected services.

| Rust behavior | Go contract |
| --- | --- |
| Typed jobs, dispatch on queues and at/after a time | `Definition[P]`, immutable `Pending[P]`, typed `Options[P]` and bounded envelopes |
| Retry budgets, backoff, deadlines and middleware | Frozen `Policy`, per-attempt typed middleware, classified history and retained terminal failures |
| Rate limits and unique windows | Existing shared typed limiter through admission; explicit typed unique keys with visible conflict results |
| Atomic claim, heartbeat, acknowledgement and crash recovery | Backend ownership proofs and expiry fencing; memory/Redis share the same behavioral suite |
| Queue priorities | Weighted queue opportunities provide lower-priority service without claiming global ordering |
| Batches, completion jobs and chains | Atomic single-queue workflow acceptance; completion waits for all successful members; cancellation keeps live members owned |
| Failure callbacks and job history | Typed middleware failure callbacks plus inspectable terminal records; callbacks are in-process, not durable effects |
| Worker/application lifecycle | Foundation worker module borrows infrastructure and drains callbacks before dependency shutdown |
| Transactional dispatch | Shared outbox publication with stable destination identities and additive progress migration |

Independent consumer code declares `Welcome`, its descriptor and a handler that
calls an injected domain service. Queue transitions, serialization, retries,
workflow progress and kernel ownership stay in Foundry. Compiler cases preserve
payload, identity, uniqueness and middleware ownership; editor probes inspect the
actual generic dispatch, workflow and module APIs. Editor/compiler execution
passed the milestone gate. Framework maintenance command composition and
additional test-harness conveniences remain owned by milestones 23/24.
