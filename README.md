# Foundry-Go

A strongly typed Go backend framework with Laravel-inspired developer experience
and **Fat Framework, Slim Project**. Applications own domain models, request and
response types, and business behavior. Foundry owns infrastructure assembly and
runtime lifecycle, with PostgreSQL as the database backend.

Teams building their own boilerplate should start with
[dependency installation and team adoption](docs/guides/team-adoption.md), including
the project-pinned CLI, generation, migrations and application CI. The
[final readiness review](docs/guides/final-readiness-20260927.md) records the latest
audit, fixes and independent package verification.

Start with [application bootstrap](docs/guides/application-bootstrap.md):
`application.New(settings)` composes configured services and HTTP routes through
ordinary typed constructors. [Generated configuration](docs/guides/generated-configuration.md)
shares one schema across Go defaults, TOML, environment variables and typed
overrides. [Named services](docs/guides/named-services.md) provide named instances
and an automatic default selection for each configured service family.

The [typed API workflow](docs/guides/typed-api-workflow.md) connects generated
request/response DTOs, validation, typed model binding, PATCH updates, idempotency,
transactions and durable outbox delivery. Its consumer fixture also exercises
generated clients and isolated HTTP database tests. Public APIs preserve concrete
Go types for compiler checks and IDE completion.

Use `foundry.New` and explicit providers when assembling the foundation directly.
Both entry points use the same foundation lifecycle; configured bootstrap owns
ordinary driver and service wiring.

## Current delivery

Milestones 01–24 are complete. The [master roadmap](blueprint/00-master-architecture-and-parity.md)
records native verification and parity evidence. Redis delivery includes typed
cache/tags, leases, distributed Remember, rate limits, pub/sub, typed data and
[scoped commands](docs/guides/redis-commands.md).

[Client contracts, OpenAPI and TypeScript](docs/guides/client-contracts.md) passed
milestone 21 native acceptance: shared manifest, exact numeric codecs, typed
HTTP/realtime clients, strict TypeScript and real transport interoperability.

[Plugin ecosystem](docs/guides/plugins.md) passed milestone 22 native verification: direct
typed contributions, independent modules, lifecycle, migration history and owned publication.

[Developer tooling and testing](docs/guides/developer-tooling-and-testing.md) passed
milestone 23 verification: typed CLI, scaffolds, doctor/inspection, test helpers,
[query plans](docs/guides/query-plans.md) and faster compiler/editor gates with
external-input cache tracking. [Production acceptance](docs/production-acceptance.md)
records explicit read routing, generated binary models, operational hardening,
independent packaged consumers, native measurements and the framework audit.

[Model-first authentication](docs/guides/authentication.md) is delivered: typed
permissions and guards, [sessions](docs/guides/sessions.md),
[browser CSRF/cookies](docs/guides/browser-sessions.md), [scoped tokens](docs/guides/tokens.md),
[password login](docs/guides/password-hashing.md), [lockout](docs/guides/login-lockout.md),
[account recovery](docs/guides/account-recovery.md), [MFA](docs/guides/mfa.md),
transactional revocation and [security events/maintenance](docs/guides/auth-operations.md).
Native tests, local PostgreSQL/Redis, races, compiler rejection, gopls and
reproducible generation passed. [Storage](docs/guides/storage.md) has passed native
framework/consumer checks, races, compiler/editor tests and parser fuzzing. Real
Cloudflare R2 and AWS S3 checks also passed, including AWS versioned reads and
cleanup. [Jobs and the worker kernel](docs/guides/jobs.md), atomic Redis workflows
and shared outbox publication passed milestone 12 acceptance. The
[scheduler](docs/guides/scheduler.md) passed milestone 13 acceptance, including
real Redis failover/overlap and native compiler/editor checks. The final native
framework gate and complete-framework audit/fix round passed; milestone 25 remains
deferred. Publishing and deployment remain operator actions.

