# Configured supporting services

C04 passed its [native acceptance gate](../evidence/consumer-startup-c04.json);
acceptance status belongs to the
[master blueprint](../../blueprint/00-master-architecture-and-parity.md). The
examples below describe the implemented API; [C05 final acceptance](consumer-startup-acceptance.md) also passed.

Use `application.Settings` for deployment values and Go declarations for domain
behavior. `application.New(settings).HTTP(routes).Jobs(jobs...).Events(listeners...)
.Schedules(schedules).Realtime(channels).Features(features).Build(ctx)` constructs
one application. Call `app.Run(ctx, foundation.HTTP)` (or Worker, Scheduler,
WebSocket) to select a runtime. Registering multiple named connections creates no
additional worker or realtime kernels.

## Names and defaults

| Family | Typed name | Constructor selection |
| --- | --- | --- |
| Mail | `email.MailerName` | `s.Mailer()` / `s.Mailers.Mailer(name)` |
| Jobs | `jobs.ConnectionName` | `s.JobConnection()` / `s.Jobs.Connection(name)` |
| Outbound HTTP | `httpclient.Name` | `s.HTTPClient()` / `s.HTTPClients.Client(name)` |
| Logging | `logging.ChannelName` | `s.Logger` / `s.Logs.Channel(name)` |
| Pub/sub | `pubsub.ConnectionName` | `s.Broker()` / `s.Brokers.Broker(name)` |
| Realtime | `websocket.ConnectionName` | `s.RealtimeConnection()` / `s.Realtime.Connection(name)` |

Each default aliases an existing configured instance. Defaults initially name
`default`; a populated collection must contain the selected name. Empty service
collections disable the family. Missing names and unsupported drivers fail rather
than selecting an unrelated instance. Database, Redis, cache and storage follow
[the same contract](named-services.md).

Use `infrastructure.DefaultMailerSettings`, `DefaultJobConnectionSettings`,
`DefaultHTTPClientSettings`, `DefaultBrokerSettings` and
`DefaultRealtimeConnectionSettings` for defaults. Mail requires an explicit sender;
`mailer.Message(subject, recipients...).Text(body)` uses it. Mail driver choices
are log, memory, SMTP, SES, Resend, Postmark and Mailgun. Log/memory simulate
acceptance. SES borrows the selected shared credential source. API tokens,
passwords and configured HTTP header collections have secret configuration keys.
`application.WithMailDriver(name, driver)` borrows a custom driver; its owner keeps
it alive until shutdown finishes.

Named objects load through generated element schemas. For example, with settings
rooted directly at `application.Settings`:

```toml
[services.jobs]
default = 'background'
[services.jobs.connections.background]
driver = 'redis'
redis = 'default'
default_queue = 'reports'
[worker]
enabled = true
```

The referenced Redis connection must also be configured. Names remain distinct
from queues, channels, topics and model types. JSON is used for explicitly tagged
collection leaves inside the typed schema; ordinary deployment files remain TOML.
Go settings/defaults and generated override keys remain the source of truth.

## Typed jobs and domain dependencies

```go
connection, err := services.JobConnection()
if err != nil { return err }
work, err := ReportJob.On(connection)
if err != nil { return err }
_, err = work.Dispatch(ctx, ReportPayload{ReportID: id}, jobs.Options[ReportPayload]{})
```

An empty per-call queue uses the connection default. An explicit queue wins. The
advanced `ReportJob.Dispatch(ctx, dispatcher, ...)` retains the definition's queue.
Capture retains a stable job ID for intentional retry. `application.Job` accepts a
constructor returning `jobs.Handler[ReportPayload]`; `.On(name)` chooses which
connection receives that declaration. `Worker.Connection` selects the one worker;
omitted queues select that connection's default queue.

`application.Listen` retains the event DTO. `Schedules` returns existing typed
schedule declarations; `schedule.ConnectionJobTarget` preserves the occurrence
identity and selected queue. Scheduling requires enabled shared Coordination.
Redis coordination reuses a configured Redis client. Process-local coordination
is appropriate only where process-local exclusion is intended.

