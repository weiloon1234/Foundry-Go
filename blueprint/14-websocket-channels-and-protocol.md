# 14 — WebSocket channels and protocol

## Purpose and prerequisites

Prerequisites: [03](03-generation-and-language-tooling.md), [08](08-http-validation-and-responses.md), [10](10-model-first-authentication-and-authorization.md). Give realtime its own typed transport and lifecycle, matching the breadth of Rust Foundry.

Rust references: `src/websocket/mod.rs`, `src/kernel/websocket.rs`, `src/contract/mod.rs`; `tests/phase2_acceptance.rs`, `websocket_observability_acceptance.rs`; `docs/guides/websocket.md`. Inspect Starter's `src/realtime/mod.rs` for consumption patterns, not as an authorization implementation to copy blindly.

## Package boundaries and typed contracts

The WebSocket package owns channel registration, protocol decoding, authorization integration, per-connection state and publishing. Distributed transport and replay are completed in [15](15-websocket-distributed-behavior.md).

- A channel descriptor binds its room-key type, access policy and event registry.
- Each incoming/outgoing event descriptor binds channel ownership, event ID, direction and payload type.
- Typed incoming handlers receive a decoded payload and authenticated subject through the adapter, without JSON field lookup or Actor conversion.
- Publishing binds a payload to its declared event and a correctly typed room key. Wrong channel, room or payload types must fail compilation.
- Raw JSON channels are an explicit dynamic escape hatch excluded from claims of full payload typing.

Verified publishing shape:

```go
messageID, err := websocket.Publish(ctx, hub, orderChannel, orderID, orderUpdated, update)
```

The referenced descriptors are declared once in Go and contribute to the shared manifest. HTTP handlers, jobs and CLI operations use the same publisher contract.

## Protocol and access decisions

Define a versioned JSON text-frame envelope with explicit action, request ID, channel, optional room, event and payload. Actions cover subscribe, unsubscribe and typed client messages. Server envelopes cover subscription results, events, acknowledgements and stable errors. Validate version, identifiers and payload size before dispatch.

Public channels require no credential but still validate payloads and limits. Private channels require a typed guard and authorization. User-scoped rooms enforce identity ownership; a matching guard alone does not grant access to every room. Presence channels expose an explicitly declared safe member DTO, never the full auth model.

Channel-wide publication reaches all subscribers in that channel. Room publication reaches only that room's subscriptions. Make this routing rule explicit in tests and client documentation.

Browser authentication uses existing secure sessions or short-lived credentials through a defined handshake/subscription flow. Do not put long-lived bearer secrets in query strings. Recheck authorization on subscription and privileged messages; revocation support is integrated in milestone 15.

## Implementation slices

1. Protocol specification and recorded frame fixtures, typed descriptors and serialization codecs.
2. WebSocket kernel/HTTP upgrade integration with standard HTTP middleware compatibility.
3. Single-instance channel/room subscriptions and server-side publishing.
4. Typed handlers, per-event authorization, public/private/presence helpers and lifecycle hooks.
5. Contract metadata and a minimal protocol test client for independent fixtures.

## Failure behavior and acceptance

One reader and a serialized writer own each connection; message handlers enqueue through bounded interfaces rather than write frames concurrently. Bound frame size and inbound work before expanding to distributed tests. Disconnect cleanup is idempotent.

Compile-test wrong room/event/payload types. Protocol-test malformed frames, unsupported versions, unknown events, direction violations, unauthorized room access, cross-guard ID collisions, duplicate subscriptions, unsubscribe/disconnect races, origin restrictions and safe presence DTOs. Every built-in notification/private-channel helper must have ownership tests. Apply the [common gate](README.md#common-completion-gate).

## Current implementation and source review

The [guide](../docs/guides/websocket.md) documents the concrete typed API and
version-one protocol. Milestone 14 implementation, test sources, independent
consumer examples and documentation preceded compilation/testing. Native
acceptance and final-source verification passed on 2026-09-16.

`websocket` owns nominal `Channel[Owner,Room,Subject]` declarations, typed incoming
and outgoing events, frozen registries, room routing, fresh authentication scopes,
safe presence DTOs, bounded connections/queues and lifecycle. `testkit/websocket`
provides a real-socket protocol client. Metadata comes from existing generated
JSON contracts and HTTP scalar codecs. The WebSocket kernel reuses the native HTTP
server/router/middleware and foundation ownership instead of adding a second
application lifecycle. HTTP credential capture reuses the existing bearer/cookie
and browser-session input validation.

Inspected Rust `src/websocket/mod.rs`, `src/kernel/websocket.rs`, the WebSocket
guide and Starter's read-only `src/realtime/mod.rs`. Starter application guards and
notification registrations were treated as consumption examples, not copied as
framework access policy. Distributed behavior remains in milestone 15.

| Rust capability | Go implementation |
| --- | --- |
| Typed channel/event descriptions | Nominal owner, room, subject and payload types; same descriptors own runtime metadata |
| Subscribe/message/unsubscribe envelopes | Strict version 1 JSON text frames and required `foundry.v1` subprotocol |
| Server publication and rooms | Exact-room publication; channel-wide fan-out deduplicated per connection |
| Guards and channel authorizers | Fresh typed auth scope per operation; payload-aware incoming event policy |
| User/notification room ownership | `OwnedRooms` requires the verified stored key and forbids whole-channel subscriptions |
| Presence and lifecycle | Explicit safe DTO; local subject/tab counts; owned join/leave cleanup and no retained auth model |
| WebSocket kernel | Standard HTTP upgrade handler, exact router path/middleware, foundation kernel and real listener readiness |
| Failure isolation and limits | Bounded frames/queues/connections/subscriptions/DTOs, owned deadlines and panic/Goexit isolation |

The transport dependency is `github.com/coder/websocket` v1.8.15, confirmed against
its published module versions and official API documentation. It fills a previously
absent dependency role and has no module dependencies. Root/consumer requirements
and checksums align. Foundry owns protocol and feature behavior.

Deliberate boundaries: there is no Rust wire compatibility, query credential flow,
implicit authorization from attribution, persistence-model payload exposure,
exactly-once delivery or durable reconnect cursor. Live Redis distribution,
heartbeat/rate enforcement, proactive revocation, replay and graceful bounded
write drain remain milestone 15; notification delivery integration belongs to 17.

## Verification and consumer review

Native macOS Go 1.27.1 `make verify` passed with required existing PostgreSQL/Redis,
827 compiler-rejection cases, 319 real-gopls probes and six field-behavior notices.
Root/consumer vet/tests, eight current-generation checks and documentation passed.
Focused WebSocket/HTTP and consumer kernel races passed. Bounded protocol fuzzing
completed 42,636 executions with two workers.

The source/consumer review confirmed generated DTO/scalar reuse, exact declaration
identity, fresh auth, room isolation, safe member data and explicit lifetime bounds.
It added regressions for concurrent unsubscribe/disconnect cleanup and publication
codecs that ignore cancellation; both passed under the race detector. It also
corrected obsolete acceptance wording for earlier milestones in the READMEs.
No runtime fixes were needed after initial compilation. The full first gate took
787.4 seconds; final-source `make verify` passed in 12.1 seconds using accepted
unchanged caches. All 2,673 recorded source/build/fixture hashes matched. Private
evidence is in `.cache/milestone14-websocket/`.

The independent consumer owns typed declarations and an injected order service.
It runs through the real foundation WebSocket kernel and native HTTP middleware,
stays usable after the HTTP request deadline, and drains on application shutdown.
Milestone 15 and the remaining blueprint/final-audit work remain required.
