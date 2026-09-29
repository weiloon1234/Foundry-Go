# WebSocket channels and protocol

Milestone 14 passed native acceptance, race checks, compiler/editor checks and
bounded protocol fuzzing. The
[roadmap](../../blueprint/00-master-architecture-and-parity.md) owns acceptance
status. The [independent consumer](../../tests/fixtures/consumer/realtime/orders.go)
shows typed declarations, a domain service and the WebSocket kernel. Distributed
fan-out, TTL presence, revocation refresh and bounded replay are described in the
[distributed runtime guide](websocket-distributed.md); the roadmap records their
separate milestone 15 acceptance.

## Declare channels and events once

`websocket.Channel[Owner, Room, Subject]` retains a nominal channel owner, room-key
type and authenticated model. Give each channel its own owner type. `Public`
uses `websocket.Anonymous`; `Private` requires a typed guard and an explicit
authorization callback. `DefineRooms` reuses HTTP text codecs and their existing
scalar metadata, including model-owned IDs, natural keys and enums. Room strings
must round-trip canonically, be nonempty, contain no controls and fit 512 bytes.

`DefineIncoming(channel, eventID, generatedDTOJSON)` declares a client event.
Optional `Authorize` evaluates a payload-aware policy; `Handle` accepts exactly
that channel's room, subject and payload types. `DefineOutgoing` declares server
events. Pass incoming handler bindings and outgoing `Registration()` values to
`Register(channel, events...)`, then `NewRegistry`. Construction starts no I/O.

The [consumer](../../tests/fixtures/consumer/realtime/orders.go) uses existing
generated `httpdto.OrderResponseJSON()` for both transport directions. DTOs own
their wire contract; persistence models do not become implicit payload schemas.
Different directions use distinct event IDs. Registries reject duplicates,
invalid descriptors and events from another nominal declaration, including a
second declaration reusing the same Go owner and string name.

`RawIncoming` and `RawOutgoing` are explicit dynamic `json.RawMessage` escape
hatches. They retain channel ownership and bounded strict JSON parsing, but have
no typed payload schema. Contract metadata marks them dynamic.

## Publishing and routing

```go
messageID, err := websocket.Publish(ctx, hub, Orders, orderID, OrderUpdated, reply)
messageID, err = websocket.Broadcast(ctx, hub, Orders, OrderUpdated, reply)
```

Handle returned errors. Room publication reaches only that exact room's
subscriptions. Whole-channel subscribers do not implicitly receive every room.
Broadcast reaches all channel subscribers, including room subscriptions, once
per connection. `MessageID` identifies the event and is not an authentication or
idempotency token. Publication is a trusted server-side operation; expose it to
clients only through an authorized typed handler.

