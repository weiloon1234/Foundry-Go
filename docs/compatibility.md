# Compatibility and rollout policy

This policy accompanies the first complete Foundry-Go implementation and its
[local candidate acceptance](production-acceptance.md). Rust supplies the feature
reference; Go source names and diagnostics schemas are native contracts, not
byte-for-byte Rust API compatibility. No public version has been published by
this acceptance work.

## SPA runtime data

`SPAConfig.Runtime`, `DefineSPARuntime` and `SPARuntime.WrapProxy` are additive
opt-ins. A zero runtime descriptor preserves existing static serving. Enabled HTML
documents use `no-store`, omit validators and range support, and return the full
representation for conditional requests. HEAD emits headers without a body.
The default script ID remains `foundry-portal-runtime`, with
`type="application/json"`.

Remove application response-rewriting middleware when enabling the hook. Documents
without an explicit head element, with an existing runtime element, or beyond the
configured bounds now fail with a safe 503 instead of silently omitting runtime
data. JSON limits also apply after HTML escaping. Enable `StyleNonceCarrier` to
preserve applications that supply an empty nonce-bearing style element for client
libraries. Proxy navigation predicates must exclude APIs and assets that require
native conditional and range behavior. See [runtime data and adoption](guides/http-assets.md#inject-public-runtime-data).
No generated contract or persisted format changes are required by this hook.

## Standard Fetch transport

Regenerate TypeScript clients to expose the additive `fetchTransport: HTTPTransport`
export from the full entry and every surface entry. It ships in the existing core
runtime module; generated filenames, publication ownership format and manifest
version are unchanged. `typescript.Render` includes the same transport. Custom
transport injection remains unchanged.

Packages with an application-owned `./fetch` export can point it at the compiled
generated entry and delete their local adapter. See
[injected HTTP transport](guides/client-contracts.md#injected-http-transport).
The shipped transport forwards a declared `HTTPRequest.redirect`, with `"error"`
as the fallback when absent. Generated requests supply `"follow"` or `"manual"`;
switching from a local adapter that hard-coded `"error"` therefore changes redirect
handling: ordinary operations follow redirects, and redirect operations use
`"manual"`. In browsers a manual redirect stays opaque, so the generated client
rejects it with `opaque_redirect`; a server-side transport can return its
`Location`. Keep a custom transport if every API redirect must be rejected. Stream
bodies receive `duplex: "half"`; other bodies do not.

## Upload kind messages in the standard catalog

The standard validation catalog gains `validation.file_kinds` and
`validation.image_kinds`. Every application with locale-enabled manifests therefore
exports two more message definitions, and its generated `CatalogMessageKey` and
`CatalogMessageArguments` change, even if it never uses `FileKinds` or image kinds.
Regenerate contracts and clients with the matching framework pin, as for any
upgrade. Already-deployed clients are not wire-incompatible because of the extra
definitions. Manifest version 9 and the ownership format are unchanged.

## Typed error details: manifest 9, v0.7.0

Regenerate Go descriptors, manifest, OpenAPI and TypeScript together. Manifest 9
adds optional `details_type` to error definitions; v8 documents are rejected.
Use the application's normal generation and contract publication commands.
Declare details with a generated non-nullable object DTO and register the same
`DefineErrorDetails` declaration on the endpoint; see
[application HTTP errors](guides/http-application-errors.md).

`http.Errors` now takes the sealed `ErrorDeclarationOption` interface so one call
can mix plain and detailed declarations. Direct calls remain valid. A
`[]http.ErrorDeclaration` cannot be spread into it: change a declaration collection
to `[]http.ErrorDeclarationOption`, then call `http.Errors(declarations...)`.
A detailed declaration requires `.New(payload)` before it can be returned as an error.

Projection now keeps built-in errors plus the application errors referenced by
selected operations. This affects **detail-free applications too**. Regenerating
can remove `ErrorCode` members from a portal: for example, `ConsumerChanged` is
absent from the members, browser and live consumer surfaces. Code importing or
accessing such a member no longer compiles. Remove the import/use, or declare the
error on an operation that the portal really uses. Do not retain unrelated errors
just to recreate the former application-wide catalog.

`APIError` remains extensible: application code can attach metadata, assign a
cause or capture a stack trace. When details are present, `isOperationError` and
the code select the received payload type; the guard validates the current
response each time. Plain variants have no details. A portal with no detailed
errors generates no `isOperationError`; code shared across portals cannot assume
that export exists. For error codes they already recognize, existing strict clients
reject an unexpected `details` property as a response-contract failure, while
tolerant clients ignore it. Deploy clients that need the payload with the
regenerated contract before relying on it.

Detailed rejections are not stored by idempotency. Their transaction and claim
roll back, so retrying the same key re-executes against current state; durable
rejection replay remains deferred.

## Attachment transactions and locale configuration — v0.5.2

`Collection.ClearIn`, `DetachIn`, `FirstIn` and `ListIn`, and `ClearIn` on
attachment handles, are additive. Existing `Clear` and `Detach`
retain their own-transaction behavior. Caller-transaction retirement is pending
until the outer commit; cleanup runs only afterward. No database migration or
generated contract update is required. See [attachment transactions](guides/attachments.md#deletion-and-inspection).

Enabled `application.LocaleSettings` now rejects a nonempty `Fallback` that
differs from `Default`, even when both are supported. Before upgrading, clear
`Fallback` to inherit `Default`, or make both equal to the intended common
fallback. The existing rejection of an unconfigured fallback remains. This
validation tightening changes startup behavior without changing persisted locale
data or removing the field. See [localization](guides/localization.md).

## R2 attachments

New variant generations use `foundry-attachments/<scope>/variants/<variant ID>`.
Existing `foundry-attachment-variants/<scope>/<variant ID>` keys remain readable
and eligible for journal cleanup. Regeneration creates a new key and retires
the previous row. No database migration or generated contract change is needed.
The bounded orphan sweep only lists the new scope prefix. Legacy variants use
their journal for recovery. See [attachment recovery](guides/attachments.md#publication-and-recovery).

## Ticket-authenticated routes: manifest 8, v0.4.0

Regenerate Go descriptors, manifest, OpenAPI and TypeScript together. Manifest 8
adds credential kind `ticket`; version 7 documents must be regenerated. The
OpenAPI scheme is `apiKey` in query parameter `ticket`. TypeScript credential
metadata retains its string kind and domain query DTOs omit the ticket.

Move `websocket.TicketRedeemer` references to `auth.TicketRedeemer`, including
`application.RealtimeDeclarations.Tickets`. There is no compatibility alias.
Replace `datatable.Tickets[M, A]{Issue, Redeem}` with
`datatable.TokenTickets(tokens)`, which returns `datatable.Tickets[M]`.
`datatable.IssuedTicket` and `DownloadQuery.Ticket` are removed.

Standard table downloads are guarded ticket routes and enforce `ExportAccess`.
Their handler resolves the actor through `Authority(ctx, actor)` in a scope that
stays open through streaming. Remove manual ticket redemption and temporary
redeemed-actor authority modes. Preserve separate queued-export authority where
needed. HEAD now returns 405 and does not consume a ticket. Follow the
[ticket security notes](guides/authentication.md#ticket-authenticated-routes).

## Framework-owned table routes — v0.3.2

`datatable.Routes` is additive; custom protocols continue to use the endpoint
constructors and binders. Standard routes retain the existing query, export and
download suffixes and JSON field names. Framework DTO schema identities replace
application DTO identities when an application adopts this helper. Regenerate
Go descriptors and clients together and update generated type imports; manifest
version 7 is unchanged. Format labels remain application-owned.

## Explicit table operations — manifest 7

Regenerate Go contracts, manifest, OpenAPI, and TypeScript together. Version 7
adds `Operation.TableOperation` and `Table.Page`; version 6 documents must be
regenerated. Declare associations with the datatable endpoint constructors and
binders. Route IDs, suffixes, and shared request/row schemas no longer create
helpers or retain tables in projections. There is no compatibility switch.

Queries require the canonical numbered page, including its metadata and links.
Migrate custom envelopes that previously supplied only a `data` array. A full
manifest rejects duplicate query/export/download slots before client selection.

`contract.TableID` replaces `datatable.TableID` without an alias. A DTO field
using this type now references `github.com/weiloon1234/Foundry-Go/contract.TableID`
in the schema graph; review regenerated TypeScript names and explicit imports.
Its string wire representation is unchanged.

Generated `TableMetadata<Row>` replaces `TableDescriptor`.
The mapped metadata registry `tables` and indexed row maps preserve generic
table access. Call operations only through `tableQueries`, `tableExports`, and
`tableDownloads`, each mapped over its own key set. Missing slots have no keys;
`tableExportKinds` exposes the literal ticket/file kind. Per-entry operation
methods and `createTableDescriptor` are removed. Ticket results retain the
application DTO. File exports and the new download helper return `FileResult`.
Server export capability does not promise a helper in every projected client.
See the [migration examples](guides/datatable.md#declare-http-operation-bindings).

## Application-error containment (H1)

Generated contracts and stored formats are unchanged. PostgreSQL configuration
adds the explicit `Config.RegisterTypes` hook and `TypeRegistry` described below.
Returned extension errors retain `errors.Is` and `errors.As` identity; routine
formatting now shows framework operation text instead of application error text.
Adapter classifiers may receive safely wrapped driver errors; use identity/type
inspection rather than requiring the original error at the graph root.
SQL and JSON conversion failures remain opaque inside native libraries and regain
their causes at the framework boundary. Previously discarded JSON causes remain
diagnostic-only for HTTP classification, preserving existing response bytes and
explicit outer classifications.

JSON conversion and HTTP error classification now contain `runtime.Goexit`, as
well as panic, and wait for the callback's actual exit. They return a safe failure
instead of ending the caller's goroutine. Custom methods must still terminate:
containment does not abandon an active callback or release its resources early.

The implementation adds internal conversion adapters and tests without changing
dependencies. Native SQL/JSON libraries inspect and format callback errors before
returning control to Foundry, so those boundaries require protected conversion.

Custom PostgreSQL type registration now belongs in
`Config.RegisterTypes func(context.Context, *postgres.TypeRegistry) error`.
The hook runs once per physical connection during setup. Its registry exposes
`LoadType(context.Context, string) (*pgtype.Type, error)`,
`RegisterType(*pgtype.Type) error`, and
`RegisterDefaultPgType(any, string) error`. These contained methods are valid only
while the hook runs. Foundry retains connection-owned descriptor graphs and
shared aliases. `LoadType` resolves the requested type under the original search
path, scopes native discovery to `pg_catalog, pg_temp`, then restores the path.
This is explicit connection-setup SQL, not an application query-observer event;
discovery or restoration failures prevent connection admission. See the [PostgreSQL guide](guides/postgresql.md#custom-postgresql-types)
for a qualified-name example.

This public addition replaces type-map mutation inside custom query rewriters.
Move native `RegisterType` and `RegisterDefaultPgType` calls into the hook.
Before and after custom rewriting, Foundry checks a synthetic cached encoding
plan through pgx's public API. Cache invalidation rejects the rewritten statement
with a safe error naming `postgres.Config.RegisterTypes`, closes the connection,
and does not automatically retry the operation. Native registration invalidates
the cache even when the caller reuses the same descriptor pointer, so such calls
inside a rewriter are also rejected. Rewriters that change SQL or arguments, or
execute unrelated SQL without changing registrations, remain supported.

There is no per-rewrite catalog query. The registration contract avoids a pgx
fork or reflection into its private registry. Existing applications without
custom type registration need no configuration change; generated contracts and
stored values do not change.

Application callbacks own calls they explicitly make inside their bodies,
including direct calls to native JSON functions. Foundry protects the callbacks
it dispatches and its own nested value helpers; containment cannot change an
application callback's implementation. Native caller-installed `WithMarshalers`
functions also retain their native precedence and callback ownership. Native
JSON entry points can return partial bytes alongside a contained error; callers
must reject that result whenever the error is non-nil.

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
manifests. Changes to binary ownership, native field capabilities, query
scopes or method signatures require regeneration and consumer compilation. A
successful runtime codec test alone does not establish declaration support.
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

## Validated framework capabilities (B31)

The framework-only `FoundryObservers`, `FoundryAutocommitQuery`,
`FoundryStatementUnknown`, `FoundryCollection` and `FoundryLinked` bridges now
use distinct validated internal seal types. Inferred zero seals fail closed;
`FoundryDiagnostic` also rejects a zero seal. These exported bridge signatures
are a documented security correction to the public Go surface, even though
ordinary application calls never need their internal arguments. Do not infer or
retain bridge tokens; use the ordinary database, relation and diagnostic APIs.

Observer lookup trusts concrete framework owners only, including pointer and
value forms of `PrimaryExecutor` and the internal enclosing transaction. Custom
wrappers, including wrappers embedding a pool, cannot prove observer absence: per-model
writes use their supplied transaction, and set-based writes require the existing
`WithoutModelHooks()` acknowledgement when absence is unknown. Adapter no-op
markers carry no capability and are never invoked; concrete package ownership
remains required. Application diagnostic methods are likewise never issued a
capability. Framework relation loaders now copy input slices before publication;
model fields retain ordinary Go value-copy semantics.

Normal caller signatures, generated declarations, persisted data and transport
formats are unchanged. Keep generator/runtime pins aligned; no generated-output
or database migration is introduced. The asset navigation hook (B30) is opt-in
and additive; see [assets and SPA routing](guides/http-assets.md).

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
| Model extension slots | Native handles declare slot names explicitly; `DefineORMOwner` takes an owner name and `OwnerOptions.StorageModel`. Preserve both names before renaming a Go field or a table; otherwise existing translations, metadata and files are hidden, not deleted. `translations`, `metadata` and `attachments` `undeclared` commands report stored names no slot declares. |
| Encrypted model fields | Opt-in native encrypted-column declarations retain their concrete plaintext and ciphertext types. Keep every key that encrypted stored values in `Encryption.Previous`: removing it makes those rows unreadable. Values are bound to table, column and primary key, so renaming either or rewriting a key needs re-encryption. |
| Browser refresh cookies | Opt-in: `TokenCookieResponse`, `RefreshTokenCookie`, `RefreshTokenCookieLogout` and `ClearRefreshCookie` add a cookie transport beside the unchanged JSON transport for the same guard. Endpoint metadata, the manifest and OpenAPI gain an optional `refresh_cookie` description (with `optional` for a cookie logout); existing operations export as before. `Tokens.LogoutRefresh` requires the backend to implement `token.RefreshRevocationBackend`, which the PostgreSQL backend does; a custom backend without it fails with `fault.Invalid`. |
| Client message formatting | Additive: entries export `catalogTranslations`, `formatText`, `formatMessage`, `CatalogMessageKey` and `CatalogMessageArguments`. The last two names are now reserved, so a schema with exactly that name receives a qualified TypeScript name. |
| Generated TypeScript layout | `contracts.export` now also publishes `<prefix>_runtime_foundry.gen.ts` and `<prefix>_runtime_realtime_foundry.gen.ts`, which the client entry imports; commit and serve them with the entry. The entry's exported API is unchanged. `manifestJSON` holds compact JSON of the same document, so code comparing its text must compare parsed values. `typescript.Render` still returns one self-contained module. Surfaces are additive; `typescript.ExportCommand` gains a variadic `Surface` parameter, so existing calls compile. |
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

## Typed query compilation footprint

Handwritten typed tables use `orm/expr` for expressions and `orm/rows` for
projections, sets and CTEs. Use `expr.AsSelected(row)` for selection. Shared
lowering preserves bindings, hydration and lifecycle behavior without a parallel
query runtime. See [typed field computations](guides/typed-field-computations.md)
and the [migration guide](guides/orm-migration.md#removed-api-spellings).
The [build measurements](guides/query-build-resources.md) retain historical cost
evidence; they do not describe a second supported runtime.

ORM columns are methodless. Per-kind package functions replace their operators,
for example `orm.TextEq(column, value)`. Query terminals and table operations are
also package functions, such as `orm.All(ctx, db, query)` and
`orm.Find(ctx, db, table, key)`. Expression and source APIs require explicit sealed
adapters, such as `expr.Value(orm.TextExpr(column))`. The old methods have no
compatibility aliases; migrate callers together. Model, value, input and role
constraints remain concrete. Query chains, CASE builders and window builders remain methods; expression comparisons and ordering are package functions.

Cursor request contract IDs now use the native ORM owner. Regenerate manifests,
OpenAPI and TypeScript together; cursor token encoding and model identity stay
unchanged. See the [publication evidence](evidence/goway-stage8d3c-report.md).

## Operational behavior

Datatable client-input rejection now matches `http.BadRequest` as well as the
existing `fault.Invalid`. HTTP handlers returning these errors unchanged receive
400 instead of 500. Remove blanket `fault.Invalid`-to-400 mappings when adopting:
server declaration/configuration and source failures must remain 500. There is no
persisted, generated-schema or catalog change. Table-specific request errors use
the existing generic public message; direct decoder causes/issue paths remain
available to Go callers.

Remote attachment imports now share `Manager.MaxActive` and its operation timeout
from the start of the download through publication. Imports waiting for capacity
do not contact the remote server or retain its body. Size limits, destination
restrictions and publication outcomes keep their existing contracts. Generator
discovery refuses import values that could be interpreted as Go command flags;
ordinary generation of declarations awaiting generated definitions still works.
Unavailable error responses omit `Retry-After` if custom error methods panic
during overload classification, while retaining the safe 503 response.

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
| Datatable downloads | Declare the download route with `http.Timeout(manager.DownloadTimeout())`; see [datatable exports](guides/datatable.md). |
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
