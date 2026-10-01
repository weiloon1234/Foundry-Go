# Compatibility and rollout policy

This policy accompanies the first complete Foundry-Go implementation and its
[local candidate acceptance](production-acceptance.md). Rust supplies the feature
reference; Go source names and diagnostics schemas are native contracts, not
byte-for-byte Rust API compatibility. No public version has been published by
this acceptance work.

## Client presentation metadata — manifest 6

Manifest version 6 adds bounded presentation metadata to public properties and
parameters. Versions 1–5 must be regenerated; readers continue rejecting unknown
versions and fields. Use the same framework revision for runtime, Go generation,
manifest/OpenAPI export and TypeScript. Existing direct SDK calls retain their
signatures. New reserved descriptor names can qualify a colliding schema type
name; review explicit type imports after regeneration. New descriptors and public
presentation declarations are documented
in [client descriptors](guides/client-descriptors.md).

Presentation is optional and never changes request codecs or validation rules.
Explicit password hints reject output graphs and credential body examples/default
parameters; applications should keep dedicated input and response views. No
headless form controller or frontend adapter is introduced by this change.

The 2026-09-30 re-audit tightened declarations that exporting a client would
already have refused. Route registration now rejects JSON and event-stream
responses reaching a password hint, even in applications that never export
clients. Path parameters, endpoint query parameters and manifest URL/room
parameters reject password hints; move credentials into a request body.
Contradictory hints on enum fields and plain scalar transport fields now fail
generation instead of registration. Declarations that passed before and export
cleanly are unaffected.

The follow-ups of 2026-09-30 add generated TypeScript names `Locale`,
`LocaleMap` and `OperationJSONBodies`; a schema with one of those names is
qualified after regeneration, so review explicit type imports. Request maps keyed
by `i18n.LocaleID` change from `Record<string, V>` to `LocaleMap<V>`: code that
wrote an unsupported locale now fails type checking, and descriptors address
their entries with `.at(locale)` instead of `.field(...)`. Received values keep
string keys. `FormTaskOptions` gained an operation type parameter with a default,
and `failed` form submissions gained `outcome`; exhaustive matches on
`ContractError` continue to match `ResponseContractError`. Registration now
rejects password-hinted datatable rows, notification inbox and realtime
payloads, presence members and server-to-client events that client export already
refused.

## Modules and public Go APIs

Root `go.mod` owns the supported Go requirement. All fixtures and development
modules use that requirement; release tools read it rather than pinning a second
SDK. Public Go packages, generated consumer APIs and observable behavior form the
release surface. `internal` packages are implementation details. Exported structs
should be initialized by named fields or provided constructors so additive fields
do not force positional-literal changes.

Before 1.0, a minor release may contain documented breaking changes; a patch
release preserves supported public signatures and wire contracts except an
explicitly documented security correction. Once a stable major is declared,
breaking source changes require the corresponding Go major module path and a
migration guide. This document does not choose a tag or publish any version.

Pin the framework version in each application and plugin. Run generation with
that same version and keep generated outputs together with their ownership
manifests. Changes to generated binary ownership, field capabilities, query
scopes or method signatures require regeneration and consumer compilation. A
successful runtime codec test alone does not establish generated-model support.
Do not combine a newer generator with an older runtime by accident. Enum cases
are the exported typed constants declared as values; aliases of another case are
not cases. An unexported constant with its own value was a case in earlier
releases, so regeneration now fails on it instead of dropping that wire value:
export it to keep it, or mark it `//foundry:ignore` to remove it deliberately.

Plugin release versions and framework compatibility requirements use the existing
typed [plugin manifest](../plugin/manifest/manifest.go). Its `FrameworkVersion`
identifies the plugin API contract independently of Go's SDK version or an
application payload version. Change it deliberately when that compatibility
surface changes and verify base/dependent/consumer resolution together. It is
not inferred from a synthetic packaging candidate version.

## Persisted and transport formats