[WebSocket channels and protocol](docs/guides/websocket.md) passed milestone 14
acceptance, including fresh authorization, safe presence, origin policy, bounded
queues and native kernel ownership. [Distributed realtime](docs/guides/websocket-distributed.md)
passed milestone 15 acceptance with two real Redis-backed servers, bounded replay,
TTL presence, typed revocation and protected diagnostics.

[Email](docs/guides/email.md) passed milestone 16 native verification and consumer
review, including local SMTP/TLS, provider contract fixtures and transactional
queue delivery. Real-account provider smoke sends remain unverified.

[Notifications](docs/guides/notifications.md) passed milestone 17 acceptance:
typed recipient/payload bindings, scoped inboxes, persistent per-channel delivery,
frozen email output, private realtime rooms and transactional jobs/outbox.

[Imaging](docs/guides/imaging.md), [attachments](docs/guides/attachments.md),
[model extensions](docs/guides/model-extensions.md), [settings](docs/guides/settings.md)
and [countries](docs/guides/countries.md) passed milestone 18 verification and
consumer review. Attachment publication and cleanup remain recoverable through
ordinary generated models and the existing jobs/outbox workflow.

[Datatables and reporting](docs/guides/datatable.md) passed milestone 19
verification and consumer review. Generated report DTOs supply shared column
metadata; authorized pages, counts and bounded CSV/XLSX exports reuse typed query
scopes and the existing HTTP/jobs/storage infrastructure.

- [HTTP kernel](docs/guides/http-kernel.md), [typed routing](docs/guides/http-routing.md), [endpoints](docs/guides/http-endpoints.md), [DTO contracts](docs/guides/http-dtos.md), [responses](docs/guides/http-responses.md) and [validation](docs/guides/validation.md).
- [Typed queries](docs/guides/http-queries.md), [composition/defaults](docs/guides/http-query-composition.md), [parameter contracts](docs/guides/http-parameter-contracts.md), [custom JSON](docs/guides/http-custom-json.md), [JSON map keys](docs/guides/http-json-map-keys.md) and [streaming JSON codecs](docs/guides/http-streaming-json.md).
- [Middleware](docs/guides/http-middleware.md), [CORS](docs/guides/http-cors.md), [trusted proxies](docs/guides/http-trusted-proxy.md), [public URLs](docs/guides/http-public-urls.md), [security headers](docs/guides/http-security-headers.md), [CSP](docs/guides/http-content-security-policy.md), [cookies](docs/guides/http-cookies.md) and [signed URLs](docs/guides/http-signed-urls.md).
- [Numbered/simple pagination](docs/guides/http-pagination.md), [cursor pagination](docs/guides/http-cursor-pagination.md) and [route-model binding](docs/guides/http-model-binding.md), preserving explicit response DTOs and model getter choices.

[Typed multipart uploads](docs/guides/http-uploads.md) passed focused, canonical
and combined full regression acceptance, including generated consumer APIs and
bounded streaming allocations. [Typed downloads](docs/guides/http-downloads.md)
passed focused and canonical runtime, consumer, compiler and editor acceptance.
[Typed stream responses](docs/guides/http-streams.md) cover finite unseekable
sources, exact length/byte limits, cancellation and cleanup. Their combined full
regression also passed, including all 633 compiler cases and three generation
freshness targets. [Static assets and SPA routing](docs/guides/http-assets.md)
passed focused lifecycle, consumer, compiler, editor and resource checks, followed
by full canonical regression with all 638 compiler cases and current generation.
[Automatic ETags](docs/guides/http-etags.md) passed focused runtime, consumer,
compiler, editor and resource checks, followed by full canonical regression
with all 639 compiler cases and current generation. [Response compression](docs/guides/http-compression.md)
now provides bounded gzip/Brotli streaming and passed focused runtime, consumer,
compiler and editor checks, followed by full canonical regression with all 641
compiler-rejection cases and current generation. Milestone 08 is complete.
milestone 25 records deferred extensions.

