# Typed pub/sub

Pub/sub supplies ephemeral fan-out between framework instances and kernels. Declare
the resource key and concrete payload once; Foundry owns serialization, readiness,
buffers, receive loops, heartbeats and shutdown. Jobs and the transactional outbox
remain separate delivery mechanisms.

## Declare a topic

The [consumer example](../../tests/fixtures/consumer/messaging/members.go) compiles
from an independent module and deliberately selects the model getter:

```go
type MemberChange struct {
    Email DisplayEmail
}

var MemberChanged = pubsub.Define[model.ID[Member], MemberChange](
    "member-changed", 1, keyspace.TextKeys[model.ID[Member]](),
)

topic, err := MemberChanged.Bind(broker)
if err != nil { return err }
subscription, err := topic.Subscribe(ctx, member.ID)
if err != nil { return err }
defer subscription.Close(context.Background())

email, err := member.AccessEmail()
if err != nil { return err }
subscribers, err := topic.Publish(ctx, member.ID, MemberChange{Email: email})
if err != nil { return err }
change, err := subscription.Receive(ctx)
```

`Member`, `DisplayEmail`, and the domain work belong to the application. In the
consumer fixture these types live in `mutatorqueries`. The compiler rejects a
different model's ID, a different payload struct, and a raw string where the getter
returns a named presentation value. Publishing a model does not automatically make
that model a transport DTO.

`Declaration[K,V]` and `Topic[K,V]` retain both types. `Subscription[V]` returns the
concrete payload; opaque `Channel`/`Message` values exist for adapter integrations.
Names and positive schema versions define compatibility. Reuse one declaration
value: a different declaration with the same name/version is rejected within a
broker, even when its Go types happen to match. A new version isolates an
incompatible payload contract on a different physical channel.

## Broker construction and application ownership

```go
broker, err := pubsub.NewBroker(client, pubsub.DefaultConfig(namespace))
```

Here `client` is an existing `*redis.Client` and `namespace` is a shared
`keyspace.Namespace`. Construction performs no I/O and borrows the adapter.
`pubsub.Module` registers the broker with explicit provider dependencies so its
subscriptions and callbacks drain before the adapter closes. The
[consumer provider](../../tests/fixtures/consumer/messaging/redis.go) reuses the
existing Redis connection provider.

For local tests, use `pubsub/memory.New(maxSubscriptions)` with the same broker and
declarations. Memory authorities share only through the same explicitly passed
backend. There is no process-global registry and no automatic Redis failure fallback.

`Topic.Subscribe` returns after confirmed readiness. Its context bounds key
encoding and establishment; it does not own the returned subscription's lifetime.
Close a subscription when finished, or let the broker/application own it until
shutdown. There are no implicit wildcard or cross-namespace subscriptions.

`Topic.SubscribeKeys(ctx, first, rest...)` subscribes several keys of one topic in a
single subscription whose `Receive` returns `pubsub.Delivery[K, V]{Key, Value}`, so
the typed key each payload was published under is preserved. Duplicate keys fail with
`fault.Duplicate`; at most `Config.Buffer.Channels` keys are accepted. Keys are
retained for delivery and must stay immutable while the subscription lives.

Typed pattern subscriptions are not offered: logical keys are hashed into channel
addresses, so a pattern cannot select typed keys, and a whole-topic wildcard would
deliver payloads without their typed key. Subscribe to explicit keys instead.

## Supervised subscriptions and gaps

`Topic.Supervise(ctx, key, policy, receive, gap)` keeps one key subscribed until
`ctx` ends, the broker closes or a callback fails. After a terminal interruption
(disconnect, overflow or an undecodable payload) it re-subscribes with jittered
exponential backoff (`pubsub.DefaultSupervisePolicy()`: 100ms doubling to 30s).
Once the **new** subscription is confirmed ready it calls `gap` with a
`pubsub.Gap{Cause, Attempts}` before delivering anything else, so the application
can reconcile from its source of truth without missing a change published after
readiness. A failed initial attempt is reported the same way. Payloads published
while `gap` runs are queued within the buffer limits and delivered afterwards, so
`receive` must tolerate a change that the reconciliation already reflected.