Custom provider factories obtain this facade with `application.FromResolver(r)`.
Its resolver is valid only during that factory; `app.Resources()` uses the frozen
runtime registry. Constructor services are for wiring. Store concrete handles in domain structs;
pass `context.Context` to operations for cancellation, deadlines and attribution.
Do not place config, managers or an erased actor into request context. The
[independent delivery constructor](../../tests/fixtures/consumer/bootstrap/supporting.go)
keeps its job payload and mailer concrete.

## Persistent typed actors

Enable `Features.Auth.Sessions` and/or `Tokens`, selecting their database and
schema. Browser policies have named guards and a default. Each browser policy owns
its cookie and CSRF policy. `application.NewBrowserGuard(services, name, provider,
source)` returns typed `Sessions`, `Browser` and `Binding`; empty name selects the
browser default. `NewTokenGuard` similarly returns typed `Tokens`, transport and
binding with an explicit model-owned access-scope ceiling.

Apply `browser.Browser.Middleware()` to the browser endpoint/group and pass
`browser.Binding` to `http.Authenticated`. Separate actor models receive separate
bindings. The bearer adapter does not require cookie state or Origin. A provider
reloads the concrete model; trusted credential verification constructs the proof
used for issuance. Framework setup does not choose login credentials or domain
permissions. See the [persistent consumer proof](../../tests/fixtures/consumer/bootstrap/persistent_test.go).

## Feature declarations and migrations

`Features` returns typed declarations for model-extension owners, settings keys,
metadata, translations, attachments, notification bindings, reports, localization
messages, custom outbox routes and readiness probes. Enable the corresponding
feature and select its database/schema in settings. The framework reuses existing
registries, managers and lifecycle modules. Domain callbacks perform pure
construction; do not call operations or resolve a manager being constructed from
that callback's own declarations.

`app.Migrations()` returns independent definition snapshots grouped by configured
database/schema. Merge them with domain definitions in an explicit migration task.
Normal Build/Start never applies migrations. An enabled outbox publisher expects
its migrations to exist before startup. Job outboxes select only durable queue
backends and reuse the connection's dispatcher. `services.JobOutbox(name)` returns
the configured producer; empty name uses the job default. Enqueue into a business
transaction from the exact configured pool. Writes use the outbox schema and
restore the caller's search path; rollback prevents publication. Cross-backend
atomic delivery and silent failover are not promised.

Resolve `scope, err := services.AuditScope()` inside the domain constructor and
retain that concrete handle. Use `scope.Within(ctx, tx, callback)` for the configured audit schema and
pool. Its child transaction shares the business commit. `services.Audit()` exposes
the advanced recorder for callers already managing transaction/executor scope.
Retention and maintenance operations remain explicit.

Health can include configured database/Redis probes plus domain probes.
Observability and maintenance state belong to each application. No administration
endpoint is exposed automatically. Existing `Register`/`RegisterPlugin` remain
available for custom modules and plugin contributions.

## Logging and ownership

`Log.Default` selects `Log.Channels`. A leaf wraps the existing stderr/stdout/file
JSON sink; a `logging.Stack` channel references other named channels. Cycles,
missing children and duplicate direct children fail Build. Overlapping branches
write each leaf once, preserving leaf levels, groups and redaction. Build performs
no file I/O; startup appends and shutdown closes owned leaves after borrowers drain.
`WithLogger` replaces the default with a borrowed logger; other explicitly
configured channels retain ownership. File sinks automatically rotate daily (UTC)
or at 20 MiB and retain at most 14 archives for 14 days. See
[logging](logging.md) for typed overrides, cleanup timing and ownership.

Only configured services acquire resources. HTTP transports, mailers, brokers,
managers and selected kernels drain before their borrowed pools/backends close.
Memory drivers are process-local. A custom borrowed driver/logger/credential
provider is never closed by assembly. Independent applications own independent
configured instances.
