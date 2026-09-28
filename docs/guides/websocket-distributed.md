# Distributed WebSocket runtime

Milestone 15 passed native verification, race checks and consumer review.
The local [channel/protocol guide](websocket.md) owns declarations, payload DTOs,
room authorization and protocol version 1. This guide adds distributed behavior.
The executable consumer is [realtime](../../tests/fixtures/consumer/realtime/orders.go).

## Assembly and ownership

Prepare the existing Redis client, then call `redis.NewWebSocketBackend(client)`.
`websocket.NewDistributed(registry, authentication, config, backend, cluster)`
constructs a socket hub. `Hub.Start(ctx)` confirms the live fan-out subscription
before upgrades are accepted. The WebSocket module starts this bridge before the
native HTTP listener and exposes readiness through `Hub.Ready(ctx)`.

Use `websocket.DefaultClusterConfig(namespace)` with the same application/environment
namespace, channel declarations, DTO metadata and authority limits in every process.
A namespace fingerprint rejects incompatible rolling configurations while leases or
history remain live. Plan compatible deployments or use an explicitly separate
namespace; the framework never clears stored state to accept new policy.

HTTP handlers and jobs use `websocket.NewPublisher(registry, config, backend, cluster)`.
The returned publisher is accepted by `Publish`, `Broadcast`, `DisconnectConnection`
and `DisconnectSubject`; it needs no socket listener or authentication scope. Reuse
one declaration/registry function across transports. A publisher does not subscribe
to fan-out. Close each publisher and stop each hub before closing the borrowed Redis
client. Construction starts no goroutines. `Stop` waits for actual owned work;
a canceled wait does not release a callback that continues to run.

## Limits, heartbeat and shutdown

`Config` owns local connection, IP, subject, subscription, frame, queue, presence-scope,
member DTO, deduplication, inbound rate and deadline bounds. `ClusterConfig` separately
owns cluster-wide `MaxConnections` and `MaxConnectionsPerSubject`, lease TTL,
operation timeout and pub/sub buffer limits. Private membership binds a subject slot;
one connection in several rooms for the same guard/provider identity counts once.
Pending admissions reserve capacity too. Anonymous connections consume global and
local/IP capacity but have no subject slot.

Every inbound frame, including malformed input, consumes the connection's configured
rate limit. The limiter uses the existing in-memory rate adapter and dies with its
connection. Pings require timely pongs. Clients must continuously read their socket
so their transport can respond to control frames. Fresh private authorization runs
independently of incoming handlers. A missed refresh deadline disconnects transport;
an uncooperative callback remains owned until it exits.

Shutdown seals admission, cancels operation contexts, drains already queued writes
within `DrainTimeout`, sends Going Away, and releases membership and domain cleanup
once. The same drain deadline bounds a write already in progress. Full outbound or
pending-admission queues disconnect the slow connection; publishers do not wait for
its reader. Bounds are validated together, including aggregate queue/history budgets.

## Publication, replay and acknowledgements

`Channel.WithReplay(websocket.ReplayConfig{Messages: 8, Bytes: 32768, TTL: time.Minute})`
retains a bounded recent **channel** bucket. Room replay filters that bucket, so
traffic in other rooms can evict its older events. Zero configuration disables replay.
Subscribe's optional `replay` integer requests at most the configured count; zero
requests none, and omission uses the channel count. After `subscribed`, retained
frames arrive oldest first with `replayed: true`. TTL/count/byte bounds all apply.

Both local and distributed delivery reuse stable `message_id` values. Each connection
has a bounded ID suppression ring. Live events arriving during admission/history
loading are buffered and merged after replay, avoiding duplicate overlap. Channel
broadcast reaches every membership once per connection; room publications reach only
that exact room. Whole-channel membership does not grant access to individual rooms.

`Incoming.AcknowledgeAccepted()` emits `accepted` after decoding and authorization,
before the handler runs. Handler success emits `ack`; failure emits a stable error.
`Incoming.Relay(outgoing)` reuses typed room/payload policy and ordinary publication.
Relay output must be registered on the exact channel declaration. A Redis publication
may reach clients before or after the handler's completion acknowledgement. Neither
acknowledgement proves recipient delivery, a transaction commit, or exactly-once work.

Redis pub/sub can lose messages. A confirmed stream gap or authority failure marks
an active hub degraded and terminates it; construct/start a new hub and reconnect
explicitly. There is no silent reconnect, fallback local cluster count or automatic
mutation retry. A failed publish may already have appended history or reached some
subscribers. Recent replay is not a durable cursor; there is no global order across
publishers or exactly-once guarantee. Clients may additionally deduplicate stable IDs.

## Presence and revocation

Presence stores only the declared safe DTO and an opaque guard/provider/stored-model
identity digest. Per-connection leases count tabs under a single subject. Last-tab
leave removes that subject; missed process cleanup expires by TTL. Heartbeat renews
live leases and reconciles locally observed presence scopes. Revisions prevent an
older response from overwriting newer counts. DTOs refresh on new joins, not on every
authorization refresh. `Presence.Members` and `Count` query the authority and return
errors during failure instead of inventing local authoritative counts.

Trusted server code calls `DisconnectSubject(ctx, publisher, guard, user.FoundryReference())`
after committed credential revocation, or `DisconnectConnection(ctx, publisher, id)`
for one connection. The generated reference preserves model/key ownership; a raw ID
cannot cross the typed boundary. Live disconnect delivery can be lost; periodic fresh
authorization remains the revocation backstop. Pending private admissions are included.

## Protected diagnostics and acceptance

`Diagnose(ctx, hub, guard, authorize)` requires an authenticated typed guard and an
explicit management authorization callback. Supply the existing HTTP authentication
scope. No public diagnostics endpoint is installed. Reports contain runtime/queue
counts, heartbeat/rate/revocation/forced-disconnect counters and per-channel incoming,
accepted, completed, failed, published, delivered and replayed counts; credentials,
raw model data and socket objects are excluded.

Acceptance sources cover two actual HTTP/WebSocket servers borrowing real Redis,
separate instance identities, fan-out and room isolation, duplicate delivery,
replay/live admission overlap, reconnects, safe multi-tab presence, global and subject
limits, typed revocation, TTL process loss, corruption/policy rejection, replay
count/byte/TTL bounds, transport gaps, idle revocation, heartbeat, rate limiting,
shutdown drain, slow queues and ownership under blocked callbacks. Compiler-negative
and real-gopls consumer cases cover the new typed public boundaries. The [master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-15-verification-and-consumer-review)
records the completed acceptance gate and review corrections.