| Contract | Rollout rule |
| --- | --- |
| Generator ownership/journal format | Use its existing versioned decoder and recovery rules. Keep manifests with owned output; never overwrite unknown ownership. |
| Public contract manifest/OpenAPI/TypeScript | Export from the registered descriptors, regenerate adapters together, and validate old/new clients against the server. Strict decoders can reject additive fields. Generated TypeScript clients decode responses and events tolerantly by default (`strictResponses`/`strictEvents` restore strict decoding); schema names no longer carry hash suffixes, so regenerate clients and update imports of renamed types together. |
| WebSocket protocol | Retain the declared protocol version and frozen transport fixtures. A version change requires explicit negotiation/migration; do not silently reinterpret messages. Handshake tickets are an additive opt-in: `foundry.v1` frames are unchanged, the exported protocol description gains `ticket_subprotocol_prefix`, and hubs without `WithTickets` reject a ticket with 401. Ticket issuance needs the token migration `000004_create_tickets`. |
| Job payload `Version` | Keep handlers for retained producer versions until queued, delayed, retried and workflow work has drained or been deliberately migrated. |
| Job envelope transport version | Legacy shape remains the default. Enable optional trace format 2 only after every worker/outbox/workflow reader can decode it. Format 3 (`MaxExceptions`, `RetryUntil`, until-processing uniqueness, encrypted payloads, workflow callbacks) is written only when a job uses those fields; upgrade every reader first. |
| WebSocket cluster policy | The namespace policy fingerprint includes cluster limits such as `MaxConnections` (default now 65,536). During a rolling upgrade, pin the previous value on new processes or deploy the new release under a new realtime namespace; mixed fingerprints are rejected as `PolicyConflict`. Fan-out envelopes stay readable by earlier releases: excluding the sender (`RelayToOthers`, `ExceptConnection` of a local connection) adds no envelope field. Excluding a connection on another instance does, and earlier releases stop their hub on such an envelope, so `ClusterConfig.ExcludeRemoteConnections` stays off (such publications fail with `fault.Invalid`) until every instance runs this release; alternatively deploy under a new namespace. This release drops a single envelope it cannot read instead of stopping. |
| Redis cache entries and rate-limit windows | Tagged cache payloads store their snapshot fingerprint and fixed rate-limit windows start at a per-key phase. Entries written by an earlier release are misses that the next write replaces; for an error-free rolling upgrade shared by old and new processes, deploy the new release under a new cache/rate-limit namespace. |
| Redis job queue storage layout | Layout 2 is written only after an explicit `jobs migrate-layout` per queue; a layout-1 queue otherwise returns `jobs.ErrLegacyLayout` unchanged. Stop or drain the previous release before migrating; rollback needs a pre-migration Redis snapshot or a fully drained queue. See [the upgrade procedure](guides/jobs-operations.md#redis-queue-layout-upgrade). |
| Outbox records | Preserve captured bytes, IDs and destinations across retries. An ambiguous publish is reconciled using the same captured pending operation. |
| Application/database schema | Use reviewed forward migrations with compatible expansion/backfill before contraction. No automatic destructive synchronization or reset. Framework-owned migrations (sessions, tokens, outbox, idempotency, audit, settings/extensions, translations, countries, notifications, attachments, job archive, outbound webhooks, MFA) must be applied before the release that reads them serves traffic; application boot never applies them. Down migrations are optional and run only through `migrate rollback --step N --confirm`. `app.RunDatabaseCommand` keeps each schema target's history in `<schema>.schema_migrations`, as the PostgreSQL testkit does; it does not read history that a hand-built `DefaultPostgresConfig` runner wrote to `foundry_ops.schema_migrations`, so keep that runner for such a deployment. |
| Model extension slots | A slot's stored name is its snake_case Go field name unless `foundry:"name=..."` pins it, and a generated owner's name and storage model are its table unless `extension_owner=` pins them. Pin both before renaming a Go field or a table; otherwise existing translations, metadata and files are hidden, not deleted. `translations`, `metadata` and `attachments` `undeclared` commands report stored names no slot declares. |
| Encrypted model fields | Opt-in field types; existing fields and generated code are unchanged. Generated `query.Column` declarations gain `Encrypted` for these fields. Keep every key that encrypted stored values in `Encryption.Previous`: removing it makes those rows unreadable. Values are bound to table, column and primary key, so renaming either or rewriting a key needs re-encryption. |
| Browser refresh cookies | Opt-in: `TokenCookieResponse`, `RefreshTokenCookie`, `RefreshTokenCookieLogout` and `ClearRefreshCookie` add a cookie transport beside the unchanged JSON transport for the same guard. Endpoint metadata, the manifest and OpenAPI gain an optional `refresh_cookie` description (with `optional` for a cookie logout); existing operations export as before. `Tokens.LogoutRefresh` requires the backend to implement `token.RefreshRevocationBackend`, which the PostgreSQL backend does; a custom backend without it fails with `fault.Invalid`. |
| Per-guard token lifetimes | Additive: `token.New` and `application.NewTokenGuard` gain a variadic `Option` parameter, so existing calls compile and issue with the store's lifetimes as before. Only a function value of their exact former type needs updating. Families already issued keep their stored lifetimes. |
| Token refresh history | Refresh keeps the current and previous generation rows and moves older consumed refresh digests to `foundry_token_consumed_refreshes`. A rollback to an earlier release keeps valid tokens working but loses reuse detection for digests already moved. |

See [job trace rollout](guides/job-trace-rollout.md),
[client contracts](guides/client-contracts.md), [realtime](guides/websocket.md),
[plugins](guides/plugins.md) and [outbox](guides/outbox.md) for the actual typed
contracts and limits. Existing application-specific payload migrations remain the
application's responsibility; a framework upgrade cannot infer their meaning.

Adding a field, changing optional/null behavior, changing numeric representation,
renaming an operation or tightening validation can break a strict peer. Test both
directions with retained old fixtures before calling a change compatible. Keep
all readers compatible with retained data before enabling new writers. Rollback
must preserve a reader capable of consuming formats already written.

## Operational behavior

A SPA more specific than a matching asset mount no longer runs inside that
mount's route middleware or budget; move middleware that must cover SPA
responses to the kernel or router. Model extension cleanup observers are
registered on every configured database connection: a model deleted through a
connection other than the extension store's is cleaned after commit instead of
failing or leaving orphans, and that connection must reach the model table in the
same database. Direct assembly sets `slots.Runtime.Store` for this. With
`features.extension_cleanup.jobs`, that cleanup is an outbox-enqueued job instead:
each configured connection must reach the outbox's database (startup refuses one
that does not), and a worker must consume the job connection's default queue.
Every connection now carries a deletion observer for slot-owning models: their
set-based deletions (`DeleteAll`, `ForceDeleteAll`) need `WithoutModelHooks()` on
every connection, and per-model deletions take the observed path there, while
creates, updates, `UpdateAll` and `Increment` are unaffected. In-transaction
attachment publication is two-phase: `PrepareFile` stores the file before the
transaction, and `ReplaceIn`/`AddIn` use only the caller's transaction.

