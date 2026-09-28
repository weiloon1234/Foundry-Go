# C04 — Supporting services through the same startup contract

Prerequisite: C03. Integrate the already delivered framework breadth into the
same settings, typed declarations, default selection and lifecycle.

| Family | Required integration |
| --- | --- |
| Mail | Named/default mailers, typed provider settings, shared cloud credentials, configured sender/limits |
| Jobs | Named/default backend connections and default queues; typed job registration and routing |
| Scheduler | Typed schedules with shared configured coordination and explicit jobs/connection targets |
| Outbound HTTP | Named/default clients, origins, headers, timeouts, bounds and trace policy |
| Logging | Named/default channels and composed stacks; owned file sinks and structured redaction |
| Pub/sub/realtime | Configured broker connections distinct from typed topics/rooms/channels |
| Image | Default engine configuration and typed reusable plans; existing codec ownership |
| Auth/browser sessions | Typed guard defaults and configured persistence/policies without actor erasure |
| Events/outbox/audit/notifications | Contribute existing registries, stores, jobs and migrations via configured dependencies |
| Other delivered features | Localization, attachments, settings, metadata, reports, health/maintenance, observations and plugins use the same assembly |

## Kernel and delivery semantics

The existing jobs module registers one Worker kernel. Refactor composition so
multiple connection definitions coexist and one selected worker kernel owns the
configured work. Apply the same principle to realtime kernel construction. Do
not register one competing kernel per connection.

Queues remain distinct from connections. Preserve namespace isolation, existing
atomic backend workflows and outbox commit semantics. Do not silently fail over
an ambiguously delivered job/email or promise cross-backend atomicity. In-memory
adapters remain explicitly non-durable.

## Lifecycle and testing

Dependency references reuse owned instances and enforce borrower-before-owner
shutdown. Only enabled features acquire resources. Migrations and maintenance
remain explicit commands/tasks, with default or named target selection.

Prove at least two instances for each named family, default alias identity,
isolated credentials/config, independent applications and no duplicate kernels.
Test dispatch routing, typed actor defaults, wrong-name/driver failure, custom
provider extension and shutdown failures. Extend the independent bootstrap
fixture and real gopls/compiler gates; finish the native full gate.


## Implementation layout and concrete contracts

C04 is one implementation batch. The following describes the assembly being
implemented; the master remains the authority for acceptance status.

### Named resources

Feature packages own distinct selectors and immutable default registries:
`email.MailerName`, `jobs.ConnectionName`, `httpclient.Name`,
`logging.ChannelName`, `pubsub.ConnectionName` and `websocket.ConnectionName`.
They reuse `internal/namedservice`; a default aliases a registered instance.
Infrastructure settings use generated element schemas and the same bounded
`config.DecodeTable` path already used by databases, disks and caches.

`infrastructure.Plan` owns validation, sorted provider construction and references
to configured Redis/storage/cloud credentials. Mail limits/default sender and
transport settings are distinct from message declarations. HTTP clients retain
existing origin/header/retry/trace rules. Pub/sub connections remain distinct
from typed topics. Realtime connections select the existing local hub or Redis
cluster authority, whose presence and fan-out contract is richer than ordinary
pub/sub. There is no conversion of a topic name into a connection selector.

Logging uses one `application.LogSettings` source: a default name and named
`logging.ChannelSettings`. Each leaf reuses `logging.Sink`; stacks form a bounded,
cycle-checked graph and deduplicate repeated leaves in declaration order. A
borrowed default logger never transfers ownership; other configured channels
remain application owned. Startup does not truncate files, and normal shutdown
closes leaves only after borrowers drain.

### Jobs, scheduling and realtime

`jobs.ConnectionModule` constructs dispatchers and freezes contributions without
registering a kernel. `jobs.WorkerModule` selects one dispatcher for one Worker
kernel. The existing `jobs.Module` composes both paths for compatibility.

`jobs.Connection` pairs a dispatcher with its configured default queue.
`definition.On(connection)` returns a payload-typed binding: empty per-call queue
selects that default, while an explicit queue wins. Existing advanced definition
APIs retain their declared queue. Capture, dispatch, inspection, cancellation and
transactional enqueue preserve IDs and payload ownership. An outbox cannot be
used through a binding belonging to another connection. Memory remains explicitly
non-durable and cannot satisfy the existing durable publication requirement.

Typed application job declarations bind handlers from constructor services using
the existing contribution registry. The scheduler reuses one configured lease
manager and the existing occurrence-to-job identity logic. Its selected connection
and queue are explicit typed values. Realtime declarations construct exactly one
selected Hub/server kernel; adding named connection definitions adds no competing
kernel. Only the selected kernels run their loops.

### Remaining feature assembly

Deployment settings select persistent session/token stores and browser policies;
Go providers, guards and policies retain their model type. Route-group guard
bindings supply concrete defaults. Browser sessions reuse the existing CSRF,
cookie, rotation and credential-lifetime implementation. Bearer policy remains
separate. Runtime model discovery and erased actor access are excluded.

Application feature assembly reuses events, outbox publishers, audit recorders,
notification managers, model-extension stores, settings/metadata/translations,
attachment managers, catalogs, datatables/reports and readiness modules. Domain
inputs are typed declarations or constructor callbacks over `application.Services`.
The framework owns adapter/manager factories and dependency keys. Existing plugin
contribution APIs remain available as the explicit advanced extension boundary.

Migrations are collected by configured database/schema and returned for an
explicit command. Neither construction nor normal boot applies them. Maintenance,
observability and readiness remain per application; this work adds no automatic
public administration endpoint. Disabled features acquire no resources.

### Acceptance batch

Add package and independent-consumer proof for each registry's default identity,
two differently configured instances, missing names, unsupported drivers, isolation
and reverse ownership. Exercise actual routed jobs, one Worker/realtime kernel,
shared coordination, native Redis/PostgreSQL, persistent typed actors, mail/HTTP
protocol behavior and owned logging. Extend generated-configuration/compiler/gopls
fixtures and user guides. Only after the whole source/test/documentation batch is
complete run generation and compilation, collect failures, fix them together and
complete the final native gate. C05 still owns final packaging, performance and
the full-change re-audit.
