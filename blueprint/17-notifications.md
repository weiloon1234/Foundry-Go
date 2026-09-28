# 17 — Notifications

## Purpose and prerequisites

Prerequisites: [07](07-model-lifecycle-events-and-audit.md), [10](10-model-first-authentication-and-authorization.md), [12](12-jobs-and-worker-kernel.md), [15](15-websocket-distributed-behavior.md), [16](16-email.md). Compose delivery channels around typed recipients and notification payloads.

Rust references: `src/notifications`, `tests/notification_queue_acceptance.rs`, `phase2_acceptance.rs`, `docs/guides/email-and-notifications.md`, and framework notification WebSocket channel helpers.

## Contracts

A notification descriptor binds its payload type, version and available channel renderers. A recipient descriptor preserves model type and key, supports route resolution and preferences, and scopes stored notifications and realtime rooms. Consumers do not manually convert model IDs into ambiguous polymorphic strings.

The delivered implementation uses explicit typed declarations and a
borrowed manager; see [the guide](../docs/guides/notifications.md) and
[consumer fixture](../tests/fixtures/consumer/notifying/orders.go). This supersedes
the earlier implicit global-dispatch sketch. Current explicit usage:

```go
report, err := orders.Send(ctx, manager, user.FoundryReference(),
    OrderPlaced{OrderID: order.ID})
```

Database, email and realtime channels share orchestration but have typed channel-specific outputs. Adding a custom channel requires implementing its focused interface and registration, not replacing the notification manager.

## Implementation slices

1. Typed notification/recipient registries, preferences, channel selection and rendering.
2. Framework notification model/migrations, unread/read operations, recipient-scoped listing and authorization.
3. Email channel and ownership-enforcing realtime channel helpers.
4. Queued and transaction-aware delivery through jobs/outbox; per-channel delivery status and retries.

## Failure behavior

Track delivery per channel so retrying email does not recreate database notifications or blindly rebroadcast successful deliveries. Persist stable notification/delivery identities for idempotency. Preference checks and recipient lookup have explicit dispatch-versus-delivery timing; recheck recipient eligibility at delivery.

Realtime publication must target the recipient's authorized guard/model/key room. A handler authenticated as one user cannot list or acknowledge another user's notifications by guessing an ID. Deleted recipients produce an inspectable terminal result rather than an endless retry loop.

## Acceptance

Compile-test wrong recipient/payload combinations. Test cross-model key collisions, read ownership, preferences, partial channel failure, retry deduplication, after-commit rollback, revoked/deleted recipients, queue serialization, and private notification subscriptions. Apply the [common gate](README.md#common-completion-gate).

Milestone 17 passed [native verification and consumer review](00-master-architecture-and-parity.md#milestone-17-verification-and-consumer-review).