```go
err := MemberChanged.Supervise(ctx, member.ID, pubsub.DefaultSupervisePolicy(),
    func(ctx context.Context, change MemberChange) error { return apply(ctx, change) },
    func(ctx context.Context, gap pubsub.Gap) error { return reloadMember(ctx, member.ID) },
)
```

Invalid keys, cycles and contained callback panics end supervision instead of
retrying; handler errors are returned (wrapped, preserving `errors.Is`). Capacity
exhaustion and transport failures are retried, including every
`pubsub.ErrDisconnected` during establishment (a refused dial, a connection that
closed while subscribing, or a protocol anomaly on the shared connection), so a
Redis restart only produces a gap. Only the explicit shutdown sentinel
`pubsub.ErrClosed` is permanent: supervision returns `ctx.Err()` on cancellation
and `pubsub.ErrClosed` on broker or adapter shutdown. It never hides
a gap: Redis pub/sub is at-most-once, so payloads published during an interruption
are lost and the `gap` callback is the application's signal to resynchronize.

## Delivery and failure semantics

`Publish` returns the authority's subscriber count. The memory backend counts
subscriptions; Redis counts subscribed connections, and every typed subscription
of one client shares one connection, so a process counts once per client. This
does not acknowledge application processing or even successful delivery into
every consumer's buffer.
Zero subscribers is a valid publication. A lost publisher acknowledgement returns
an error with an unknown outcome; Foundry never retries the mutation automatically.

`Receive` permits one active call per typed subscription, including decoding. A
concurrent receive returns `fault.Conflict` rather than accumulating waiters.
Its context bounds that receive attempt. Canceling a receive leaves a live
subscription available for a later call; cancellation after dequeue may consume
the in-flight message. No automatic retry or replay is implied.
Cancellation matching inspects at most 256 error nodes and 64 unwrap levels.
Unclassifiable cyclic/deep/wide errors terminate the subscription and retain their
original cause; panic/Goexit from error methods becomes a contained callback failure.
Custom methods must return, and this bound does not make arbitrary traversal of a
returned cause safe for application code.

Each subscriber receives an independent typed JSON snapshot. Foundry reuses
`value.NewJSON` and `value.ParseJSON`: unknown/duplicate fields, missing required
fields, invalid Unicode, inappropriate nulls, and resource bounds follow the same
rules as other typed JSON values. Custom JSON methods own their representation
and must preserve inputs and honor the framework's callback requirements.

Queue overflow returns `pubsub.ErrOverflow` and terminates that subscription,
discarding queued messages. Oversized/malformed payloads and unexpected channels
also terminate it. Network/heartbeat failures report `pubsub.ErrDisconnected`;
other unexpected protocol transitions likewise terminate with an explicit error.
The framework never silently drops a message and resumes as if delivery continued.
Applications can inspect `Err()` and `Done()` even when they are not calling Receive.

`Close(ctx)` starts cleanup even if its wait context is canceled; canceling the
wait does not abandon cleanup. `Done()` closes after owned work exits and broker
capacity is released. Broker shutdown cancels publication/setup work, closes all
streams, and waits for active decoders and callbacks. An uncooperative custom
callback can delay completion and retains its slot until it returns. Callbacks
receiving an operation context cannot synchronously wait for their own broker's
shutdown; that cycle is rejected. Do not bypass this rule by capturing an unrelated
background context inside a callback.

## Redis behavior

