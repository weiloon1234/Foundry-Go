# Notifications

Milestone 17 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-17-verification-and-consumer-review)
records the full gate, focused regressions and operational limits.

`notifications` binds a versioned input DTO to a typed recipient and channel
renderers. It owns PostgreSQL notification/inbox rows and per-channel progress.
It borrows the existing authentication provider, email mailer, WebSocket publisher
and jobs/outbox infrastructure. No separate queue or transport runtime is created.
The independent [consumer fixture](../../tests/fixtures/consumer/notifying/orders.go)
shows generated DTO contracts, model-owned keys and thin domain services.

## Declarations and sending

Declare an input DTO with `//foundry:dto`, generate its JSON descriptor, then use
`notifications.Define(name, version, InputJSON())`. Reuse that same declaration for
every recipient binding. `DefineRecipient` takes an existing `auth.Provider[M,K]`,
its exact `auth.Guard[M]` declaration and a preference callback. The framework
reuses the generated key codec and current eligibility policy. Matching provider
names do not permit substituting a different lookup authority.

`notifications.Bind(definition, recipient, channels...)` retains both model and
payload types. Freeze its `Registration()` with `NewRegistry`, then construct
`New(existingDB, registry, config)`. Duplicate recipient, notification/version or
channel declarations fail explicitly. A binding supports at most 16 channels and
one database channel. Construction performs no I/O or migrations.

Run `notifications.Migrations()` explicitly using the ordinary migration runner
in `Config.Schema`. Keep notifications and the jobs outbox in the same schema.
The tables contain private payloads, email routes and rendered content; access to
the database requires the same protection as application messages.

Capture a typed reference and input once when caller retries must preserve an ID:

```go
pending, err := orders.Capture(ctx, user.FoundryReference(), input,
    notifications.ID[models.User]{})
if err != nil { return err }
report, err := pending.Send(ctx, manager)
```

The zero ID generates a UUIDv7. An explicit ID is a transport/retry boundary, not
authorization. Reusing an ID with different input, recipient or channel selection
returns a conflict. The first stored capture also freezes provenance; later
enqueue attempts reuse it even when called from a different request context.
Provenance is metadata and never authentication evidence. Capture validates and snapshots data without loading a model
or contacting any service. It rejects live model payloads through the shared DTO
contract. Runtime pending handles are deliberately not JSON serializable.

`binding.Send` is a convenience for a new notification; repeat a captured pending
handle to retry the same notification. Inspect its report even when an error is
returned. A delivered channel is never run again by notification retries.

## Channel outputs and timing

`Database(id, OutputJSON(), renderer)` returns a typed database channel. Bind its
`Channel()`, and use `Decode(ctx, definition, record)` to obtain the concrete inbox
output. The input DTO and inbox DTO may differ. Renderers receive the fresh model,
input and `DeliveryContext`, including a stable notification and delivery ID.

`Email(id, mailer, renderer)` returns an email channel. Its renderer returns the
ordinary `email.Message`. Foundry captures a bounded `email.Snapshot`, including
addresses, headers, bodies and pinned attachment references. Retrying a definite
temporary rejection reuses that exact content and provider idempotency key.
Attachments must specify storage `Version` or `IfMatch`. Files, readers and paths
never enter the durable snapshot. Snapshot size is bounded by the shared JSON
limit; notification input and prepared output each have a 768 KiB limit.

`DefineRealtime[C]` creates an owned private room and server-only event using the
recipient's exact guard and generated model key. Register its `Registration()`
with the existing WebSocket registry before constructing a hub or distributed
publisher. `RealtimeChannel` binds that declaration to a typed renderer and
publisher; the renderer cannot choose another recipient room. `RealtimeMessage[D]`
adds the stable notification ID, name and version to the output schema without
repeating the DTO's fields. Whole-channel and foreign-room subscriptions fail.
Publication success means local admission/distributed publication attempt, not
receipt by every user device. Reuse the existing replay/TTL policies as needed.

`Custom(id, OutputJSON(), renderer, transport)` accepts a focused
`Transport[D].Deliver(ctx, DeliveryID, D)` implementation. Put custom routing data
inside that declared output so it is frozen too. Return `Accepted`, `Retry`,
`Reject` or `Unknown`. **Retry promises definite nonacceptance**; transport errors,
panic, Goexit or invalid outcomes are uncertain and never silently retried.

