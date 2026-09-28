# 15 — WebSocket distributed behavior

## Purpose and prerequisites

Prerequisites: [09](09-redis-cache-and-coordination.md), [12](12-jobs-and-worker-kernel.md), [14](14-websocket-channels-and-protocol.md). Reach current Rust realtime feature parity with clear limits on delivery and replay.

Rust references: `src/websocket/mod.rs`, `src/kernel/websocket.rs`, `src/redis`, `src/logging/diagnostics.rs`; `tests/distributed_runtime_acceptance.rs`, `websocket_observability_acceptance.rs`; `blueprints/14-websocket-system.md`.

## Public capability additions

Add validated heartbeat/rate/queue/connection limits, distributed publication, typed client-event relay, acknowledgements, bounded replay options, connection/subject disconnect commands and diagnostics. Presence helpers return typed member records and counts.

Implemented management usage:

```go
err := websocket.DisconnectSubject(ctx, publisher, guard, user.FoundryReference())
```

Subject identity includes the guard/provider namespace and model-specific key. A raw ID cannot accidentally disconnect a different class of authenticated user.

## Implementation slices

1. Heartbeat/pong deadlines, per-connection rate limits, maximum subscriptions, bounded outbound queues and slow-consumer disconnect.
2. Redis fan-out, instance identity and dispatch from HTTP/jobs without direct socket ownership.
3. Presence membership per connection with TTL/heartbeat, safe member metadata and last-connection leave semantics.
4. Distributed subject connection limits and forced disconnect/revocation commands.
5. Client acknowledgements and bounded channel/room replay, with documented ordering and retention.
6. Per-channel/runtime metrics, protected diagnostics and operational failure tests.

## Delivery and recovery semantics

Redis pub/sub is live fan-out and can lose messages during interruption. Bounded replay supplies recent channel/room history; it is not a durable session cursor or guaranteed recovery after arbitrary downtime. Durable reconnect recovery is deferred in [25](25-deferred-extensions.md).

Acknowledgements distinguish protocol acceptance from handler completion. A completed client-message acknowledgement means the handler returned successfully; it does not certify that every subscriber received a broadcast. Stable message IDs allow clients to deduplicate replay/live overlap. Do not claim global ordering across publishers or exactly-once delivery.

Presence counts authenticated subjects correctly across tabs/connections. Abrupt process loss expires stale memberships. During Redis failure, do not publish misleading authoritative cluster counts; surface degraded capability. Local and cluster-wide connection limits must be separately named/documented.

Disconnect pub/sub may also be interrupted, so authorization refresh remains a backstop for credential revocation. Shutdown stops upgrades/subscriptions, drains bounded writes and runs cleanup exactly once per connection.

## Acceptance

Run two server instances with real Redis. Test cross-instance room isolation, live fan-out, replay bounds, duplicate handling, overlapping subscriptions, acknowledgements after success/failure, stale heartbeat, slow consumers, reconnects, process loss, TTL cleanup, multi-tab presence, distributed limits and revocation. Use race tests and frame fuzzing; measure goroutine/queue/memory bounds under churn. Apply the [common gate](README.md#common-completion-gate).

## Implementation and parity review

Milestone 15 passed native verification and consumer review.
The [distributed guide](../docs/guides/websocket-distributed.md) owns concrete APIs,
defaults and failure semantics. No compilation or tests ran between implementation
slices; the complete code, test sources and docs entered the milestone verification/fix cycle.

Rust channel publication, heartbeat, presence TTL, subject limits, forced disconnect,
replay and observability were reviewed against `src/websocket/mod.rs` and
`src/kernel/websocket.rs`. Go reuses the accepted auth, typed room/DTO, Redis pub/sub,
rate limiter, native HTTP, lifecycle and callback-ownership components. Redis authority
operations live in one bounded script; no namespace reset or automatic mutation retry
is exposed. Deliberate recovery policy: a live subscription gap terminates the hub
rather than claiming continuous fan-out; restart/reconnect explicitly. Safe presence
DTOs refresh on joins, with authorization refreshed independently. Durable reconnect
cursors remain deferred to milestone 25.

The [master evidence](00-master-architecture-and-parity.md#milestone-15-verification-and-consumer-review) records the completed gate, review corrections and deliberate delivery limits.