The [stabilization handoff](guides/stabilization-20260929.md) records the boilerplate
upgrade order and additional authentication/retry corrections. Custom session
adapters require the optional `session.ResumptionBackend` capability for atomic
impersonation resume; the built-in PostgreSQL adapter already implements it.
The second review replaced `ConsumeActorIn` with `ResumeActor`: custom adapters
must rotate the exact live actor session in place without extending its absolute
lifetime, following the [session contract](guides/sessions.md).

Password logins using `WithLockout` must bind a declaration made with
`lockout.DefineLogin(name, codec, lockout.DefaultLimits())` (or explicit validated
limits). The older single-key `lockout.Define` remains useful for authenticated
MFA/challenge attempts, but password-login construction rejects it. Regeneration
does not replace handwritten declarations. Request attribution and configured
trusted proxies supply the client address; see [login lockout](guides/login-lockout.md).

Scheduler capacity now waits according to `CapacityWait` before skipping work.
Tests with manual clocks must advance that clock through the wait, or explicitly
select zero when testing immediate rejection. HTTP numbered/simple pagination
also enforces `MaximumPage`; exceeding it reports the page field's validation
path before offset arithmetic. Preserve those bounds when updating consumer
tests. Migration tests should compare the configured declarations and their
states rather than retaining a fixed framework migration count.

