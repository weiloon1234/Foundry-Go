# Typed events and transaction-aware dispatch

Runtime and consumer races, PostgreSQL, compiler-rejection, actual-gopls and
full repository checks have passed. The
[master roadmap](../../blueprint/00-master-architecture-and-parity.md) owns milestone status.

Define a concrete payload DTO and one reusable descriptor:

```go
type RecordCreated struct {
    ID model.ID[observerqueries.Plain] `json:"id"`
}

var Created = events.Define[RecordCreated]("records.created", 1)
```

The descriptor retains the payload type for listeners and publication. Its name
is a semantic identifier and its version is a nonzero schema version. Keep the
same descriptor across domain code. Register new versions explicitly when a
payload contract changes; a version is not a retry counter.

```go
err := Created.Dispatch(ctx, bus, RecordCreated{ID: record.ID})
```

Wrong payloads and model-owned IDs fail Go compilation. Converting a topic to
another payload owner is also rejected. Payload DTOs are independent of persisted
models: choose the fields to publish, and keep credentials out of them.

## Application assembly and listeners

Register `events.Module` through the same application builder used by all five
kernels. Use `events.RegisterListener` in a domain provider, resolving its service
dependencies once through `foundation.Resolver`. The returned `events.Handler[E]`
is an ordinary typed function; business handlers receive context and E directly.
The [consumer domain](../../tests/fixtures/consumer/eventqueries/events.go) shows
listener registration and a model observer without another service container.

Listener constructors are pure. They may resolve their own bus without creating
a construction cycle; Foundry binds all contributions before boot. A module owns
its bus startup. Separate applications receive separate buses and constructed
handlers unless the caller deliberately supplies a shared dependency.

Listener names are unique within a topic name/version. Multiple declarations can
contribute listeners to the same schema; conflicting payload types fail during
construction. Different versions can coexist. Registration order, including
provider dependency order, determines listener order. `RegisterTopic` permits an
intentional topic with no listeners; listener registration already declares its
schema, so it does not require another registration. Undeclared topics fail.

## Queued listeners, subscribers and test fakes

`events.Listen(name, handler).Queued()` (or `events.RegisterQueuedListener`,
`application.ListenQueued`) marks one listener to run as a job instead of
inline. Sync and queued listeners can be mixed for one event: at its position in
declaration order, each queued listener is enqueued with a fresh payload
snapshot and later runs with the job's retries and attribution; a failing sync
listener still stops the listeners after it. Queued delivery needs an
`events.ListenerQueue` per bus: create it with `events.NewListenerQueue(bus,
policy)`, register its `Declaration()` in the job registry and `Bind` the
dispatcher before dispatching. Dispatching an event with a queued listener and no
bound queue fails with `fault.Invalid`. Configured applications enable this with
`features.events.queued_listeners = true`, optionally selecting
`listener_connection` (default: the default job connection) and
`listener_queue` (default `default`); a worker must consume that queue.

A subscriber groups related listeners in one type: implement
`events.Subscriber` (`Declarations() ([]events.Declaration, error)`, typically
one `Topic.Declare` per event, mixing sync and queued listeners) and register it
with `events.Subscribe` for `Prepare`, `events.RegisterSubscriber` for a provider,
or `application.Subscribe(id, constructor)` for a configured application.

`testkit/events.NewFake(t, bus)` intercepts every dispatch on a production bus
for the rest of the test: listeners (sync and queued) do not run, while payloads
are still validated and captured. `Dispatched`, `AssertDispatched`,
`AssertNotDispatched` and `AssertDispatchedCount` decode the recorded payloads
of a typed topic and apply typed predicates without printing payloads. The fake
uses a module-internal interception seam (`Bus.Intercept` requires a token only
framework test helpers can obtain, so production code cannot suppress
listeners) and is removed at cleanup.

## Payloads, failures and ownership

Foundry snapshots the input through its existing strict typed JSON facility,
then decodes a fresh value for each listener. Maps and slices changed by one
listener do not alter the caller's input or the next listener's payload. Unknown
or malformed JSON and resource-limit violations fail at the shared codec boundary.
Omitted, NULL and zero still follow the declared DTO's JSON types and tags.

Custom JSON methods must be pure, concurrency-safe and return owned decoded
values. Capturing or decoding a payload is covered by callback failure isolation.
Panic and `runtime.Goexit` become ordinary framework errors without exposing
panic data. Custom codec errors retain the typed-JSON error behavior; ordinary
handler error identity remains available through `errors.Is`.

Listeners run sequentially. The first error stops later listeners. Earlier side
effects are not automatically reversed: pass a real transaction explicitly when
those effects must participate in it. Independent retryable work belongs to
jobs and durable outbox delivery, not an unmanaged background goroutine.

Directly prepared buses use `Prepare(config, declarations...)`, then `Start(ctx)`
and `Close(ctx)`. The application module handles this lifecycle automatically.
`DefaultConfig` admits up to 256 concurrent captures/deliveries and 32 active nested
dispatches. A top-level dispatch beyond `MaxInFlight` waits briefly (bounded by
its context and the shared five-second admission wait) and then fails with
`fault.Overloaded`, which HTTP maps to a retryable 503. A nested dispatch from an
active listener never waits for capacity held by its own ancestors; it fails
immediately with `fault.Overloaded` instead of deadlocking. Completed context
markers do not count as active recursion.

Shutdown rejects new work, cancels active work and waits for actual callback exit.
Successful listener completion still checks owner cancellation before returning.
An expired close-wait context does not abandon callbacks; `Done` closes only after
they return. Callers can wait again. A handler cannot wait for its own bus to close
using its active handler context. Custom code that ignores cancellation can delay
shutdown; Foundry must keep its dependencies alive until that code exits.

## Dispatching from a model lifecycle

The consumer registers a generated `PlainObserver` whose `Created` callback reads
the typed stored model from `PlainChanges`, then schedules its event:

```go
return Created.AfterCommit(ctx, tx, bus, RecordCreated{ID: after.ID})
```

`AfterCommit` requires a concrete `*database.Tx`, not a pool. It snapshots the
payload and immutable attribution when called. Savepoint rollback drops that
savepoint's events; outer rollback drops all its pending events. Successful
savepoints retain order until the outermost commit.

Execution uses the actual outer commit callback context and restores the
captured attribution. A canceled per-write context or later context attribution
does not replace that lifetime or change the captured origin. Model keys use
stored identity values; getters do not silently change them.

A listener failure after commit returns `database.AfterCommitFailed` with
`database.Committed` outcome. Committed data remains committed, and unrelated
later transaction callbacks still run. The
[PostgreSQL consumer](../../tests/fixtures/consumer/eventqueries/events_postgres_test.go)
checks the stored model after an injected listener failure.

These callbacks are process-local and have no crash durability. Milestone 07's
outbox will persist an enqueue record with the business write; milestone 12 supplies
its at-least-once delivery runtime. Job/WebSocket listener adapters arrive with
their owning subsystems, and milestone 23 consolidates typed event fakes and test
assertions. Runtime, consumer and failure tests accompany this event increment.