Recipient lookup, eligibility and preferences run immediately before **each
channel attempt**, including retries of prepared outputs. Missing/disabled models
become terminal `Ineligible`; a false preference becomes terminal `Skipped` for
this notification. Lookup/preference failures leave the channel retryable.
Recipient error inspection is bounded to 256 nodes and 64 unwrap levels. An
incomplete lookup error graph leaves the channel pending without rendering or
sending. Custom error methods must return; graph bounds cannot interrupt a method.
Eligibility/preferences are checked again after rendering before admission.
Renderers run only while pending and must be pure, deterministic and free of
external side effects. Concurrent callers may render more than once before one
snapshot wins. Once prepared, routes/content remain frozen even if the profile
changes; current eligibility/preferences still apply. A change cannot retract a
delivery already admitted or accepted.

## Inbox ownership

`recipient.Inbox(manager)` creates an inbox accessor. `List`, `UnreadCount`,
`MarkRead`, `MarkUnread` and `Status` resolve the verified guard from the current
context and recheck current eligibility. They take no client-supplied recipient
identity. Stored predicates include recipient/guard/provider/model scope and the
canonical stored key. Equal keys belonging to other models or guards do not
collide. Parsing or retyping an ID cannot grant access.

`List(ctx, query.PageRequest, unreadOnly)` returns bounded pages of `Record[M]`.
Decode a record through its database channel and build the application's declared
HTTP response DTO; records do not implicitly serialize their private payload.
`MarkRead`/`MarkUnread` return false for both absent and foreign IDs. Repeating
`MarkRead` preserves the original read instant. Pagination count and rows use
separate read-committed queries, so concurrent changes may affect their snapshots.

## Queued and transactional delivery

`DefineDeliveryJob(name, jobs.Policy)` declares the existing worker handler.
Register `job.Declare(manager)` in the normal jobs registry and prepare an ordinary
`jobs.Outbox`. Capture the notification outside retrying business transactions,
then enqueue it inside the business transaction:

```go
_, err := pending.Enqueue(ctx, tx, manager, deliveryJob, jobOutbox)
```

Notification rows and the ordinary outbox message share a savepoint in that exact
borrowed database pool. Any enqueue error rolls back both, even if application
code ignores the error and commits its outer transaction. Schema changes are
restored before control returns. The outer commit decides durability; rollback
cannot leak a queue message. Publication and queue deduplication retain the
ordinary jobs/outbox guarantees. The job payload contains only a stable
notification ID; fresh workers restore input and per-channel state from storage.

One job attempts all available channels. Only retryable channels cause another
attempt; completed/terminal channels are skipped. A terminal failure alongside a
retryable channel does not prevent that remaining channel from being attempted.
When every remaining outcome is terminal, the job stops retrying. Notification
state records deletion/preference decisions even when no inbox row was created.

## Persistent outcomes and ownership

| State | Meaning and retry behavior |
| --- | --- |
| `Pending` | Input captured; current recipient checks/rendering can run. |
| `Prepared` | Output frozen; a definite nonacceptance may retry it. |
| `Running` | An external call was durably claimed. It may still be active or its process may have been lost. No automatic reclaim/resend. |
| `Delivered` | Database transaction committed or transport reported acceptance. |
| `Skipped` | Current preferences disabled this channel. Terminal. |
| `Ineligible` | Recipient was missing or ineligible at delivery. Terminal. |
| `Rejected` | Invalid rendered output or definite permanent transport rejection. Terminal. |
| `Uncertain` | External acceptance cannot be established. Terminal. |

Database delivery creates one inbox row and records delivery in the same locked
transaction. External channels claim before I/O and persist outcomes afterward.
No database transaction holds locks while user renderers/transports run. A crash
after claiming or a failed completion write leaves `Running`, preserving evidence
instead of risking an automatic duplicate. Operators must investigate these rows
and uncertain outcomes against the provider before deciding on a new send; this
API does not claim exactly-once external delivery or automatically repair unknown
results. Finite provider idempotency retention does not change that policy.

Manager admission and contexts are bounded. Callbacks own their actual lifetime;
panic/Goexit are contained, but an uncooperative callback is not abandoned.
Completion/status persistence has a separate five-second cleanup bound after an
operation deadline. `Close` cancels admission/work; its context bounds the caller's
wait while `Done` remains open until actual completion. A callback cannot wait for
its own manager to close. `Module` integrates this ownership with foundation;
declare every borrowed dependency in `Requires` so shutdown drains in order.
Default formatting omits captured payloads, routes and rendered content.

No new dependency or external email provider send is required by notifications.
Email transport support and the remaining real-provider smoke gap are documented
in [email](email.md). Verification evidence is recorded in the
[master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-17-verification-and-consumer-review).