Success records local publication/queue admission attempts. It does not certify
that every peer received or processed a frame. `websocket.NotPublished(err)`
reports an error that happened before anything was published or retained
(validation, local admission, a closing hub, encoding), so a retry cannot
duplicate the event; any other error may follow partial distributed publication
or history. Full output queues disconnect slow
connections. There is no global order, durable history or exactly-once guarantee.
Routing visits only the subscribers indexed under the published room (or the
channel's rooms for a broadcast), not every connection.

`websocket.ExceptConnection(id)` skips live delivery to one connection, and
`Incoming.RelayToOthers(outgoing)` relays a client event to every subscriber except
its sender ("to others"); recent replay still includes the event. Trusted
publication, presence inspection and disconnect operations share
`Config.MaxOperations` (default 1024), separate from socket capacity: callers wait
briefly for a slot and then receive `fault.Overloaded` (HTTP 503) instead of an
immediate capacity error.

## Authentication and room ownership

`New(registry, authentication, config)` borrows the existing HTTP authentication
adapter. It captures declared bearer/cookie inputs at the handshake. Each subscribe
creates and closes a fresh auth scope: provider eligibility, credential validity
and channel authorization run again before admission. Incoming messages reuse the
connection's authorization freshness window instead: the connection's current auth
scope (with its cached guard resolution) and each subscription's last authorized
subject and target. The periodic refresh (`AuthRefreshInterval`, jittered by ±10%)
re-resolves credentials in a new scope and re-authorizes every private
subscription; success replaces the cached scope and results, failure disconnects,
and a refresh that misses its window closes the socket. The window is
`AuthRefreshInterval` × 1.1 + `OperationTimeout` + `PongTimeout`, plus
`ClusterConfig.ConnectionTTL`/3 for a distributed hub (a ping and a lease renewal
may run first on the same timer). A successful refresh extends the window before
it waits for in-flight messages to release the previous scope, so that wait never
revokes a fresh connection. A revoked credential can therefore still send messages until the next
refresh. The cached subject lives in memory only for that window; it is never
exported or persisted. Event policies run after payload decoding on every message.
Verified guard attribution reaches domain handlers. Only immutable request
metadata is copied from the handshake; request context values and inherited
model/system authority are discarded. Unsubscribe can always release an existing
subscription.

For user-scoped channels, use `OwnedRooms` with the guard and generated zero
model's `FoundryReference()`. It compares the room to the guard's verified stored
key and forbids whole-channel subscription. Matching IDs from another guard do
not establish authority. The consumer's `UserInbox` shows this composition.

Browser applications pass `BrowserSessions.Authentication()`. Capture reuses its
secure-cookie and credential validation. Session login/rotation remain ordinary
HTTPS endpoints; reconnect after changing credentials. No long-lived secret is
accepted in a URL or protocol payload. All upgrade query strings are rejected.
In addition to incoming operation checks, private subscriptions now refresh
authorization independently of handlers. The distributed guide covers this
revocation backstop and typed cluster disconnect.

## Origins and HTTP integration

Clients must request the `foundry.v1` WebSocket subprotocol. Origin validation
accepts same-origin requests or exact configured HTTP(S) `AdditionalOrigins`.
Null/opaque origins, duplicate headers and wildcard trust are rejected. Native
clients without `Origin` require explicit `AllowOriginless`. The shared HTTP
origin parser checks scheme as well as authority. Configure trusted proxies and
public-host admission through existing HTTP middleware before the upgrade route.

`websocket.Module` registers a typed Hub service and `foundation.WebSocket`.
Its `ServerConfig` supplies the HTTP listener settings, exact path and standard
HTTP middleware. `Hub.Ready(ctx)` reports the module's actual bound address. The
existing HTTP server owns each upgrade handler until all connection work exits.

`SharedModule(name, key, serverKey, requires, construct)` instead serves the hub
from an existing HTTP kernel: mount `websocket.Route(hub, path, middleware...)` on
that router. Boot starts the hub, `Ready` reports that server's address, and the
hub drains its sockets when the application lifetime ends. `WithLogger(logger)`
(the modules default to the application logger) records cluster degradation,
stream loss/recovery and terminal faults with redacted diagnostics.

For manual hosting, mount `hub` as a native `http.Handler` at the intended path,
keep its borrowed dependencies alive, and call `hub.Stop(ctx)` during shutdown.
The connection has its own lifetime; an HTTP request timeout after hijacking does
not become the socket deadline. The transport library is
[coder/websocket](https://pkg.go.dev/github.com/coder/websocket); Foundry owns
channel authorization, protocol, queues and application lifecycle.

## Version-one frames

The [recorded conversation](../../websocket/testdata/protocol-v1.json) is exercised
by a real-socket compatibility test. Frames are JSON text, with exact names:

```json
{"v":1,"action":"subscribe","id":"join-1","channel":"chat","room":"1"}
{"v":1,"action":"message","id":"send-1","channel":"chat","room":"1","event":"send","payload":{"text":"hello"}}
{"v":1,"action":"unsubscribe","id":"leave-1","channel":"chat","room":"1"}
```

`room` is omitted for the whole channel; null and empty are invalid. IDs are
bounded semantic identifiers, up to 128 ASCII lowercase letters, digits, dots,
underscores and hyphens, starting with a letter or digit. Request IDs correlate
responses; reusing one does not prevent a second execution.

Server response types are `subscribed`, `unsubscribed`, `ack`, `error`, `event`
and the three presence changes. Every response has `v: 1`. Replies carry the
request `id`, channel and optional room. Events carry the declared event and
`message_id`. `ack` means the handler completed successfully; it does not mean
that subscribers received publications made by that handler. Failed input,
authorization and handler execution produce stable codes without underlying
error strings, credential values or panic data.

Duplicate keys, case-folded field names, unknown envelope properties, invalid
Unicode (including unpaired surrogate escapes), undeclared action fields and
unsupported versions are rejected in one strict decoding pass. Error replies carry
the request `id` whenever it was decoded, including `malformed` replies. The rate
limit is checked before a frame is decoded, so a `rate_limited` reply carries the
`id` only when it appears among the frame's first four scalar members, as
generated clients send it (`v`, `action`, `id`). Binary or oversized frames close the connection. Subscribing twice is an
error and does not run the join hook twice. Messages require the exact
subscription and cannot invoke a server-only event. The `unavailable` code marks
a retryable transient failure, such as a cluster authority timeout or exhausted
management capacity; retry the same operation later.

## Presence and lifecycle hooks

`WithPresence(privateChannel, generatedSafeDTOJSON, mapper)` produces a typed
presence declaration. Register its `Channel()` and use its trusted server-side
`Members(ctx, hub, target)` helper for typed results. The mapper explicitly chooses
public fields; persistence models are rejected by the JSON contract. Stored state
contains only safe DTO bytes, opaque member IDs and immutable subject references.

Local member identity includes guard, provider and stored model identity. Tabs
share one member with a connection count within that exact channel/room. First
join emits `presence_joined`; count/data changes emit `presence_updated`; final
leave emits `presence_left`. The subscription reply contains the current bounded
member list. New subscriptions refresh the member DTO from their fresh subject.
Local counts describe one Hub. Distributed hubs use the shared authority described
in the [distributed presence guide](websocket-distributed.md#presence-and-revocation).

`WithHooks` supplies typed `Joined` and `Left` callbacks. Joined runs before
subscription admission. A successfully returned join gets exactly one leave,
including cancellation before admission commits. A failed join is responsible
for its own partial domain effects. Left receives room/connection/subject-reference
metadata, never a cached authentication model. Cleanup removes membership before
calling Left. An unsubscribe hook failure does not undo that removal; disconnect
cleanup failures increment operational failure counts.

## Bounds and ownership

Start with `DefaultConfig`. It bounds connections (10,000 per process),
subscriptions, inbound (64 frames) and outbound (256 frames) queues, frame/payload
parsing, callback/write timeouts, presence members and member DTO bytes. Queued
bytes are enforced when they are queued: `MaxQueuedBytes` (default 1 MiB) bounds
one connection's inbound, outbound and pending-admission frames and
`MaxTotalQueuedBytes` (default 256 MiB) bounds all connections. Exceeding a
connection's own budget disconnects that connection. Exhausting the hub budget
disconnects the connections actually holding it, largest queue first, releasing
their queued bytes at once, so a reading subscriber whose own queue is small is
not disconnected because others stopped reading; the connection being enqueued to
is disconnected only when it holds the most. Publishers never block. Presence limits
must fit a single subscription response. One reader, serialized writer, sequential
operation worker and one maintenance timer (pings, authorization refresh, cluster
lease renewal) own each connection. Frame overflow closes the socket even while
an application callback is still exiting.

`MessageRate` (default 128 frames per second) is a per-connection token bucket
with a burst of `Requests`, refilled continuously and measured on the monotonic
clock, so wall-clock steps never refill it or disconnect clients. Every frame,
including malformed input, consumes a token. `DescribeClient` exports
`inbound_queue` and `message_rate`; generated TypeScript clients keep at most
`min(subscriptions, inbound_queue)` operations in flight, reject locally with
`rate_limited` before exceeding the rate, and treat uncorrelated error replies as
non-fatal (only `unsupported_version` closes the client).

Callbacks, codecs and custom error inspection isolate panic and Goexit. Framework
error traversal is bounded to 256 nodes and 64 levels per inspection. Cyclic or
oversized handler errors return `operation_failed`; malformed room codec results
remain `malformed`. Transient cluster failures fail only their operation with
`unavailable` and mark the hub degraded until an operation succeeds; see the
[distributed guide](websocket-distributed.md). Custom error methods must still return.
Deadlines
cancel contexts, but cannot kill arbitrary Go code. A callback ignoring cancellation
retains connection/public-operation capacity and dependencies until actual exit.
`Stop` permanently seals admission, cancels operation contexts and drains queued
writes within `DrainTimeout`; `Done` waits for actual cleanup. Self-wait from an
active callback returns `fault.Cycle`. Ordinary connection failure can discard
pending output; shutdown drain does not promise delivery to a non-reading peer.

`Snapshot` reports local counts without credentials or model objects, including
queued bytes, operation overloads and (distributed) stream state. `Hub.Probe(ctx)`
is an I/O-free readiness check: it fails while stopping, after a terminal fault or
while a distributed hub's fan-out stream is not subscribed. Protect any
operational endpoint exposing channel/member inspection. `Registry.Channels()`
returns owned, deterministic metadata from the runtime's exact descriptors for
the shared client-contract exporter in milestone 21.

The [protocol test client](../../testkit/websocket/client.go) performs bounded real
HTTP/WebSocket I/O for independent fixtures. Verification covers typing, room
isolation, protocol failures, origin policy, auth freshness, safe presence,
cancellation/races, native kernel ownership and the common compiler/editor gate.