Redis [pub/sub is at-most-once](https://redis.io/docs/latest/develop/pubsub/#delivery-semantics):
messages can be lost during interruption. The framework's bounded queue and explicit
termination make local loss observable; they cannot recover messages Redis never
delivered. Bounded WebSocket replay belongs to milestone 15, and durable recovery
remains deferred. Pub/sub does not promise an outbox, delivery receipts, global
ordering across publishers, or exactly-once processing.

Redis database numbers [do not isolate pub/sub](https://redis.io/docs/latest/develop/pubsub/#database--scoping).
Foundry always embeds application/environment, feature, declaration and schema
version in its channel address. Tests verify cross-database delivery for the same
owned namespace and isolation for a different namespace.

All subscriptions of one `redis.Client` share one dedicated subscription
connection with a single reader and a single heartbeat goroutine; a subscription
owns no connection or goroutine of its own (the typed broker adds one watcher per
subscription so termination is visible without a receiver). A channel already
confirmed on the connection is ready immediately; a new channel waits for its own
SUBSCRIBE acknowledgement. Acknowledgements are matched to commands in send order
and the server's running subscription count is checked exactly. When the last
stream using a channel closes, the adapter unsubscribes it; when the last stream
closes, the connection closes, so an idle client holds no subscription connection.
Each stream keeps its own bounded buffer: an overflowing or over-bound stream ends
alone while other streams on the connection keep receiving.

The adapter explicitly checks every Subscribe error and acknowledgement; it does
not use the driver's convenience channel, which may hide drops. Any receive error,
heartbeat loss, failed command write or unexpected protocol frame ends the shared
connection, and **every** stream on it terminates with `pubsub.ErrDisconnected`
(joined with the cause). The driver may attempt reconnection before returning an
error; Foundry still reports the interruption and never resubscribes implicitly.
A later subscription transparently uses a fresh connection; use `Supervise` or
re-subscribe explicitly after handling the gap.

An owned heartbeat sends PING after each operation-timeout interval and requires a
matching PONG within the next interval. Normal idle topics remain subscribed.
Receive itself has no idle timeout: a timeout halfway through a RESP frame must
not be mistaken for harmless idleness. Closing the connection unblocks the reader.
Client shutdown owns establishment, heartbeat, reader and connection cleanup
before reporting completion.

`redis.Config.MaxSubscriptions` defaults to 64 and bounds live and draining
subscription streams per client; ordinary `MaxConnections` remains the
command-pool limit. A subscription beyond the bound fails immediately with
retryable `fault.Overloaded` (subscriptions are long-lived, so the bound does not
queue). `Client.Stats()` reports subscriptions and the live subscription
connection separately from ordinary `Open`/`Idle` connections. Establishment and
publication use the client's queued operation admission. Published payloads also
respect the configured Redis value-byte limit.

## Bounds and verification

Broker defaults allow 256 declarations, 1,024 logical key bytes, 128 concurrent
publication/setup operations, 64 subscriptions and a five-second operation timeout.
A publication or setup beyond `MaxConcurrent` queues in FIFO order for at most the
operation timeout (capped at five seconds) and the caller's deadline, then returns
retryable `fault.Overloaded` (HTTP 503 with `Retry-After`); broker shutdown ends
queued waits with `pubsub.ErrClosed`. An operation nested inside the broker's own
callback never waits. `MaxSubscriptions` (and the memory adapter's capacity) fail
fast with `fault.Overloaded` instead of queueing, because subscriptions are
long-lived.
Each default buffer permits 64 pending messages, four MiB of queued payloads, and
64 KiB per payload. Adapter batches default to at most 64 exact channels. Bounds
apply per broker/adapter as documented; they are not a measurement of process RSS.

Physical addresses reuse `keyspace` validation and hashing. Queues own their byte
buffers and use bounded rings. Serialized payload limits do not constrain arbitrary
allocation inside custom JSON methods or an untrusted Redis server's RESP parser;
the Redis endpoint and its authorized publishers remain part of the infrastructure
trust boundary. Payload length is checked before the adapter makes an additional
byte copy or enters the delivery queue.

Shared memory/Redis tests cover readiness, exact-topic fan-out, independent bytes,
cancellation, close and invalid setup. Redis tests cover multiple clients, database
and namespace scope, overflow, heartbeat, dropped connections, interrupted setup,
unknown publish outcomes and draining shutdown. Typed tests cover JSON validation,
callback ownership, provider ordering, compiler rejection and real-gopls discovery.
Full native verification passed in 430.4s with 2012 matching source inputs, required local PostgreSQL and Redis, all 675 compiler cases, 247 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Fresh focused native races, consumer checks and 1,519,877 queue fuzz executions also passed. No dependency was added.
Milestone 09 remains open for the additional source-parity work in its blueprint.
