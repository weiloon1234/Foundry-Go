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

## Publication failure classification

The publisher treats wrapped `fault.Invalid` and `fault.Missing` errors as
permanent failures. Other failures retry under the configured attempt limit.
Inspection is bounded and isolates custom error methods: cyclic, excessively
large/deep graphs or methods that panic or call `runtime.Goexit` remain retryable.
Custom `Is` and `Unwrap` methods must return; `Is` must make a shallow comparison.
The original failure stays in the in-process result without storing its text.

Publisher shutdown also bounds cancellation matching over transaction and post-commit observer errors. Malformed observer errors cannot retain `Run` ownership or change a committed publication; callback panic/Goexit produces a safe failure, and a later run can process pending messages.