[Typed auditing](docs/guides/audit.md) records stored model changes and explicit domain events through the actual transaction, with generated field policies, redaction, model-owned history, provenance and bounded retention. Runtime, PostgreSQL consumer, compiler-rejection, actual-gopls and full generator race acceptance passed. The model lifecycle, events and audit milestone is complete.

[Typed JSON models](docs/guides/json-models.md) preserve concrete Go payloads through immutable snapshots, generated fields/drafts, PostgreSQL JSONB codecs, containment, kind inspection and projections. Generated properties, typed map keys and array indices support nested queries without repeating JSON field names. SQL NULL, JSON null and omitted writes remain distinct.

[Typed lateral joins](docs/guides/lateral-joins.md) select complete models and generated reports per preceding row, with explicit correlation ownership, per-parent windows and preserved outer nullability.

[Typed temporal queries](docs/guides/temporal-calculations.md) provide date/time components, truncation, explicit timezone conversion, calendar/elapsed arithmetic and exact Unix milliseconds while preserving scope, NULL and selected-expression phases.

[Typed interval queries](docs/guides/interval-queries.md) add interval models, nullable codecs, sums/averages, differences and dynamic calendar shifts. Eager relations match PostgreSQL interval equality while retaining stored components.

[Typed RANGE frames and named windows](docs/guides/window-ranges-and-names.md) retain numeric distance types, calendar/elapsed interval meaning and reusable window definitions within their SELECT scopes.

[Computed query keys](docs/guides/computed-query-keys.md) support typed grouping, window partitions and DISTINCT ON over row computations and selected aggregate/window values, preserving scope ownership and bound parameter identities.

The [blueprint suite](blueprint/README.md) specifies the full framework roadmap. Available foundation APIs include application assembly, provider lifecycle, typed constructor injection and configuration, logging, model-specific identities, temporal values, and consumer testing helpers. Read the [foundation guide](docs/guides/foundation.md) and [typed values guide](docs/guides/typed-values.md) for executable reference examples.

The [model generation guide](docs/guides/model-generation.md) covers compiler-checked fields, mutation drafts and enums generated from handwritten Go structs. [Typed model reads](docs/guides/model-queries.md) execute through a shared SQL compiler, with complete hydration, typed key lookup, streaming and aggregates. [Typed model writes](docs/guides/model-writes.md) add creation, scoped updates/deletes, explicit defaults and transactional returning rows. [Model pagination](docs/guides/model-pagination.md) preserves typed models and stable ordering. [Lifecycle hooks](docs/guides/model-hooks.md), [managed timestamps](docs/guides/model-timestamps.md) and [soft deletion](docs/guides/model-soft-deletes.md) integrate typed writes with application behavior. HTTP transport is delivered; storage passed live AWS S3 and R2 checks. Worker, scheduler and WebSocket runtimes are delivered. The kernel APIs share the foundation lifecycle; remaining production work follow the [master roadmap](blueprint/00-master-architecture-and-parity.md).

The [database runtime](docs/guides/database-runtime.md) implements connector-based pooling, application lifecycle integration, raw parameterized SQL, streaming, transactions, sessions, savepoints, and after-commit outcomes. The [PostgreSQL adapter](docs/guides/postgresql.md), [migrations, seeders, consumer commands, and scaffolding](docs/guides/migrations-and-seeding.md) have protocol, real PostgreSQL, and independent consumer coverage.

[Primary/read routing](docs/guides/database-routing.md) and [binary model fields](docs/guides/binary-models.md) passed milestone 24 verification.

[Shared observations](docs/guides/observability.md), [protected diagnostics](docs/guides/production-diagnostics.md), [readiness and maintenance](docs/guides/readiness-and-maintenance.md), bounded HTTP admission and [versioned queued trace propagation](docs/guides/job-trace-rollout.md) also passed the milestone 24 gate. See the consolidated [production acceptance](docs/production-acceptance.md).

