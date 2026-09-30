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
| WebSocket protocol | Retain the declared protocol version and frozen transport fixtures. A version change requires explicit negotiation/migration; do not silently reinterpret messages. |
| Job payload `Version` | Keep handlers for retained producer versions until queued, delayed, retried and workflow work has drained or been deliberately migrated. |
| Job envelope transport version | Legacy shape remains the default. Enable optional trace format 2 only after every worker/outbox/workflow reader can decode it. Format 3 (`MaxExceptions`, `RetryUntil`, until-processing uniqueness, encrypted payloads, workflow callbacks) is written only when a job uses those fields; upgrade every reader first. |
| WebSocket cluster policy | The namespace policy fingerprint includes cluster limits such as `MaxConnections` (default now 65,536). During a rolling upgrade, pin the previous value on new processes or deploy the new release under a new realtime namespace; mixed fingerprints are rejected as `PolicyConflict`. Fan-out envelopes stay readable by earlier releases: excluding the sender (`RelayToOthers`, `ExceptConnection` of a local connection) adds no envelope field. Excluding a connection on another instance does, and earlier releases stop their hub on such an envelope, so `ClusterConfig.ExcludeRemoteConnections` stays off (such publications fail with `fault.Invalid`) until every instance runs this release; alternatively deploy under a new namespace. This release drops a single envelope it cannot read instead of stopping. |
| Redis cache entries and rate-limit windows | Tagged cache payloads store their snapshot fingerprint and fixed rate-limit windows start at a per-key phase. Entries written by an earlier release are misses that the next write replaces; for an error-free rolling upgrade shared by old and new processes, deploy the new release under a new cache/rate-limit namespace. |
| Redis job queue storage layout | Layout 2 is written only after an explicit `jobs migrate-layout` per queue; a layout-1 queue otherwise returns `jobs.ErrLegacyLayout` unchanged. Stop or drain the previous release before migrating; rollback needs a pre-migration Redis snapshot or a fully drained queue. See [the upgrade procedure](guides/jobs-operations.md#redis-queue-layout-upgrade). |
| Outbox records | Preserve captured bytes, IDs and destinations across retries. An ambiguous publish is reconciled using the same captured pending operation. |
| Application/database schema | Use reviewed forward migrations with compatible expansion/backfill before contraction. No automatic destructive synchronization or reset. Framework-owned migrations (sessions, tokens, outbox, idempotency, audit, settings/extensions, translations, countries, notifications, attachments, job archive, outbound webhooks, MFA) must be applied before the release that reads them serves traffic; application boot never applies them. Down migrations are optional and run only through `migrate rollback --step N --confirm`. |
| Model extension slots | A slot's stored name is its snake_case Go field name unless `foundry:"name=..."` pins it, and a generated owner's name and storage model are its table unless `extension_owner=` pins them. Pin both before renaming a Go field or a table; otherwise existing translations, metadata and files are hidden, not deleted. `translations`, `metadata` and `attachments` `undeclared` commands report stored names no slot declares. |
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