The [second review](guides/second-review-20260929.md) also requires these consumer
adjustments when the corresponding features are enabled:

| Feature | Upgrade requirement |
| --- | --- |
| Password confirmation | Configure `PasswordLogin.WithConfirmationLockout` and the confirmation route's HTTP rate limit; see [password confirmation](guides/browser-sessions.md). |
| Datatable downloads | Declare the download route with `.WithTimeout(manager.DownloadTimeout())`; see [datatable exports](guides/datatable.md). |
| Failed-job archive | Apply its new migration and use cursor paging with `ListOptions.After`; see [archive operations](guides/jobs-operations.md). |
| Outbound webhook workers | Register `job.FailureSink(service)` with every worker running the delivery job so terminal failures update delivery state; see [outbound delivery](guides/webhooks.md). |
| Login limits | The per-address ceiling is now opt-in. Review [login lockout](guides/login-lockout.md) before enabling it for shared client addresses. |
| Public readiness | Dependency results are cached for one second by default; lifecycle and maintenance state remain live. See [public probes](guides/production-diagnostics.md) when setting load-balancer probe expectations. |
| SPA fallbacks and asset mounts | A SPA more specific than a matching asset mount now owns its subtree, and a SPA with the same prefix answers the mount's misses; before, a root mount at `/` answered those paths itself, usually with 404. Check paths under each SPA prefix that a root mount used to serve from its own directory; they are now 404 unless the SPA's build contains them. See [SPA composition](guides/http-assets.md). |
| Maintenance bypass | Share application encryption keys across instances. Without keys, bypass cookies work only in the issuing process; configure trusted proxies before using client allowlists behind a load balancer. See [maintenance](guides/readiness-and-maintenance.md). |

Apply the new session/token constraint-validation and idempotency-index migrations
before serving the revised runtime, as well as migrations for any other enabled
framework store. Migration commands must target each store's configured database
and schema; enabling a feature does not apply its schema automatically.

Default-safe opt-ins remain explicit: incoming trusted trace context, outgoing
HTTP propagation, queued trace transport and read-pool routing. Replica reads do
not promise immediate visibility after primary commits. Use the primary executor
or actual transaction when the operation requires that visibility.

Maintenance stops admission while existing owners finish. A deadline bounds the
caller's wait; it does not prove a callback stopped or release its resources.
Do not turn a timeout into silent data loss or close a dependency under admitted
work. Use lifecycle state, owner `Done` signals and bounded diagnostics during
rolling termination. Operational additions preserve ordinary authentication,
permissions, redaction and typed identifiers.

## Optional generated form adapters

The [form controller](guides/client-forms.md) is additive to the existing SDK and
manifest v6. Regenerate with the matching tool/runtime and review type imports: new
SDK declarations can qualify colliding DTO names. React/Vue output is opt-in and
uses separate generated modules/subpath exports; the core stays dependency-free.
The generator owns adapter imports, freshness and removal along with all other
artifacts. Never hand-edit generated adapters or re-export optional peers from a
core entry point. Database startup logs add safe diagnostics without changing
connection budgets or the B08 release disposition.

The 2026-09-30 re-audit changed `FormSubmission`, which no published starter uses
yet. Edits no longer abort a sent request or produce `stale`: completed results
carry `changed`, and `canceled` carries `outcome` (`not_sent` or `unknown`).
`FormTaskResult` no longer includes `stale`; an ended run is `canceled`. The
descriptor `variant()` type now requires a key that tags every union member, so
calls passing an enum-typed property of a plain object stop compiling (they
already failed at runtime). Update any code that matched `stale` and regenerate
the SDK with the matching tool.
