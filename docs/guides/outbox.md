# Transactional outbox

Runtime and consumer PostgreSQL races, vet, current generation, and clean-copy
generation/compilation have passed. Seven compiler-rejection cases and four
real-gopls probes also passed, followed by full repository acceptance. The [master roadmap](../../blueprint/00-master-architecture-and-parity.md) owns status.

An outbox stores a message in the same transaction as its business write. A
successful enqueue returns a typed message ID; the outer transaction still owns
commit or rollback. Enqueue does not invoke listeners or establish delivery.
Milestone 12 adds the [shared publisher and typed job/event delivery](jobs.md#transactional-publication); its acceptance status is recorded in the roadmap.

## Register once, publish a concrete DTO

Keep the existing typed event descriptor and declare one durable destination:

```go
var Durable = foundation.NewKey[*events.Outbox]("domain.outbox")

// Inside a provider's OnRegister:
err := events.RegisterOutbox(r, Durable, Bus, "domain.events")
```

`Bus` is the service key registered through `events.Module`. Its topic/listener
declarations remain the schema registry. `RegisterTopic` declares a schema when
the producer has no in-process listeners. Provider construction performs no I/O
and rejects repeated durable destinations, even across different buses or public
service keys. Independent applications have independent constructed producers.
Direct owners can use `PrepareOutbox(destination, bus)` and are responsible for
their own destination configuration.

The [consumer provider](../../tests/fixtures/consumer/eventqueries/durable.go)
maps a generated `PlainChanges` snapshot to `RecordCreated`, inside its model
observer's supplied transaction:

```go
id, err := Created.Enqueue(ctx, tx, producer, RecordCreated{ID: after.ID})
```

The ID is `outbox.ID[RecordCreated]`. An unrelated payload ID or model ID cannot
be passed to its typed reload. Passing a pool instead of `*database.Tx` to enqueue
fails compilation. The framework's internal model and generated query builder own
the INSERT, codecs and hydration; application code does not build SQL or envelopes.

## Commit, rollback and cancellation

Payload and immutable attribution are captured during enqueue. Later changes to
the caller's maps, slices or context do not replace that stored snapshot. Payloads
use the same strict typed JSON facility as ordinary events. Custom JSON methods
must be pure, concurrency-safe and return owned values.

The model write and outbox row commit together. Outer rollback removes both;
savepoint rollback removes its own records. Failure after enqueue, including a
later model observer failure, still rolls back the row. A storage error fails the
write transaction. Ordinary `AfterCommit` remains a separate process-local option
and does not provide crash durability.

An ID returned before outer commit is not a commit receipt. Retain the database's
explicit committed/unknown outcome semantics after a commit error; do not blindly
repeat business work. Delivery will be at least once, so receiving work must be
idempotent. Origin records attribution, never reusable authorization credentials.

## Typed reload and model fields

```go
result, err := Created.Find(ctx, db, producer, id)
// Handle err, then inspect the optional result.
stored, present := result.Get()
if present {
    payload, err := stored.Payload() // RecordCreated, error
    // Map the concrete payload deliberately to the required domain/response DTO.
}
```

Reload selects the expected destination, topic and schema version along with the
ID. A foreign address does not match. The concrete schema is checked again on
persisted data; malformed payloads or provenance fail without listener dispatch.
`Payload` returns a fresh decoded value. `Origin`, `CreatedAt` and `ID` expose the
captured metadata without exposing the heterogeneous storage envelope.

A handwritten model may store an `outbox.ID[RecordCreated]` field. The
[consumer model](../../tests/fixtures/consumer/eventqueries/event_link.go) verifies
generated setters, predicates and hydration retain that payload owner. UUID
behavior comes from the existing model ID implementation. `outbox.Message[E]`
is its exported type marker so generated codecs can name the complete Go type;
it does not contain a payload or represent a delivered message.

## Explicit framework migrations

Add `outbox.Migrations()` once to the definitions passed to `migrate.New`, together
with application and plugin definitions. Execute them through the ordinary
migration command. Creating an outbox producer never runs migrations or synchronizes
schema. Keep introduced definitions unchanged; later delivery features add new
migrations.

The table follows the connection's selected database schema, like ordinary model
tables. Migration execution and producers must target the same intended schema.
The fixture tests apply these unchanged definitions in unique retained schemas;
the separate migration-runner tests cover locking and history behavior.

The additive publication migration preserves the original persistence schema
history. Polling, queue acceptance and event delivery are covered in the
[jobs guide](jobs.md); publication success and handler success are distinct.

Migration `000003_add_claim_indexes` adds two partial indexes: one on
`(created_at, id)` for pending rows, serving the publisher's multi-route claim,
and one on `(published_at, id)` for published rows, serving retention pruning.
It uses `CREATE INDEX IF NOT EXISTS`, so operators of a very large existing table
can build both indexes with `CREATE INDEX CONCURRENTLY` beforehand instead of
holding a write lock while the migration runs.

## Publisher throughput, backoff and deployment

Each poll locks up to `MaxInFlight` (default 16) due rows across every route in
one short transaction with `FOR UPDATE SKIP LOCKED`, ordered by creation, and
publishes them with bounded concurrency; order within a batch is not preserved.
The batch's publications share `OperationTimeout`; the transaction keeps an
extra bookkeeping margin (a quarter of `OperationTimeout`, between 1 and 30
seconds) to record every outcome, so a publication that exhausts its deadline
counts as a failed attempt without rolling back the other rows. Caller
cancellation still rolls the whole batch back.
Idle polling starts at `PollInterval` (100ms) and doubles up to
`MaxPollInterval` (1s). Concurrent `PublishOne`/`PublishBatch` callers wait
briefly for a publication slot and then fail with `fault.Overloaded`.

A failed attempt retries after `RetryDelay` (1s) doubled per attempt up to
`MaxRetryDelay` (5 minutes), plus jitter in `[0, max(Jitter, delay/5)]`
(`Jitter` 1s; zero disables it). The default budget of 1,000 attempts keeps a
message retrying through roughly 83 hours of broker outage before it is marked
`attempt_limit`. `Run` never returns on transient SQL, transaction or
publication errors: it logs `outbox publication batch failed` (or a per-message
attempt failure) with a redacted diagnostic through the application logger,
backs off with jitter and continues. It returns only when its context ends or on
invalid use. Observer failures are logged and never stop `Run`.

`features.outbox.kernels` selects which running kernels also run the publisher
(`http`, `cli`, `worker`, `scheduler`, `websocket`). Empty keeps the default of
publishing under every kernel. Prefer `["worker"]` or `["scheduler"]` so CLI
commands and HTTP replicas do not poll the outbox. A direct owner uses
`publisher.KernelModule`; an application started without selecting kernels
publishes under every kernel.

## Operations: requeue and prune

`Publisher.Stats` counts rows by state. `Publisher.Failed` lists up to 100 of the
oldest failed rows as safe metadata (ID, kind, destination, name, version,
attempts, persisted reason; never payload, origin or error text).
`Publisher.Requeue(ctx, publisher.Selection{...})` makes failed rows pending
again with a fresh attempt budget and immediate eligibility: select every failed
row (zero selection), one `ID`, or one producer `Kind` such as `job` or `event`.
`Publisher.Prune(ctx, cutoff, limit)` deletes published rows completed before
`cutoff`; pending and failed rows are never pruned. Both change at most `Limit`
rows (1 to 10,000, default 1,000) per committed transaction; repeat until a
batch reports fewer rows. Keep the prune retention longer than any window in
which a consumer may reload a message by its outbox ID.

Register `outbox/command.Declaration` in the application's CLI registry with a
constructor returning `services.OutboxPublisher()`:

```sh
./service outbox stats --format json
./service outbox failed --kind job --limit 20
./service outbox requeue --all            # or --kind job, or --id MESSAGE_ID
./service outbox prune --older-than 720h
```

Configured applications can prune published rows automatically with the
[housekeeping schedule](production-operations.md#housekeeping-schedule) (`features.maintenance.outbox.retention`, default 30 days).

`requeue` and `prune` repeat bounded batches; requeue is bounded by the failures
present when it starts, so rows that fail again are not requeued repeatedly.
Delivery remains at least once: the stable destination identity still
deduplicates a row that was accepted before it failed.

## Publication failure classification

Publisher-owned SQL timestamps use microsecond precision regardless of the
application clock's precision. Eligibility reads and successful completion times
round down. A retry deadline adds the attempt's backoff delay to the original completion clock
sample, then rounds up to the next representable microsecond if needed. This
never makes a retry eligible before its intended deadline; rounding can add less
than one microsecond. Polling cadence can add further latency.

The generic clock and temporal values still retain nanoseconds. Explicit caller
values still pass through strict SQL codecs, which reject sub-microsecond data.
No application clock workaround, configuration change or schema migration is
required for this publisher correction.
The [B06 handoff and verification status](outbox-timestamp-b06.md) separates the
framework regression evidence from the remaining starter macOS/Linux acceptance.

The publisher treats a wrapped `fault.Invalid` error as a permanent failure.
`fault.Missing` (for example a job or event name/version not yet registered in
this process) stays transient: during a rolling deploy another replica may
already declare it, so the row retries with backoff. A job queue policy
mismatch (`jobs.ErrQueuePolicy`, for example a replica configured with a
different `QueueConfig` during a rollout) also stays transient, so a
misconfigured replica cannot permanently fail rows. Other failures retry under
the configured attempt budget.
Inspection is bounded and isolates custom error methods: cyclic, excessively
large/deep graphs or methods that panic or call `runtime.Goexit` remain retryable.
Custom `Is` and `Unwrap` methods must return; `Is` must make a shallow comparison.
The original failure stays in the in-process result without storing its text.

Post-commit observer errors, including malformed, panicking or Goexit-ing ones,
are logged with a bounded diagnostic; they cannot retain `Run` ownership, stop
`Run` or change a committed publication, and a later run can process pending
messages.