[Release checks](docs/release-checklist.md), [compatibility policy](docs/compatibility.md),
[operations](docs/guides/production-operations.md), [dependency review](docs/dependency-review.md)
and [native resource measurements](docs/guides/developer-resource-measurements.md)
describe the accepted production gate and its operational limits. The project uses the approved
[MIT license](LICENSE).

[Typed database codecs](docs/guides/database-codecs.md) preserve named scalars, enums, model IDs, exact decimals, nullable states and temporal meaning. Generated enum SQL interfaces, model predicates and model hydration share these codecs.

Only the framework is being built here. Rust Foundry is the feature reference; Rust Foundry-Starter illustrates future consumption. This repository does not contain or create Foundry-Go-Starter. Test fixtures are not application templates.

[Typed relations](docs/guides/model-relations.md) provide belongs-to, has-one, has-many and many-to-many with concrete pivot models, batched scopes and nested eager loading. [Typed relation writes](docs/guides/model-relation-writes.md) add lifecycle-aware attach/detach with derived keys, endpoint locking and bounded atomic removal. [Per-model batch writes](docs/guides/model-batch-writes.md) run ordinary lifecycle behavior per row within a bounded atomic operation. [Lookup writes](docs/guides/model-lookup-writes.md) add typed `FirstOrCreate` and `UpdateOrCreate` with lazy branch preparation, stored-scope checks and explicit concurrency behavior. [Typed relation aggregates](docs/guides/model-aggregates.md) add computed counts, extrema, sums and averages. [Declared projections](docs/guides/model-projections.md) return separate typed records for partial reads, grouped reports and scalar summaries, with typed HAVING filters and aggregate ordering. [Typed joins](docs/guides/model-joins.md) add model aliases, inner/outer/self joins, nullable scopes, inferred projection builders and complete-record selection through `SelectRecord`. Query builders remain separate from returned models and ordinary typed Go slices. Typed query and model-lifecycle milestones 05–07 are complete.

The [agent language tooling client](docs/guides/agent-language-tooling.md) requests completion, hover, and definition from gopls using unsaved consumer buffers. Real gopls acceptance passes against the consumer's generated model APIs.

