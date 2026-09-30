# Independent consumer fixture

The module pins the development CLI with a Go `tool` directive; `go tool foundry`
uses the same framework version as application imports. The
[pinned-tool check](pinned_tool_test.go) exercises doctor and generation.
See [team adoption](../../../docs/guides/team-adoption.md) for a new project.

The [tooling consumer](tooling/commands.go), [HTTP/factory tests](tooling/tooling_test.go)
and [local capability recipes](tooling/helpers_test.go) exercise milestone 23's
public command, inspection and test APIs. [Database recipes](tooling/database.go)
preserve transaction requirements for query analysis. Twelve new compiler cases
and eight editor scenarios accompany the implementation. Native verification,
relevant races and the complete 915-case compiler/365-scenario editor gates passed;
see the [tooling guide](../../../docs/guides/developer-tooling-and-testing.md).

The [articles consumer](articles/article.go) declares translated text,
attachments and typed metadata as model extension slots, registers them with
`Builder.Models` and exercises generated deletion cleanup against PostgreSQL;
see the [slot guide](../../../docs/guides/model-extension-slots.md).

The [portal consumer](spaportals/portals.go) declares browser portals with
`Builder.SPA` beside a root public mount, content-hashed bundles and a typed API
route, and checks client-route fallback, precedence and Build-time rejection;
see [SPA fallbacks with application.New](../../../docs/guides/http-assets.md#spa-fallbacks-with-applicationnew).

The [plugin consumer](pluginusage/bootstrap.go) imports independent
[base](../plugin_base/README.md) and [dependent](../plugin_dep/README.md) plugin
modules with main-module replacements and `GOWORK=off`. Milestone 22 acceptance
passed actual worker/event/route use, configuration, asset/scaffold publication,
retained PostgreSQL migration history, ten new compiler-rejection cases and seven
new editor probes. Independent ordinary/race and full regression checks passed;
see the [plugin guide](../../../docs/guides/plugins.md).

The [reporting consumer](reporting/members.go) declares generated response
projections, typed table columns, scoped authorization, joined/grouped reports,
HTTP downloads and an ordinary export job. Its public PostgreSQL/auth/HTTP,
worker retry/local-storage, race, sixteen compiler-rejection and six real-editor
checks passed milestone 19 acceptance; see the [guide](../../../docs/guides/datatable.md).
[Member imports](reporting/member_import.go) stream CSV/XLSX uploads into a typed
row with declared heading codecs, per-row validation and row-numbered failures.

The [WebSocket consumer](realtime/orders.go) binds model-owned rooms, generated
DTOs, typed event handlers and an injected domain service. Its native middleware
and WebSocket kernel tests, typed relay/replay, eleven compiler-rejection cases
and six editor scenarios passed milestone 15 acceptance.

The [scheduler consumer](scheduling/reports.go) declares a daily report in an
explicit business timezone and injects a domain service through the Scheduler
module. Its kernel, race checks, typed compiler cases and editor probes passed
milestone 13 acceptance.

The [recovery HTTP consumer](recovering/routes.go) keeps model/purpose-owned tokens
in generated bodies and completes reset/verification without logging in.
[Public link requests](recovering/link_routes.go) compose the framework requester,
recipient/IP limits and stored-address delivery. Their PostgreSQL/HTTP tests and
generation passed milestone 10 acceptance. See the [recovery guide](../../../docs/guides/account-recovery.md).

The [MFA consumer](multifactor/routes.go) now composes encrypted enrollment,
confirmation/recovery disclosure and pending browser/API credential completion.
Its [PostgreSQL completion cases](multifactor/completion_postgres_test.go) and
[HTTP cases](multifactor/http_postgres_test.go), together with generation, passed
milestone 10 acceptance. The [guide](../../../docs/guides/mfa.md)
describes transaction, CSRF, secret delivery and failure boundaries.

The [audit consumer](auditqueries/audit_postgres_test.go) checks automatic stored-value history, getters/setters, exact decimals, nested redaction, natural keys, special deletion, rollback, missing storage and bulk/per-row semantics. [Registration and domain code](auditqueries/domain.go) keep the consumer thin; the [audit guide](../../../docs/guides/audit.md) describes its public contracts.

The [reference consumer](mutatorqueries/reference_test.go) checks concrete model-owned and natural keys, immutable attribution and exclusion of unrelated model fields. See the [guide](../../../docs/guides/model-references.md) for the explicit identity serialization boundary.

The [source-write consumer](linkqueries/source_write_postgres_test.go) covers typed UPDATE FROM and DELETE USING, duplicate-match rollback, fixed inputs, windows, nullable/natural keys, joins/CTEs, setters/timestamps, soft/physical removal and trigger suppression. Its [guide](../../../docs/guides/model-source-writes.md) and master roadmap record acceptance status.

The [insert-from-query consumer](linkqueries/insert_select_postgres_test.go) exercises typed identities, nullable values, database defaults, exact decimals, typed JSON, setter/timestamp ownership, source windows and CTEs, trigger suppression, result bounds and transaction rollback. See the [guide](../../../docs/guides/model-insert-from-query.md) and master roadmap for acceptance status.

The [lookup-write consumer](linkqueries/lookup_postgres_test.go) exercises branch choice, lazy draft preparation, normal lifecycle, callback transaction I/O, scopes, natural keys, soft visibility and rollback. [Concurrency acceptance](linkqueries/lookup_concurrency_postgres_test.go) checks locks before callbacks and competing missing-row creations under a real unique index. See the [lookup-write guide](../../../docs/guides/model-lookup-writes.md) and master roadmap for current verification status.

The [per-model batch consumer](linkqueries/each_postgres_test.go) exercises concrete drafts and callbacks, per-row lifecycle time, limits, primary ordering, soft deletion/restoration, transaction ownership and failure rollback. [Mode tests](linkqueries/each_modes_postgres_test.go) retain explicit bulk behavior, physical deletion and natural keys; [concurrency acceptance](linkqueries/each_concurrency_postgres_test.go) checks complete candidate locking and exclusion of later inserts. See the [batch-write guide](../../../docs/guides/model-batch-writes.md) and master roadmap for verification status.

The [timestamp models](timequeries/models.go) exercise conventional creation/update time, an explicit opt-out and mixed Go instant types. PostgreSQL acceptance covers hook order, effective changes, trigger output, rollback, nested ownership, bulk/upsert semantics and clock failure isolation. Compiler fixtures reject wrong clock and timestamp setter types; actual gopls checks draft signatures, automatic field notices and clock definitions. The master roadmap records verification status.

The [distinct input model](inputqueries/models.go) accepts concrete email/label inputs and stores separately named values. Its PostgreSQL tests cover before-hook inputs, stored change snapshots, omitted/NULL/zero values, normalization, rollback, bulk writes and reusable conflict assignments. Compiler cases reject stored values at input setters, fresh inputs in stored predicates, nullable result mismatches and foreign scopes. Real gopls inspects draft/conflict setters and combined input/getter notices at the handwritten field. Draft diagnostics omit captured values.

The [retrieval models](retrievalqueries/models.go) combine explicit getters, automatic mutators, local retrieval hooks and injected observers. Their PostgreSQL tests cover callback order, one-connection wrapper I/O, stored-value/DTO separation, direct and through relations, explicit loading, pagination, composed queries, bounded iteration and rollback on failure. Compiler cases reject wrong model callbacks, observer owners, factory results and write/read hook confusion; gopls inspects generated retrieval signatures in this consumer workspace. See the [retrieval guide](../../../docs/guides/model-retrieval.md) for semantics and the master roadmap for current verification status.

The [automatic mutator model](mutatorqueries/member.go) and [PostgreSQL acceptance](mutatorqueries/mutators_postgres_test.go) cover normalized create/update, nullable and omitted fields, final enum validation, immutable drafts, mutator veto/panic, nested rollback and normalized bulk/upsert inputs. Compiler-failure fixtures retain field value, model and nullability types; real gopls resolves the handwritten methods and generated drafts.

The [transaction composition examples](transactionqueries/) retain SELECT-owned locks through CTEs, derived records, inner/outer/cross joins and generated projections. Independent PostgreSQL transactions check skip-locked limits, lock strength, Count/Exists, nested savepoints, cancellation and validation before SQL. Compiler-rejection fixtures enforce source separation, transaction-only execution and exact scopes/results; real gopls inspects the generated builders and terminal contracts.

The [transaction correlation examples](transactionqueries/correlations_postgres_test.go) add typed parent/child and nested-ancestor scopes, generated correlated projections, all three lateral join kinds, nullable lateral parents, per-parent locks and skip-locked candidates. Invalid outer lock targets and grouped lock selections fail before SQL. Separate compiler cases prevent standalone correlated execution, ordinary-source conversion, foreign parents and nullability loss.

The [JSON examples](jsonqueries/) exercise typed immutable payloads, exact decimals, whole-document containment, generated nested/recursive properties, typed map/array access, quoted scalars, JSON kinds, nullable projections, partial updates, eager relations and canonical natural keys. Malformed stored documents fail complete hydration; separate negative-compilation fixtures reject unrelated payloads, owners, scalar values, keys and query phases.

The [RANGE and named-window examples](framequeries/) cover exact decimals, integer/float ordering, nullable peers, descending frames, calendar/DST boundaries, month ends, interval styles, shared definitions, CTE discovery and correlated windows. They retain generated sample models and native result slices.

The [computed key examples](keyqueries/) exercise typed grouped reports, computed partitions and distinct models/values, selected aggregate/window keys, CTE/correlation composition and invalid scope/phase boundaries. Results retain native slices and generated projection types.

The [temporal query examples](temporalqueries/) exercise typed extraction, truncation, timezone and DST behavior, calendar versus elapsed arithmetic, exact Unix milliseconds, NULL propagation, CTE/correlation/window composition and bounded result codecs. Compiler-failure examples reject mixed temporal types, units, scopes, phases and untyped zones/intervals.

The [interval examples](intervalqueries/) exercise all four PostgreSQL output styles, bounded interval decoding, nullable mutations, exact components, dynamic shifts, normalized differences, summaries/windows, SQL-equivalent natural keys, eager relations and cursor ties. Invalid values, ownership, query phases and result types fail Go compilation.

The [conditional expression examples](expressionqueries/) use typed row values, CASE branches, nullable fallbacks and codec-owned parameters. They check numeric CASE ordering, grouping and windows, CTEs/correlations, typed projections, relation aggregates and pre-execution failures. Separate negative-compilation cases preserve owner/value/nullability and row/selected phase boundaries.

The [aggregate filter examples](filterqueries/filters_postgres_test.go) cover independently filtered grouped measures, windows, nullable results, relation slots and CTE/correlation composition. The [relation aggregate tests](model_aggregates_postgres_test.go) also check filtered target/pivot inputs and cardinality failures. Six invalid compiler fixtures protect filter ownership, value types, row/group boundaries and result capabilities.

This independent module imports the public Foundry-Go root package through a local replacement. Run it with `GOWORK=off`; [the root Makefile](../../../Makefile) does this automatically.

The [lifecycle test](lifecycle_test.go) loads typed configuration, resolves concrete services, boots two fake providers, runs a cancellable kernel with an injected clock, and verifies reverse-order cleanup. The [value test](values_test.go) exercises model-owned IDs and typed nullable patches. The [compiler tests](types_test.go) invoke the selected Go compiler against intentionally invalid packages under `testdata/compilefail`; those failures are required assertions, not broken framework packages.

The [handwritten models](models/models.go) own model fields, column tags, enum constants, and business methods. The [generated API tests](generated_test.go) verify typed queries, mutation states, self-reference IDs, natural keys, enum serialization, SQL codecs and contract descriptors. The [catalog model](catalog/product.go), its [separate enum package](catalog/status/status.go), and [package graph test](package_graph_test.go) exercise generated dependencies across package boundaries. Generated files and each package's `.foundry-gen.json` are generator-owned. Root `make generate` processes this entire consumer package graph; `make verify` checks that outputs are current.

The [configuration test](configuration_test.go) consumes the optional TOML adapter, loading typed scalar/collection settings and applying a typed override with source provenance. Its parser dependency follows the framework's pinned module version.

The [codec tests](codecs_test.go) reuse generated enum codecs and preserve named natural keys. The [ledger model](models/ledger.go) exercises exact decimal fields and nullable drafts. Compiler-failure cases reject wrong codec owners/values, float decimal assignments and unsupported decimal text operators. The PostgreSQL consumer also verifies generated enum codecs against valid and malformed stored values.

The [generated model query acceptance](model_query_postgres_test.go) executes typed reads, complete hydration, filtered primary-key lookup, exact decimal/natural-key models, window aggregates and streaming against its own isolated schema. It rejects partial results from malformed stored rows. Query keys remain typed after fluent derivation, with required compilation failures for mismatched owners and natural-key types.

The [generated model write acceptance](model_write_postgres_test.go) creates, patches and deletes isolated fixture records using typed drafts and keys. The [write-record model](models/write_record.go) declares database-owned defaults. Coverage includes omission versus zero/null, UUID preparation, exact decimals, filtered writes, failed hydration, uniqueness and nested rollback. Negative compiler fixtures reject incompatible write drafts and keys.

The [row-locking acceptance](lockqueries/locking_postgres_test.go) exercises competing transactions, lock-strength compatibility, skip-locked slices, natural-key lookup, offset locks, savepoint release/rollback, context cancellation, typed join targets, eager parent reads and stream cleanup. Compiler failures enforce transaction ownership, exact keys/results and non-nullable join scopes; real gopls inspects the generated and shared locking methods.

[Per-input locking](lockqueries/clauses_postgres_test.go) declares independent lock strengths and waiting policies for joined tables. It checks actual contention and PostgreSQL precedence for overlapping clauses. Separate compiler failures reject foreign owners, nullable targets, explicit owner conversions, pool/session execution and ordinary source conversion.

The [result cursor acceptance](cursorqueries/cursors_postgres_test.go) exercises complete projections/models, aliases, ordinary/recursive CTEs, sets, grouped exact totals, joined composite identities, distinct winners and window values. It checks forward/backward nullable traversal, page-size changes, single-query execution, source windows, mismatched tokens, malformed lookahead and oversized output tokens. Public compiler fixtures reject wrong result owners, input/output scopes and unrestricted cursor composition.

The [pagination acceptance](model_pagination_postgres_test.go) tests numbered/simple pages and cursor traversal with natural keys, ties, nullable sorts, mixed directions, exact decimals, changed query/model rejection and inserts around a retained keyset position. The model read fixture adds temporal/UUID cursor boundaries and failed lookahead hydration. Negative compiler assertions reject incompatible cursor owners/requests and partial response records used as write drafts.

The [projection/simple page acceptance](pagequeries/pages_postgres_test.go) checks grouped/distinct/join/set totals, nested limits, nullable and exact result codecs, window evaluation, count-free lookahead, eager-load budgets, malformed hidden rows and all-or-error page metadata. Public compiler cases preserve page result ownership/nullability and reject unavailable simple-page totals. Real gopls inspects typed model and projection page methods.

The [chunk acceptance](chunkqueries/chunks_postgres_test.go) checks bounded offset/key traversal, selected windows, descending natural keys, callback mutation, filter updates, eager `Each`, relation budgets and same-transaction work after rows close. Failures cover decoding, eager cardinality, later reads, cancellation and callback errors/panics. Compiler cases preserve callback model ownership, and gopls inspects typed batch and individual callbacks.

The [upsert acceptance](upsertqueries/upserts_postgres_test.go) covers generated single/batch upserts, composite and named conflict targets, defaults, typed values/NULLs, skipped rows, retained UUIDs, concurrent writers, reserved target names, duplicate-target rollback and failed batch hydration. [Conflict calculations](upsertqueries/calculations_postgres_test.go) add generated stored/proposed field scopes, exact decimals, nullable values, trigger/default behavior, scalar and correlated assignments, CTE dependencies and rollback after scalar cardinality errors. [Index targets](upsertqueries/index_targets_postgres_test.go) exercise typed expressions and partial predicates under generic prepared plans, static quoted constants, conditional updates and unmatched index errors. Negative compiler cases preserve destination, scope, value, nullability and row/selected-phase rules. Gopls inspects generated methods, conflict scopes, typed assignment construction and index declarations.

The [relation declarations](models/relations.go) use generated Go fields to connect typed keys. [PostgreSQL relation acceptance](model_relations_postgres_test.go) verifies batched/nested self-relations, nullable inverse keys, loaded states, scopes, cardinality, natural keys and query/resource bounds. The catalog model also targets a generated model in another package. Compiler failures cover wrong relation keys, owners, targets and scoped predicates.

The [many-to-many acceptance](model_through_postgres_test.go) loads concrete [membership pivots](models/membership.go) beside each group and self-referencing friendship targets. It verifies combined target/pivot ordering, distinct duplicate edges, nullable keys, separate scopes, nested target/pivot relations, query counts, loaded states, ambiguous joins and failed pivot hydration. Compiler assertions reject incompatible pivot keys, owners, scopes, ordering and payloads.

The [aggregate declarations](models/aggregates.go) and [PostgreSQL aggregate acceptance](model_aggregates_postgres_test.go) cover generated computed slots, exact sums beyond int64, fractional averages, nullable decimal/float summaries, text extrema, scoped pivot aggregates, distinct targets, nested loading and failure budgets. Aggregate compiler assertions reject wrong owners/results/nullability and numeric operations on non-numeric fields.

The [projection declarations](reports/projections.go) and [projection acceptance](projectionqueries/projections_postgres_test.go) verify partial reads into complete separate records, grouped/scalar exact summaries, imported enums, nullable promotion, result-window counting, mapping validation, cancellation and failed decoding. Typed HAVING filters, aggregate ranking and empty/NULL group conditions exercise the shared compiler. Negative fixtures check ownership, value/nullability types, row/group predicate separation and the absence of model writes on projections. Real gopls resolves generated selection definitions in the reports package and inspects typed aggregate condition methods.

The [join acceptance packages](joinqueries) and [joined result declarations](reports/join_projections.go) exercise inner/left/right/full self-joins, source filters/windows, composite/alternative ON conditions, natural keys, grouped reports and chained nullable scopes. Smaller inner, outer/chained and natural-key packages share fixture setup to bound compiler memory without removing coverage. Generated projection builders infer the joined scope. Compiler assertions reject wrong alias/key/side ownership, unlifted field scopes and erasure of outer nullability; gopls inspects generated scoped fields and fluent selection signatures in the consumer workspace.

The [subquery acceptance](advancedqueries/subqueries_postgres_test.go) composes declared reports, typed membership and scalar values. The [correlation acceptance](correlations/correlations_postgres_test.go) adds per-user aggregates, nested parent/grandparent references, grouped correlation, scoped writes and many-to-many filter qualification. [Nullable joined correlations](correlations/nullable/nullable_postgres_test.go) run in a smaller compilation package while preserving their outer/inner NULL checks. These packages share an isolated [query fixture](internal/queryfixture/postgres.go). Compile-failure cases retain outer ownership and nullable value types; real gopls inspects the correlated fields and methods.

The [relationship-filter acceptance](relationshipfilters/filters_postgres_test.go) covers generated `WhereHas`/`WhereDoesntHave`, parent-owned existence predicates, direct/inverse and nested self-relations, natural/nullable pivot keys, duplicate links, nested target/pivot filters, query counts, reusable eager descriptors, scoped updates and cancellation. Compiler assertions retain relationship ownership and model-specific lookup keys; gopls inspects generated methods and concrete slice results.

The [database example](database_example_test.go) compile-checks public executor/transaction contracts and a parameterized raw query. The separate [PostgreSQL acceptance test](postgres_test.go) executes public application assembly, typed pool resolution, bound SQL and a transaction with after-commit work against the isolated test database.

The [CTE acceptance](ctes/ctes_postgres_test.go) covers shared/dependent definitions, materialization choices, grouped projections, outer nullability, enum/decimal codecs, explicit correlation, relationship filters, direct/through/aggregate loading and scoped writes. Invalid definitions fail before executor access. Compile-failure fixtures preserve record ownership, IDs and nullable values; real gopls inspects CTE methods and fields in this module.

The [set-operation acceptance](setqueries/sets_postgres_test.go) covers union/intersection/difference, duplicate/NULL multiplicity, input and result windows, complete models, reports from different input scopes, exact aggregates, CTEs, joins, correlations, typed membership/scalars and scoped writes. It also checks canceled streams and all-or-error decoding. Negative compiler fixtures preserve record/value ownership, scope and nullability; gopls inspects fluent operations and combined output fields.

The [whole-record selection acceptance](recordqueries/records_postgres_test.go) selects complete models and reports from preserved joined scopes. It checks duplicate and unmatched rows, optional/required first results, typed slices, result windows, shared CTE/set composition, original codecs, stream cleanup and all-or-error decoding. Negative compiler fixtures reject wrong scopes, nullable sides, wrong result types and model writes on read-only selections.

The [recursive CTE acceptance](recursivequeries/recursive_postgres_test.go) exercises model and declared-report hierarchies, generated natural keys, empty/multiple anchors, duplicate paths, cycles, step predicates, shared CTE/set/membership composition, stream cleanup and a real cancellation deadline. Compiler fixtures preserve working-table ownership and result/key types; gopls inspects recursive constructors and generated working-table fields.

The [distinct acceptance](distinctqueries/distinct_postgres_test.go) exercises complete joined-model deduplication, nullable/enum values, grouped aggregates, selected scalar ordering, result-window counts and one ordered record per typed key combination. CTEs, sets, membership and correlated queries retain the same contracts. Invalid key/predicate owners, scalar types and model writes through a distinct read fail Go compilation.

The [window declarations](windowqueries/reports.go) and [window acceptance](windowqueries/windows_postgres_test.go) cover rankings, ties, distributions, buckets, row/peer frames, exclusions, nullable navigation, typed fallbacks, exact summaries and typed outer filtering. Windows compose with CTEs, sets, distinct reads, correlations and recursive steps. Compile-failure fixtures preserve input/output ownership and nullability, and gopls inspects scope-inferred windows and rank functions.

The [migration declarations](migrations/migrations.go) and [migration test](migrations_test.go) use typed origins/IDs, immutable SQL definitions, prerequisites, and history inspection. Tests inspect metadata without executing SQL. A negative compiler fixture rejects a migration ID used as an origin.

The [database command example and test](database_command_test.go) consume framework parsing/help and explicit command resources. Runtime command behavior is covered by driver protocol fixtures; scaffold tests compile separately created consumer modules.

The [calculation acceptance](scalarqueries/scalars_postgres_test.go) covers numeric/text operations, narrow and nullable codecs, exact conversions, escaped patterns, Unicode text, variadic NULL behavior, computed model ordering and grouped/window results. [Composition tests](scalarqueries/composition_postgres_test.go) cover declared projections, derived aggregation, CTE discovery, explicit correlations and computed target/pivot orders. Compile-failure fixtures retain scope, numeric/text types, nullable values and row/selected phase boundaries; real gopls inspects their consumer signatures.

The [comparison fixtures](comparisonqueries) cover typed row/selected comparisons, NULL-aware predicates, computed inner/outer join conditions, PostgreSQL FULL JOIN planner limits, Cartesian source windows, inherited nullability and transaction-scoped reads. Scalar-row tests retain inner aggregate phases, cardinality failures, CTE discovery and correlated relation alias qualification. Compiler fixtures and real gopls probes preserve value, scope and phase contracts.

The [lateral fixtures](lateralqueries) exercise per-parent model records, generated correlated reports, aggregate/window phases, optional ON conditions, empty results, chained nullability, CTE/set composition, pagination and explicit transaction locks. Compiler fixtures preserve outer ownership and forbid standalone correlated execution.

Normal tests skip real PostgreSQL acceptance when no test URL is supplied. Root `make test-postgres` requires the database and runs this module with race detection; see the [configuration instructions](../../../docs/guides/contributing.md#postgresql-acceptance). These fixtures are **not Foundry-Go-Starter**. Further feature modules arrive through the [roadmap](../../../blueprint/00-master-architecture-and-parity.md).

The [soft-delete models](softqueries/models.go), [lifecycle acceptance](softqueries/lifecycle_postgres_test.go) and [visibility acceptance](softqueries/visibility_postgres_test.go) cover automatic deletion timestamps, typed restore/force-delete hooks, operation-aware changes, rollback, and independent target/pivot scopes across eager loading, aggregates and advanced query sources.

The [tenant models](tenantqueries/models.go) and their [PostgreSQL acceptance](tenantqueries/tenant_postgres_test.go) cover a context tenant scope and a static published scope across reads, pagination, chunking, `WhereHas` and eager loading; set-based `UpdateAll`, `Increment`/`Decrement`, `DeleteAll` and `ForceDeleteAll`; lookup defaults from filters and scopes; and `Sync`, `SyncWithoutDetaching`, `Toggle`, `AttachMany`, `DetachMany`, `DetachAll` and `UpdateExistingPivot` change sets; [concurrent pivot synchronization](tenantqueries/pivot_concurrency_postgres_test.go) serialized per source; an [upsert](tenantqueries/upsert_scope_postgres_test.go) whose `DO UPDATE` stays within the scopes; and [bounded pruning](tenantqueries/prune_postgres_test.go) in mass and lifecycle modes through the `prune run` command. See the [global-scope guide](../../../docs/guides/model-global-scopes.md).

The [pivot-hook models](pivothooks/models.go) and their [PostgreSQL acceptance](pivothooks/pivot_hooks_postgres_test.go) run `Sync`, `SyncWithoutDetaching`, `Toggle`, `AttachMany`, `DetachMany`, `DetachAll` and `UpdateExistingPivot` through a pivot with local hooks and a registered provider observer: each changed link runs its lifecycle exactly once with its before/after state, unchanged links run none, returned changes match the stored links, and a hook failure at any step rolls back the whole operation without after-commit work. Its [`CreateOrFirst` acceptance](pivothooks/create_or_first_postgres_test.go) resolves only the model insert's own unique conflict, returning hook, hook-write and trigger violations.

The [office models](officequeries/models.go) and their [PostgreSQL acceptance](officequeries/office_postgres_test.go) cover `HasManyThrough`/`HasOneThrough` loading, intermediate soft deletion and `WhereHas`, `LatestOfMany`/`OfMany` choices with filters (including `HasOneThrough(...).OfMany` per parent), polymorphic relationships, and `RelatedValue` ordering, filtering and `WithValue` pairs. Its [desk acceptance](officequeries/desk_scope_postgres_test.go) covers a global scope declared with a relation-existence predicate.

The [relation-write models](linkqueries/models.go) and their lifecycle, key and concurrency acceptance cover derived pivot keys, ordinary hooks/mutators/timestamps, bounded atomic soft/force deletion, stale/nullable/natural keys, endpoint locking, unique conflicts and rollback. See the [relation-write guide](../../../docs/guides/model-relation-writes.md) and master roadmap for current verification status.


The [event consumer](eventqueries/events.go) connects generated model observers to typed event listeners through production application assembly. Its PostgreSQL test verifies commit timing, rollback suppression, concrete model IDs, attribution and committed data after listener failure. See the [event guide](../../../docs/guides/events.md).

The [durable event consumer](eventqueries/durable.go) enqueues a concrete event from a generated model observer. Its [PostgreSQL tests](eventqueries/durable_postgres_test.go) verify model/message commit and rollback, failure after enqueue, missing storage, and a generated model field containing `outbox.ID[RecordCreated]`. See the [outbox guide](../../../docs/guides/outbox.md).

The [authentication consumer](authenticating/accounts.go) reuses generated model
references and typed queries, shares one provider between guards, checks concrete
order policies and receives a concrete user in a guarded HTTP handler. Its test
verifies one hydration per request. Credential adapters in this fixture are test
inputs; session/token persistence belongs to later milestone 10 slices.

The [session consumer](authenticating/sessions.go) borrows an existing PostgreSQL
pool and typed model provider. It demonstrates verified issuance, model-owned
metadata, listing and targeted revocation without application SQL. Its integration
test uses an isolated retained schema. Five additional compiler cases reject wrong
models, references, session IDs, metadata and secret types; three gopls probes
inspect session methods in this consumer workspace. Browser login and CSRF are
not implemented by this fixture.


Recovery models now persist a typed email revision. The `recovering` model's
ordinary lifecycle hooks initialize it, rotate it when email changes, clear email
verification and reject a late email change without updated state. Test sources
cover restoration, direct draft attempts, rollback and concurrent recovery.
The `authenticating` fixture now also registers typed model permissions and
captures guard-derived attribution without another provider lookup. These cases passed the complete milestone 10 gate.


The `authenticating/composed_routes.go` fixture combines concrete User guards,
permissions, signed typed Order routes, generated Order queries, resource policies
and native HTTP handlers. It preserves independent subject/resource types and
keeps raw payloads out of DTO metadata. Consumer and type/editor acceptance cases passed the milestone 10 gate.

`multifactor/security.go` composes typed security notices with the existing event
outbox and account retirement with ordinary generated deletion in one transaction.
Its rollback/rejection/after-commit tests passed the milestone 10 gate.


The `realtime` consumer now also declares bounded replay, typed relay/acceptance,
shared local/distributed registry assembly, a trusted HTTP/job publisher, generated
subject-reference revocation and guarded diagnostics. These additions passed
milestone 15 verification and consumer review.


Milestone 16 adds [mailing](mailing/welcome.go): typed template data, immediate
sending, a foundation mailer module, an ordinary email job declaration and shared
transactional enqueue, plus a generated recipient DTO using the shared address
contract. Its five compiler rejections, three editor probes and native module/DTO
tests passed milestone 16 acceptance.

Milestone 17 adds [notifying](notifying/orders.go): generated input/inbox DTOs,
typed model recipient declarations, database/email/private realtime channel
bindings, captured transaction-aware jobs, and an authenticated inbox accessor.
Its public PostgreSQL consumer, seven compiler rejections and three real-gopls
probes passed focused checks and the full native milestone acceptance gate.


Milestone 18 adds [profiles](profiles/profile.go): generated profile/DTO declarations,
image and localized collection policy, typed metadata/settings/translations,
explicit country seeding, and generated owner lifecycle cleanup. Its public
PostgreSQL and race checks, 15 compiler rejections, seven actual-gopls probes and
the full native milestone gate passed. Parent rollback preserves extension rows
and files; committed force deletion schedules conditional object cleanup.

The [localization fixture](localization/messages.go) declares typed message
arguments and enum labels and composes validation, permission and HTTP locale
APIs. Milestone 20 generation, public consumer/race, compiler/editor checks and
the full native acceptance gate passed. Its [locale preference test](localization/locale_preference_test.go)
resolves a subject's locale from the request, a stored preference and
`Accept-Language` through `i18n.LocaleResolver`.

The [outgoing HTTP fixture](outgoing/client.go) wraps one named client, generated
request/response DTOs, callback-scoped streaming and the bounded fake transport.
Its milestone 20 generation, public consumer/race, compiler/editor checks and
the full native acceptance gate passed.
## Client contract acceptance

`clientcontracts` assembles real HTTP, authenticated/presence WebSocket, rendered
notification, reporting, localization and permission descriptors into the shared
manifest. It exports OpenAPI and a dependency-free TypeScript module, with strict
positive/negative type cases and real network interoperability. This milestone
21 generation, full native verification, strict TypeScript, real HTTP/WebSocket,
compiler/editor, publication/recovery and relevant race checks passed. See the
[client contract guide](../../../docs/guides/client-contracts.md) for native Node
and pinned compiler configuration. The root compiler rejection catalogue and
actual-gopls scenarios also include the public manifest/export APIs.

The [authenticated pagination consumer](httppagination/authenticated.go) names
concrete actor adapters for all three page modes. Compiler cases reject mismatched
actors, scopes, permissions and authorization callbacks. The existing
[client fixture](clientcontracts/contracts.go) registers real guarded pages and
checks manifest/OpenAPI security, strict TypeScript and authenticated HTTP calls.