[Typed CTEs](docs/guides/model-ctes.md) preserve model and projection records across reusable definitions, joins, subqueries and scoped writes, with explicit materialization choices and dependency ordering. [Recursive CTEs](docs/guides/model-recursive-ctes.md) add typed anchors and owned working-table references for model and report hierarchies. See the [milestone 06 completion review](blueprint/06-relations-and-advanced-queries.md#completion-review) for the delivered advanced-query boundary.

[Typed set operations](docs/guides/model-set-operations.md) combine complete model/projection records and single values with union, intersection and difference. Input and output scopes/windows remain separate, and combined results reuse the compiler, codecs and streaming runtime.

[Typed distinct reads](docs/guides/model-distinct.md) deduplicate complete selected models, reports or values, with PostgreSQL `DistinctOn` for one ordered record per typed key combination. Counts preserve distinct result cardinality and pagination.

[Typed window expressions](docs/guides/model-windows.md) add rankings, distributions, neighboring/frame values and per-row aggregates with scoped partitioning, ordering and frames. They reuse declared projections, typed codecs and the shared SQL compiler.

[Typed aggregate filters](docs/guides/model-aggregate-filters.md) restrict individual measures in grouped reports, windows and relation slots while preserving scoped predicates, result types and cardinality checks.

[Typed conditional expressions](docs/guides/conditional-expressions.md) add CASE, COALESCE, NULLIF and standalone bound parameters. Row expressions preserve predicate ownership; selected counterparts compose with aggregates and windows while retaining explicit nullability and codec types.

[Typed pagination](docs/guides/model-pagination.md) includes count-free simple model pages and numbered/simple pages for projections, sets and selected values. Model pages append the primary key; report pages require explicit ordering. Eager loading excludes hidden lookahead rows, and page failures discard partial results and metadata.

[Bounded model iteration](docs/guides/model-chunks.md) processes typed slices or individual models in offset or primary-key batches. Eager `Each` uses the same batching; rows close before chunk callbacks, and natural keys, selected limits and relation budgets stay explicit.

[Typed arithmetic and text calculations](docs/guides/scalar-calculations.md) compose model fields, conditional values, projections, aggregates and windows through the same expression AST. Computed model and relation ordering retains typed results, explicit NULL behavior and bound parameters.

[Value comparisons and joins](docs/guides/value-comparisons-and-joins.md) connect computed operands to WHERE, HAVING and typed ON conditions. Cartesian joins preserve aliased input boundaries, while explicit row-subquery helpers retain nullability, inner SELECT phases and correlation ownership.

[Typed upserts and batch inserts](docs/guides/model-upserts.md) preserve generated drafts, composite conflict targets, nullable/constant/incoming assignments and complete model results. Bounded batches execute atomically, and skipped conflicts have explicit absence semantics. [Typed insertion from queries](docs/guides/model-insert-from-query.md) adds model-specific stored-value selectors, literal drafts, affected counts and bounded complete-model results through the shared SELECT compiler. [Typed source updates and deletes](docs/guides/model-source-writes.md) preserve source scopes and destination keys, reject ambiguous SQL-mapped updates, and retain fixed draft setters and ordinary deletion visibility. [Automatic field mutators](docs/guides/model-mutators.md) normalize assigned model values inside the write transaction through compiler-checked handwritten methods.

[Generated model changes](docs/guides/model-changes.md) retain typed persisted snapshots and field sets, distinguishing assignment from changes in stored values, model absence and NULL. [Typed model hooks](docs/guides/model-hooks.md) connect declared create/update/delete callbacks and automatic change capture to the transaction pipeline.

[Transaction-scoped row locking](docs/guides/row-locking.md) retains typed model keys, projection/value results and join targets across four PostgreSQL lock strengths. Locked reads require an explicit transaction, with waiting, immediate failure or skip-locked selection.

[Result cursor pagination](docs/guides/result-cursor-pagination.md) uses typed output fields and explicit `UniqueBy` identities for complete reports, joins, CTEs, sets and scalar values. It preserves completed-query semantics and returns ordinary slices with nullable, bidirectional navigation.

## Development

The required Go toolchain is declared in [go.mod](go.mod). Local development commands run natively on macOS; see the [contributing guide](docs/guides/contributing.md).

```sh
make verify
make race
```

[Generated model references and attribution](docs/guides/model-references.md) retain concrete stored keys and capture model/system provenance without copying complete models. They reuse database codecs and preserve explicit getter semantics. Runtime, PostgreSQL, compiler-rejection, real-gopls and full-repository acceptance have passed.

[Typed events](docs/guides/events.md) preserve payload types through registration, ordered dispatch and model after-commit observers. Listeners receive independent payloads; rollback suppresses queued callbacks. Runtime and consumer races, compiler and gopls checks, and full repository acceptance passed.

The optional [TOML configuration adapter](config/toml) and [PostgreSQL adapter](database/postgres) use approved dependencies pinned in `go.mod`; foundation packages do not import them. gopls is a separate development tool. `make verify` checks formatting, static analysis, root packages, the independent consumer module, stale generated output, local documentation links, blueprint numbering, and fixture toolchain alignment. Consumer tests exercise lifecycle behavior and assert compilation failures for incompatible public types. `make test-postgres` runs required database acceptance with race detection against an explicitly supplied isolated test database.

The intended module path is `github.com/weiloon1234/Foundry-Go`. No remote repository or release is created by this scaffold.

## Architecture and progress

[Derived records and subqueries](docs/guides/model-subqueries.md) compose complete reports as query sources and preserve concrete result types through single-value, membership, existence and scalar queries.

[Correlated subqueries](docs/guides/model-correlations.md) bind typed inner queries to an outer row, including nested references, nullable joined records and column comparisons. Ordinary model calls use generated field and relationship descriptors; raw SQL is an explicit extension boundary.

[Relationship filters](docs/guides/model-relationship-filters.md) provide `WhereHas` and `WhereDoesntHave` for direct and many-to-many relations. Nested self-relations and target/pivot scopes use automatic aliases, while terminal collection reads return ordinary typed Go slices.

Read [master architecture and parity](blueprint/00-master-architecture-and-parity.md) for the agreed scope, module mapping, dependencies, implementation status, and outstanding checks.

Future API examples in blueprints are explicitly marked as planned. They are not a claim that the corresponding package or method is implemented.

[Transactional outbox](docs/guides/outbox.md) stores typed event payloads and attribution in the business transaction using a framework-owned generated model. Model/message commit and rollback, failure handling, clean-checkout generation, compiler/gopls checks and full repository acceptance passed. Milestone 12 completed [transactional publication and worker delivery](docs/guides/jobs.md#transactional-publication).


Typed [rate limiting](docs/guides/rate-limiting.md) preserves domain key types across
HTTP and other kernels with explicit memory or Redis authority. Focused and full
native verification pass.

Typed [pub/sub](docs/guides/pubsub.md) now preserves resource and payload types
through explicit memory or Redis fan-out. Focused and full native verification
pass. Milestone 09 subsequently passed its complete acceptance and consumer review,
including typed Redis data and scoped commands.

Cache-wide invalidation is now implemented as `store.Invalidate(ctx)`, with automatic
namespace snapshots across native memory/Redis values, tags, counters and fills.
It passed full native verification; [the guide](docs/guides/caching.md#invalidate-a-complete-cache-namespace)
explains the consumer API and retention semantics.

Typed cache and counters now expose `Exists`, `Expire` and atomic `ForgetMany`;
[consumer examples](tests/fixtures/consumer/caching/entries.go), focused races and
compiler/editor checks and full native verification pass.

Typed [Redis hashes and sets](docs/guides/redis-data.md) now have concrete consumer
examples and native adapters. Full native acceptance passed.

[Scoped Redis commands, pipelines and scripts](docs/guides/redis-commands.md) now
preserve concrete result types and explicit keys. Focused and full native checks
passed, and milestone 09 is complete after source/parity and consumer review.

Authentication passed its consolidated verification/fix cycle.
See [auth operations](docs/guides/auth-operations.md) and the
[auth source-parity inventory](blueprint/10-model-first-authentication-and-authorization.md#source-parity-closure).
Milestone 10 is accepted; the complete foundation and consumer-startup acceptance are recorded in the master roadmap.

[Localization](docs/guides/localization.md),
[supporting APIs](docs/guides/supporting-apis.md) and
[outbound HTTP clients](docs/guides/http-client.md) passed milestone 20 verification
and consumer review. Typed generated messages reuse the DTO contract; named HTTP
clients own bounded operations, streams and explicit retries.

The consumer startup continuation adds [named services and defaults](docs/guides/named-services.md) over the existing lifecycle.
- [Application bootstrap](docs/guides/application-bootstrap.md) — configured HTTP and typed constructor dependencies.
- [Compact consumer and acceptance](docs/guides/consumer-startup-acceptance.md) — runnable typed startup and final C05 proof matrix.
- [Supporting startup services](docs/guides/supporting-services.md) — C04 named defaults, typed declarations and lifecycle.

### Security and production boundaries

See [private vulnerability reporting](SECURITY.md),
[restricted outbound clients](docs/guides/http-client.md),
[inbound webhook verification](docs/guides/webhooks.md), and
[online migrations and reconciliation](docs/guides/migrations-and-seeding.md).
