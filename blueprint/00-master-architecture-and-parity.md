# 00 — Master architecture and parity

## Purpose and scope

Build Foundry's capabilities in idiomatic Go: **Fat Framework, Slim Project**. Foundry owns runtime orchestration, infrastructure, typed persistence, and transport contracts. Applications own domain models, business behavior, DTOs, and registration.

Only `Foundry-Go` is being built. User direction on 2026-09-18 makes its source, public contracts and tests the authority for new work; sibling repositories are no longer required references. Historical mappings below remain delivery history. Business applications and starter products remain separate projects.

This document is the single source of truth for milestone status. Subsystem blueprints own their detailed contracts.

## SPA fallbacks with application.New (starter B09) — 2026-09-30

Status: **accepted** for the framework. `application.Builder.SPA` and
`http.RegisterSPA` declare SPA fallbacks on routers built by the application or
from contributions, checked by Build before assets open. SPAs compose with asset
mounts by prefix, so portals coexist with a root public mount, hashed-bundle
mounts and API routes, and a portal at `/` can sit beside a root mount. The
[asset guide](../docs/guides/http-assets.md#spa-fallbacks-with-applicationnew)
documents use; the [portal consumer](../tests/fixtures/consumer/spaportals/portals.go)
covers the starter's requirements. `make verify`, a real-gopls probe and HTTP,
application and consumer races passed
([evidence](../docs/evidence/spa-application-20260930.json)). The starter bumps
its pin and switches its portal routes after publication.

## Form controller, optional adapters and startup diagnostics — 2026-09-30

Status: **accepted** for the framework follow-up and starter migration candidate.
The [form guide](../docs/guides/client-forms.md) records the shared controller,
explicit parsing, bounded async ownership and optional React/Vue subscriptions.
Database startup diagnostics preserve existing retry/timeout behavior. Final
`make verify`, real HTTP/TypeScript consumers, React/Vue rendering/cleanup/SSR,
React hydration and relevant database races passed. The
[starter handoff](../docs/guides/forms-starter-handoff-20260930.md) includes a patch
verified through the Go export command, installed SDK consumers and both real
HTTPS profiles using a private overlay/local framework replacement. The
[evidence](../docs/evidence/forms-startup-20260930.json) records final source hashes
and review corrections. Publication, the actual starter upgrade and its independent
platform release checks remain separate; B08 is open.

Re-audited on 2026-09-30: edits no longer abort a sent submission, whose outcome
is reported (`changed`, or `canceled` with `not_sent`/`unknown`); debouncing task
runs release capacity; parsing is exact; the Vue adapter leaves no SSR
subscription. Full `make verify`, TypeScript/React/Vue, real-gopls and PostgreSQL
race gates passed on the corrected source
([re-audit evidence](../docs/evidence/client-reaudit-20260930.json)).

## Typed client descriptors and presentation — 2026-09-30

Status: **accepted** for this framework slice. The
[client descriptor guide](../docs/guides/client-descriptors.md) records typed
operation/schema lookup, exact field/owner identity and optional public semantic
hints carried through existing declarations and manifest version 6. Validation
continues through `validateRequest`; controllers and frontend adapters remain
outside this slice. Final `make verify`, real HTTP/WebSocket TypeScript and
JavaScript consumers, intended compiler failures, real-gopls probes, affected
races and bounded manifest fuzzing passed. The
[acceptance evidence](../docs/evidence/client-descriptors-20260930.json) records
source hashes, review corrections and exact check scopes. Starter adoption and
publication remain separate; B08 remains an open release-acceptance item.

Re-audited on 2026-09-30: route registration rejects password-hinted responses
and URL parameters; model-ID-keyed maps navigate by identity; `.at` applies codec
key rules; enum and scalar contradictions fail generation; repeated inputs are
checked per element; the kind set, rule IDs and key bound have single owners.
The same gates passed on the corrected source
([re-audit evidence](../docs/evidence/client-reaudit-20260930.json)).

## Second independent review — 2026-09-29

Status: **accepted** for the reviewed source. Eight read-only reviewers
re-examined every area changed by the improvement program and its stabilization;
the owning implementers fixed the confirmed defects with regression tests. The
[review record](../docs/guides/second-review-20260929.md) lists the principal
corrections and remaining boundaries. Native `make verify`, the TypeScript
client gate, real-gopls smoke and full `make race` passed with PostgreSQL and
Redis on this revision. The later
[acceptance follow-up](../docs/guides/second-review-acceptance-20260929.md) repeats
PostgreSQL races, security, fuzzing, packaged-consumer and macOS/Linux starter
checks. Most passed; an additional Linux queue-process timeout leaves release
acceptance incomplete.

## Improvement-program stabilization — 2026-09-29

Status: **accepted**. The [stabilization review](../docs/guides/stabilization-20260929.md)
records the must-have follow-up fixes for query cancellation, transaction retry,
authorization error inspection, atomic session resume, OAuth/OIDC validation,
publisher initialization and cache metrics. It also records the starter migration
requirements and the existing rollout rules.

Final `make verify`, affected runtime/consumer races, independent packaged
consumption and required-integration starter checks on macOS and Linux arm64
passed. Queue process stress passed ten repetitions per platform. The earlier
full race gate preceded the final query cleanup-hook correction; final evidence
includes the reproducing negative control, 4,000 fixed cancellation cases and
the affected race reruns. The [acceptance record](../docs/evidence/stabilization-20260929.json)
retains source hashes, commands, failures and corrections, security review and
deployment-test boundaries. Runtime and test inputs matched that review's final gate.

No required framework finding remains in this follow-up. The real starter was
unchanged; a tested migration patch is retained for adoption after the user
publishes and selects a reviewed revision. Deferred parity and milestone 25
remain unchanged. Private candidate acceptance does not publish a release or
certify production infrastructure.

## Framework improvement program — 2026-09-29

Status: **accepted** for the reviewed source. A framework-wide audit against
Laravel-style production expectations led to fixes for failure diagnostics,
overload handling, bounded-store pruning, transient-failure recovery and hot-path
round trips, plus parity work across HTTP contracts, authentication, the typed
ORM, background work, storage and operations. The [program record](../docs/guides/improvement-program-20260929.md)
lists delivered and deliberately deferred work; the changelog details each area
and the compatibility policy records the new rollout rules. Native `make verify`,
the TypeScript client gate, real-gopls smoke and full `make race` passed with
PostgreSQL and Redis. `make test-postgres`, fuzzing, security scanning and private
package consumption were not rerun for this program.

## Authenticated pagination adapter — 2026-09-28

Status: **accepted** for the framework's B05 adapter.
[`pagination.Authenticated`](../docs/guides/authenticated-pagination-20260928.md)
binds concrete actors to numbered, simple and cursor pages through existing HTTP
authentication and shared page completion. The final native `make verify`,
affected races, six new compiler rejection cases, two real-editor scenarios and
actual generated TypeScript HTTP checks passed. The evidence record in the guide
retains source hashes and the corrected editor probe. Starter route registration
and application-level list acceptance remain the starter's responsibility.

## Final team readiness review — 2026-09-27

Status: **accepted**. The [final review](../docs/guides/final-readiness-20260927.md)
reconciles all 66 current module families and fixes browser plural precision and
an omitted localization dependency in private configured consumer packages.
Named database validation now has concurrent PostgreSQL/cancellation coverage;
prepared message recipes also have round-trip fuzz coverage.

The final native `make verify` passed in 521.47 seconds with required PostgreSQL,
Redis, TypeScript and real gopls. Full framework/consumer races, final focused
client/catalog/release-tool races, five bounded fuzz campaigns, security review
and fresh independent packages passed. All 3,710 code inputs match verification.
The [evidence record](../docs/evidence/final-readiness-20260927.json) retains commands,
hashes, package identities and limits. No required runtime/package finding remains;
publication is an operator action and milestone 25 remains deferred.

## Validation expansion — 2026-09-26

Status: **accepted**. The [expanded validation contract](../docs/guides/validation-expanded.md)
covers common Laravel-inspired rules, redacted password inputs, bounded concurrent
I/O checks and typed batched model existence. The full native `make verify` passed
in 788.42 seconds with required PostgreSQL, Redis, gopls and TypeScript, including
compiler negatives, real HTTP/client behavior and generation freshness. Relevant
framework and consumer races passed; final review found no unresolved findings.
All 3,766 frozen source inputs matched that verification. See the
[acceptance evidence](../docs/evidence/validation-expanded-20260926.json).

## Request-aware validation messages — 2026-09-26

Status: **accepted**. [Shared message recipes](../docs/guides/validation-messages.md)
provide built-in English/plural defaults, typed custom overrides, translated
field/comparison labels and automatic locale-enabled HTTP error presentation.
Decoding retains 400, validation retains 422, and codes/paths/order stay stable.
Generated clients reuse the recipes and preserve explicit server-only skips.

Final native `make verify` passed in 661.68 seconds with required
PostgreSQL, Redis, TypeScript and real gopls, plus compiler negatives and current
generation. Framework and independent consumer races passed. The source review
fixed bounded browser substitution and explicit unbound argument preservation.
All 3,782 frozen source inputs matched final verification.
See [acceptance evidence](../docs/evidence/validation-messages-20260926.json).

## Agreed architecture decisions

- Latest stable Go, initially 1.27.1, verified against [official downloads](https://go.dev/dl/) on 2026-09-11. The delivered `go.mod` owns the actual toolchain requirement; do not duplicate it in scripts or CI.
- One Go framework module with public feature packages, private implementation under `internal`, and development commands under `cmd`. Nested modules are permitted for independent consumer/plugin tests.
- Foundry-owned model generation and one shared query AST; PostgreSQL is the only database adapter currently in scope.
- Typed model APIs are the normal application boundary: CRUD, filters, relations, joins and advanced queries must not require raw SQL when Foundry can express the operation. Missing typed coverage is framework work. Parameterized SQL remains an explicit escape hatch for unsupported database capabilities; it does not carry the model API's compile-time guarantees.
- Model-first authentication. Authentication strategies and providers are separate; one model can use multiple guards.
- Local, AWS S3, and Cloudflare R2 are required storage targets.
- WebSocket feature parity includes bounded replay, not durable reconnect recovery or Pusher/Echo protocol compatibility.
- Go DTOs and transport registrations own generated TypeScript and OpenAPI contracts. Browser models are not inferred from persistence models.
- Rust source/wire compatibility is not a release promise for the new Go framework. Preserve useful behaviors; document redesigned semantics and version Go transport contracts.
- Explicit registration and constructor injection; no process-global application singleton, magic runtime discovery, or exported map-based service locator.
- Needed dependencies are preauthorized by the user. Reuse existing libraries and do not add a different repository for an already-covered function; keep fixture module requirements aligned.

## Clarified consumer direction — 2026-09-17

Target Laravel-like developer convenience with fully typed Go APIs, IDE completion
and measured performance. Foundry owns library selection and ordinary infrastructure
assembly; consumers supply typed configuration and domain behavior. Configurable
services support named instances and one automatically selected default, including
multiple PostgreSQL connections and multiple storage disks. Preserve model/payload
types, explicit resource ownership and application isolation.

The [developer experience audit](../docs/framework-experience-audit.md) records the
current gaps, default-selection contract and proposed implementation batches.
The [consumer startup continuation](consumer-startup/README.md) is complete. Further
API experience work follows the [typed API continuation](typed-api/README.md).
Acceptance of milestones
01–24 below covers their original delivered contracts, not the additional
configuration-driven assembly described by this clarified objective.

## Consumer startup delivery

This table owns continuation status. Subsystem contracts live under
`blueprint/consumer-startup/`; the original foundation milestone numbering remains unchanged.

| ID | Contract | Status |
| --- | --- | --- |
| C01 | [Typed configuration and generation](consumer-startup/01-typed-configuration.md) | Complete — native full gate, races, independent consumer, compiler rejection and real gopls passed |
| C02 | [Named services and defaults](consumer-startup/02-named-services.md) | Complete — native full gate, races, persistent adapters, independent consumer and real gopls passed |
| C03 | [Application and HTTP assembly](consumer-startup/03-application-assembly.md) | Complete — final native full gate, races, executable consumer, parallel isolation, real gopls and build-cost evidence passed |
| C04 | [Supporting services](consumer-startup/04-supporting-services.md) | Complete — final native full gate, targeted races, persistent actors/features, named routing and real gopls passed |
| C05 | [Acceptance and final audit](consumer-startup/05-acceptance.md) | Complete — full change audit/fixes, final native gate, independent packages, build/editor/runtime measurements and security review passed |

C01 evidence: [native acceptance record](../docs/evidence/consumer-startup-c01.json).
C02 evidence: [native acceptance record](../docs/evidence/consumer-startup-c02.json).
C03 evidence: [native acceptance record](../docs/evidence/consumer-startup-c03.json), [build/runtime cost](../docs/evidence/consumer-startup-c03-cost.json).
C04 evidence: [native acceptance record](../docs/evidence/consumer-startup-c04.json).
C05 evidence: [final acceptance](../docs/evidence/consumer-startup-c05.json), [native measurements/security](../docs/evidence/consumer-startup-c05-native.json), [complete change audit](../docs/guides/consumer-startup-audit.md).
All five consumer-startup milestones are accepted; their individual evidence
records retain the historical gate timings. The additional
[performance and security review](../docs/guides/performance-security-review.md)
is complete: three confirmed fixes passed the full native gate, framework/consumer
races, 52 fuzz targets, repeated runtime measurements, independent packaged
build/editor/security checks and the completion audit.

## Module review — 2026-09-23

The [module review](../docs/guides/module-review-20260923.md) is complete across
all 65 current families and supporting tools. Eighteen findings are fixed while
preserving fully typed, Laravel-inspired Go contracts and shared ownership.
The post-verification audit corrections, final native gate, affected races,
consumer/compiler/editor/client acceptance, fuzzing, independent packages and
security review passed. The [acceptance record](../docs/evidence/module-review-20260923.json)
retains source fingerprints, coverage, actual results and measurement limits.
Milestone numbering and deferred M25 scope remain unchanged.

## Security hardening — 2026-09-24

The [security continuation](security-hardening/README.md) implements all five items from
[the new risk review](../docs/guides/security-gap-review-20260923.md). Status:
**accepted** — S01 and G01–G04 are implemented. The final native gate, required
backend races, consumer/compiler/editor/client checks, protocol fuzzing and security
scans passed. The re-audit corrections are verified; see the
[acceptance report](../docs/guides/security-hardening-20260924.md) and
[evidence](../docs/evidence/security-hardening-20260924.json). Hosted CI execution
remains untested; the workflow was validated locally and no Git changes were pushed.

## Team adoption readiness — 2026-09-25

The [team dependency audit](../docs/guides/team-readiness-20260925.md) is complete.
Current-source comparison, final native verification, required backend/editor/client
checks, fresh consumer races, independent packages, security review, empty-project
installation and Linux cross-compilation passed. The [adoption guide](../docs/guides/team-adoption.md)
adds project-pinned tooling, package initialization, generation and application CI.
The framework is ready for the team's own boilerplate on its supported stack;
remote distribution still requires an operator-selected available version.
No runtime API or persisted format changed, and deferred milestone 25 is unchanged.
See [current evidence](../docs/evidence/team-readiness-20260925.json) for scope and limits.

## Automatic file logging — 2026-09-25

File log sinks now provide [automatic rotation and retention](../docs/guides/logging.md):
daily rollover (UTC by default)/20 MiB and 14-archive/14-day defaults, typed configuration,
exclusive file ownership and bounded cleanup. The application default remains
INFO JSON on stderr. The focused source review, logging/application/consumer
races, final native full gate, generated configuration and real editor checks
passed. Linux amd64/arm64 logging test binaries compiled; Linux execution was not
performed. See the [acceptance evidence](../docs/evidence/logging-rotation-20260925.json).

## Application timezone — 2026-09-26

[Application timezones](../docs/guides/application-timezone.md) now share one typed
setting across `s.Time()` calendar helpers, `s.Calendar()` schedules, owned log
timestamps/daily rollover and report export presentation. UTC is the default;
explicit overrides remain available. Clock and timezone state are application-owned,
with immutable temporal values and no process-global timezone mutation. Database
instants/wire values remain UTC, and TTLs/timeouts/retention ages remain elapsed
durations. Named zones have a standard-library embedded IANA fallback.

The source/consumer review and focused root/consumer races passed, including real
PostgreSQL export formatting, concurrent application isolation, strict DST parsing,
23/25-hour calendar days and local-midnight file rollover/restart. The final native
`make verify` gate passed in 796.38 seconds with required PostgreSQL, Redis,
TypeScript and actual gopls. All 3,747 frozen source fingerprints matched. Generated
configuration, two new compiler rejection cases and three new editor scenarios
are included. See [acceptance evidence](../docs/evidence/application-timezone-20260926.json).

## Typed API delivery

The 2026-09-18 [Go source audit](../docs/typed-api-gap-audit.md) confirms specific
remaining gaps while preserving existing typed HTTP, PATCH, pagination and outbox.
The [new series](typed-api/README.md) contains seven milestones. This table alone
owns their status; authoring these contracts does not imply runtime implementation.

| ID | Contract | Prerequisites | Status |
| --- | --- | --- | --- |
| T01 | [Generic DTO schema generation](typed-api/01-generic-dto-generation.md) | Accepted foundation and C01–C05 | Complete — native gate, typed consumer/client/compiler/editor acceptance and cost evidence passed |
| T02 | [Discriminated unions](typed-api/02-discriminated-unions.md) | T01 | Complete — native gate, typed union/client/compiler/editor acceptance, fuzzing and cost evidence passed |
| T03 | [Forms and request lifecycle](typed-api/03-forms-and-request-lifecycle.md) | T01–T02 | Complete — native gate, forms/lifecycle/client/compiler/editor acceptance, races, fuzzing and hook-cost evidence passed |
| T04 | [Nested scoped model binding](typed-api/04-scoped-model-binding.md) | T01–T03 | Complete — native gate, nested PostgreSQL binding/policy/compiler/editor acceptance and races passed |
| T05 | [Isolated HTTP database tests](typed-api/05-isolated-http-tests.md) | T01–T04 | Complete — native gate, isolated PostgreSQL HTTP/commit/outbox/race/compiler/editor acceptance and pool-cost evidence passed |
| T06 | [Inbound idempotent operations](typed-api/06-inbound-idempotency.md) | T01–T05 | Complete — native gate, PostgreSQL replay/crash/race/client/compiler/editor/fuzz acceptance and cost evidence passed |
| T07 | [Acceptance and final re-audit](typed-api/07-acceptance-and-audit.md) | T01–T06 accepted | Complete — integrated workflow, complete audit/fixes, final native gate, packaged consumers, compiler/editor/client/fuzz and cost evidence passed |

T01 acceptance (2026-09-18): the complete native `make verify` gate passed in
641.7 seconds after the source/docs batch and batched corrections. Focused runtime
and consumer races, three compiler-rejection cases, real gopls probes, strict
TypeScript/HTTP round trips and deterministic generation passed. Generic and
concrete HTTP envelopes both measured 247 allocations in the controlled fixture;
generic descriptor assembly costs more and stays outside request handling. See
the [guide](../docs/guides/generic-dtos.md) and
[commands, samples and source hashes](../docs/evidence/typed-api-t01.json).
This records T01 acceptance; later milestone evidence follows separately.


T02 acceptance (2026-09-18): native `make verify` passed in 471.4
seconds after batched corrections, a cold-cache restart and the benchmark metrics
fix. Focused runtime and consumer races, all variant/client round trips, four compiler-rejection cases,
real gopls, deterministic generation and bounded fuzzing passed. See the
[union guide](../docs/guides/tagged-unions.md) and
[commands, source hashes and measured costs](../docs/evidence/typed-api-t02.json).
Subsequent milestone acceptance is recorded below.


T03 acceptance (2026-09-18): native `make verify` passed in 588.7 seconds.
Generated forms, shared rules across four input sources, preparation/prohibition
ordering, required/optional and distinct guard actors, owned cancellation/upload
cleanup, strict TypeScript HTTP round trips and compiler/editor acceptance passed.
Bounded fuzzing completed 18,159 executions. The local endpoint benchmark median
was 5.475 microseconds without hooks and 6.473 microseconds with preparation and
authorization (75 versus 93 allocations, including httptest setup). See the
[guide](../docs/guides/forms-request-lifecycle.md) and
[commands, samples and source hashes](../docs/evidence/typed-api-t03.json).
Subsequent milestone acceptance is recorded below.


T04 acceptance (2026-09-18): native `make verify` passed in 761.1 seconds.
Native PostgreSQL and race acceptance prove three typed binding levels, identical
child slugs under different parents, duplicate-key rejection, soft-delete and
explicit relation filters, retrieval/eager-load behavior and request/resource
policy order. Six compiler-negative cases and real gopls probes passed. A normal
three-level route makes three resource SELECTs; request-policy denial makes zero.
See the [guide](../docs/guides/scoped-model-binding.md) and
[commands, source hashes and query counts](../docs/evidence/typed-api-t04.json).
Subsequent milestone acceptance is recorded below.


T05 acceptance (2026-09-18): native `make verify` passed in 691.7 seconds.
Native integration/races prove independent complete HTTP apps with identical keys,
real commit/rollback/after-commit/outbox behavior, named/default/read pools, schema
reset/reconnect, migration grouping, startup cleanup and retained data. Compiler
and real gopls checks passed. Native trivial-query checkout medians were
25487 ns unscoped and 75070 ns scoped;
the explicit scoped adapter adds a validation/reset round trip. See the
[guide](../docs/guides/isolated-http-tests.md) and
[commands, hashes and setup/pool costs](../docs/evidence/typed-api-t05.json).
Subsequent milestone acceptance is recorded below.


T06 acceptance (2026-09-18): native `make verify` passed in 177.4 seconds.
Typed operations atomically commit claim, business effects, existing outbox and
bounded response. Independent application races, commit-acknowledgment failures,
actual pre/post-commit process termination, failed socket writes, current policy,
verified webhooks, strict TypeScript HTTP, compiler/gopls and bounded fuzzing passed.
Local runner medians were 1.007 ms new and 0.581 ms replay; a contended two-request
pair took 3.021 ms including its intentional delay. See the
[guide](../docs/guides/idempotent-operations.md) and
[commands, hashes, samples and measurement limits](../docs/evidence/typed-api-t06.json).
Subsequent milestone acceptance is recorded below.


T07 acceptance (2026-09-19): the initial native gate passed in 511.5 seconds;
a complete changed-code review confirmed five fixes/improvements. The final native
`make verify` gate passed in 633.4 seconds. PostgreSQL/races, persisted PATCH
states, generic/tagged/paginated clients, all four input sources, two-app duplicate
submissions, receiving receipts and fresh-process replay passed. Independent
packaged consumers have no module replacements; strict TypeScript, compiler/editor,
eight fuzz targets and controlled build/generation/runtime/editor checks passed.
The complete local HTTP workflow measured median 1.953 ms new and 0.964 ms replay;
these include more work than the T06 runner benchmark and are not an overhead ratio.
See the [workflow guide](../docs/guides/typed-api-workflow.md),
[complete audit and fixes](../docs/guides/typed-api-audit.md) and
[commands, samples and source hashes](../docs/evidence/typed-api-t07.json).
All seven typed API milestones and the requested final re-audit are complete.
Existing foundation and consumer-startup milestones retain their accepted status.

## Model extension slot delivery

User direction on 2026-09-29 adds model-level declaration of translated text,
attachments and typed schemaless values over milestone 18's extension stores,
similar to Laravel model traits. Slots are typed model fields with one
`DefineExtensions` policy method; generation binds owners, registrations,
descriptors and deletion cleanup. The user chose explicit bind-once runtime
injection. The [series](model-extension-slots/README.md) owns design. This table
alone owns status; authoring these contracts does not imply runtime implementation.

| ID | Contract | Prerequisites | Status |
| --- | --- | --- | --- |
| E01 | [Slot declarations and generation](model-extension-slots/01-slot-declarations-and-generation.md) | Accepted milestones 06, 07, 18 and 20 | Complete — native `make verify` (680 s), PostgreSQL races for affected framework and consumer packages, three compiler-rejection cases and four real-gopls scenarios passed |
| E02 | [Slot loading and reads](model-extension-slots/02-slot-loading-and-reads.md) | E01 | Complete — native `make verify` (534 s), PostgreSQL races for slot loading and affected stores, constant statement counts (23 for 1 and 41 parents), one compiler-rejection case and two new real-gopls scenarios passed |
| E03 | [Slot writes and HTTP input](model-extension-slots/03-slot-writes-and-http-input.md) | E01–E02 | Complete — native `make verify` (670 s), PostgreSQL races for slot writes, real JSON/multipart endpoints and detection parity, three compiler-rejection cases and two new real-gopls scenarios passed; the TypeScript locale union was not delivered (compatibility review required) |
| E04 | [Consumer fixture, tooling and documentation](model-extension-slots/04-consumer-tooling-and-documentation.md) | E01–E03 | Complete — native `make verify` (354 s), PostgreSQL races for the articles consumer, extension inspection snapshot and undeclared-name commands passed; scaffold slot flags were not delivered |
| E05 | [Integrated acceptance and final re-audit](model-extension-slots/05-acceptance-and-audit.md) | E01–E04 accepted | Complete — native `make verify` (500 s) after the re-audit corrections, 445 real-gopls checks, the compile-fail catalog, statement and list-loading measurements, and PostgreSQL races for every root and consumer package except `internal/workscope`, whose race-mode timing test failed intermittently ([evidence](../docs/evidence/model-extension-slots-e05.json)); that test was then fixed and the full `make test-postgres` passed, with extension batch reads about 41% faster at 1000 parents ([follow-up](../docs/evidence/model-extension-slots-performance-20260930.json)) |

## Milestones and dependencies

Numbering is the default implementation order. A prerequisite is a gate, not permission to silently implement a later milestone. Later integrations are stated explicitly to avoid circular dependencies.

| ID | Blueprint | Prerequisites | Implementation status |
| --- | --- | --- | --- |
| 00 | Master architecture and parity | None | Blueprint authored |
| 01 | [Repository and toolchain](01-repository-and-toolchain.md) | 00 | Complete — verified and consumer boundary reviewed |
| 02 | [Foundation and application lifecycle](02-foundation-and-application-lifecycle.md) | 01 | Complete — core and approved TOML adapter verified; consumer experience reviewed |
| 03 | [Generation and language tooling](03-generation-and-language-tooling.md) | 02 | Complete — package graph, typed generation, recovery and real-gopls acceptance verified; consumer experience reviewed |
| 04 | [Database runtime and migrations](04-database-runtime-and-migrations.md) | 02, 03 | Complete — approved PostgreSQL adapter, runtime, migrations and tooling verified against real PostgreSQL; consumer experience reviewed |
| 05 | [Typed models and queries](05-typed-model-and-query-core.md) | 04 | Complete — typed codecs, generated CRUD, numbered/cursor pagination and transaction outcomes verified; consumer experience reviewed |
| 06 | [Relations and advanced queries](06-relations-and-advanced-queries.md) | 05 | Complete — typed relations, advanced queries, projections, pagination, upserts and transaction-preserving lock composition verified; consumer experience reviewed. Lifecycle/bulk-mutation integration remains in 07, with database tooling and operational parity tracked in 23/24 |
| 07 | [Model lifecycle, events, and audit](07-model-lifecycle-events-and-audit.md) | 05, 06 | Complete — typed hooks/observers, getters/mutators with automatic field notices, stored changes, soft deletion, events, transactional outbox and audit verified; consumer experience reviewed |
| 08 | [HTTP, validation, and responses](08-http-validation-and-responses.md) | 02, 03, 05, 07 | Complete — typed HTTP transport, validation, pagination/model binding, files/static/SPA, middleware and gzip/Brotli compression verified; consumer experience reviewed. Cross-feature integrations remain in their owning milestones. |
| 09 | [Redis, cache, and coordination](09-redis-cache-and-coordination.md) | 02 | Complete — typed cache, tags/namespace invalidation, local/distributed Remember, counters, leases, rate limiting, pub/sub, typed Redis hashes/sets and scoped commands/pipelines/scripts verified with native local services; source parity and consumer experience reviewed. |
| 10 | [Model-first authentication](10-model-first-authentication-and-authorization.md) | 07, 08, 09 | Complete — typed guards/permissions, sessions/tokens, passwords/lockout/recovery, MFA, attribution, HTTP composition, security events and retirement passed native acceptance; source parity and consumer experience reviewed. Later integrations retain their owning milestones. |
| 11 | [Storage reliability](11-storage-reliability.md) | 02, 08 | Complete — typed storage, local/AWS S3/R2 adapters, native and live provider checks, versioned cleanup and consumer experience verified |
| 12 | [Jobs and worker](12-jobs-and-worker-kernel.md) | 07, 09 | Complete — typed jobs, atomic Redis workflows, owned workers, shared outbox publication, native acceptance and consumer/parity review passed |
| 13 | [Scheduler](13-scheduler-kernel.md) | 09, 12 | Complete — calendar/interval schedules, bounded execution, shared Redis coordination, typed job targets and native acceptance/consumer review passed |
| 14 | [WebSocket channels and protocol](14-websocket-channels-and-protocol.md) | 03, 08, 10 | Complete — typed protocol/runtime, fresh auth and room ownership, safe local presence, native kernel and full acceptance/consumer review passed |
| 15 | [Distributed WebSocket behavior](15-websocket-distributed-behavior.md) | 09, 12, 14 | Complete — Redis fan-out/replay/presence, limits/revocation, diagnostics, native full gate and consumer review passed |
| 16 | [Email](16-email.md) | 11, 12 | Complete — native provider fixtures, typed jobs/outbox, lifecycle and consumer review passed; real-account smoke gap recorded |
| 17 | [Notifications](17-notifications.md) | 07, 10, 12, 15, 16 | Complete — native persistent channel/inbox, queue/private realtime and consumer review passed |
| 18 | [Imaging and model extensions](18-imaging-and-model-extensions.md) | 06, 07, 11, 12 | Complete — native imaging, durable attachments, typed model extensions, reference seeding and consumer review passed |
| 19 | [Datatables and reporting](19-datatables-and-reporting.md) | 06, 08, 10, 11 | Complete — typed reporting, scoped pages/counts, bounded CSV/XLSX, HTTP/jobs/storage composition and native consumer review passed |
| 20 | [Localization and supporting APIs](20-localization-and-supporting-apis.md) | 02, 03, 08, 09 | Complete — typed localization, shared labels, bounded outbound HTTP and supporting APIs passed native verification and consumer review |
| 21 | [Contracts and TypeScript](21-contracts-and-typescript-sdk.md) | 03, 08, 10, 15, 17, 19, 20 | Complete — versioned manifest, OpenAPI, exact typed HTTP/realtime SDK, publication/recovery and native consumer acceptance passed |
| 22 | [Plugin ecosystem](22-plugin-ecosystem.md) | 02 and the feature registrations above | Complete — typed plugins, direct contributions, independent modules, historical migrations and guarded distributions passed native verification and consumer review |
| 23 | [Developer tooling and testing](23-developer-tooling-and-testing.md) | 03 and delivered feature APIs | Complete — typed CLI, scaffolds, doctor/inspection, test helpers, query plans and batched tooling passed native verification and consumer review |
| 24 | [Production hardening and release](24-production-hardening-and-release.md) | 01–23 | Complete — native full gate, independent packaging, controlled measurements, security/license review and complete-framework audit/fixes passed |
| 25 | [Deferred extensions](25-deferred-extensions.md) | Separate future work | Deferred |

Foundation introduces the extension lifecycle and core test harness. Every feature contributes a registration interface, tests, and contract metadata as it is built. Milestones 21–24 assemble those capabilities; they do not postpone typing, testing, plugin compatibility, or observability until the end.

The evidence sections below record each delivery at that point in implementation. Later sections may complete work described as pending in an earlier section; use the status table and owning subsystem blueprint for current scope.

### Milestone 01 verification evidence

Verified on 2026-09-11 inside the `foundry-go` microVM, with the host workspace mounted at `/Users/weiloon/Projects/rust-go` and `go version go1.27.1 linux/arm64`.

- `make verify`: passed formatting, vet, repository checker unit tests, independent consumer vet/compilation, local Markdown links, blueprint numbering and fixture Go-version alignment.
- `make race`: passed for current tool packages and the consumer compilation check.
- At milestone 01 completion, consumer coverage was import-only. Runtime lifecycle and type-safety coverage are added in milestone 02.
- No third-party Go dependencies, database migrations, starter application, Git initialization, commits, pushes or merges were introduced.

The initial VM-helper attempt failed during Redis setup; a retry using the current standard kit succeeded. Go selected the required toolchain from `go.mod`. This environment issue does not leave an outstanding milestone 01 verification gap.

### Milestone 02 implementation evidence

Delivered core packages: root assembly, `foundation`, `config`, `fault`, `logging`, `secret`, `clock`, `model`, `value`, `temporal`, and `testkit`. Providers and typed service constructors are explicit. All five kernel kinds use one lifecycle; concrete feature kernels remain in later milestones. The [independent consumer](../tests/fixtures/consumer/README.md) exercises these public boundaries.

Configuration supports defaults, decoded file-value layers, environment, typed overrides, validation, safe provenance, and isolated mutable values. The approved `config/toml` adapter uses the parser pinned in `go.mod`, sharing declared key ownership with `Schema.Load`. Structured `config.JSON` keys use the same decoder for normalized TOML collections and environment input.

Verified on 2026-09-11 inside the existing `foundry-go` VM using `go1.27.1 linux/arm64`:

- `make verify`: passed formatting, vet, all root behavior tests, independent consumer tests, five negative-compilation fixtures, and repository documentation checks.
- `make race`: passed root and consumer tests, including concurrent lifecycle and clock behavior.
- Bounded UUID parser fuzzing: passed 350,938 executions in approximately three seconds with two workers.
- Bounded local date-time parser fuzzing: passed 294,685 executions in approximately three seconds with two workers. These are smoke checks, not exhaustive parser proofs.
- Foundation audit fixes cover structured provider/task/resource names, retained constructor-resolver ownership, cleanup panic/Goexit handling, safe secret/error formatting, nested credential log groups, explicit JSON-null rejection, UTC calendar-range validation, and documentation checks distinguishing inline generic Go code from links.

After explicit dependency approval on 2026-09-11, the TOML adapter passed scalar/collection/date decoding, exact int64 preservation, arrays of tables, namespace ownership, unknown/malformed input, bounded reads, secret-safe errors, and precedence tests. `make verify race agent-smoke` passed with the real gopls executable selected, including the independent consumer's TOML usage. Bounded TOML fuzzing passed 14,551 executions with two workers. The adapter review confirmed no second settings schema and no partial settings on failure.

The consumer review confirms explicit public imports, typed constructors/configuration, one shared lifecycle, and request-independent clock control. No starter or concrete network/database/provider runtime is implied. The final framework-wide audit requested by the active objective remains outstanding until all required implementation milestones are delivered.

### Milestone 03 implementation evidence

Delivered packages: `database/query` with a shared typed expression representation, `enum` with typed contract descriptors, `internal/generate` with Go declaration/type analysis and deterministic emission/recovery, `internal/agent` with a read-only LSP client, and `cmd/foundry` generation/agent commands. The [consumer models](../tests/fixtures/consumer/models/models.go) are handwritten. Generated APIs preserve model owners, field types, aliases, nullable mutation states, self-reference IDs, natural keys, and local/imported enum membership types. SQL execution remains milestone 05 work.

Verified on 2026-09-11 using the existing VM's Go 1.27.1:

- `make verify` and `make race` passed for root packages and the independent consumer. The verification gate now includes `make generate-check`.
- Twelve negative compilation fixtures cover service/configuration ownership/value errors, ID owners/conversions, generated query predicate/order owners, wrong field/ID/draft values, numeric text operations, and null clearing of non-nullable fields.
- Generator tests cover fresh output with business methods/variables referencing generated declarations, deterministic repeat generation without rewriting files, stale checks, full-overlay failure without replacement, obsolete owned-file removal, malformed/duplicate declarations, preserved aliases, default primary-key ownership, unowned/edited/orphaned generated output, unsafe manifest paths, and rollback on cancellation or injected rename failure.
- Audit improvements include shared SQL identifier validation, strict tag parsing, output permission preservation, publication-time input checks, and rejection of orphaned generated files that would otherwise be excluded from validation.

Further milestone 03 verification on the same toolchain:

- `make fmt generate verify race`: passed root and independent consumer gates; generated fixture output is current. The additions cover typed enum `Value`/`Scan` methods, exact contract definitions, imported enum operator restrictions, generated aliases/ignored fields, `iota` and inferred enum constants, and import-name collisions.
- Forced-termination tests passed for incomplete publication rollback, complete publication retention, automatic recovery, and active publisher exclusion. Additional cases cover creations/deletions, edited targets, changed permissions, symlinks, damaged backups, and a read-only `--check` during interruption.
- Generator tests also passed with their temporary workspaces under the actual host-mounted repository filesystem, exercising file sync, advisory locks and recovery on that mount.
- The LSP client passed protocol fixture tests for all three operations, unsaved buffers, Unicode coordinates, server-requested edit refusal, error handling, cancellation, and owned-process cleanup. A bounded framing-parser fuzz smoke test passed 224,960 executions in approximately three seconds with two workers.
- This slice's audit corrected stdout pipe ownership, cancellation error preservation, enum wire-type/range validation, generated import collisions, permission changes, safe restoration temporary files, and completed-staging cleanup behavior on mounted filesystems. These checks do not replace the final framework-wide audit after implementation.

Package-graph implementation and verification on 2026-09-11:

- Recursive generation shares complete checked packages in memory, validates dependents before publication, and uses one journal across the selected graph. The consumer's catalog/status packages demonstrate imported enum fields, foreign model-owned IDs, and handwritten methods returning generated types from another package.
- `make verify` and `make race` passed, with current generated output and thirteen required compiler-failure fixtures. The new negative fixture rejects text operators on an imported generated enum.
- Actual import-cycle fixtures, cross-package duplicate tables, nested-module exclusion, fresh generation, stale checks, dependent compilation failures, new package detection, and module/workspace file changes are covered.
- Forced-termination tests passed after partial and complete graph publication. Incomplete graphs restore all participants, including a package whose own manifest was already published. Child generation/recovery cannot bypass the ancestor journal, including after a nested module is added. Symlinked directories and escaping/unguarded journal paths are rejected.
- The generator suite passed again with temporary fixtures on the host-mounted workspace, covering graph journals, guards, sync and `os.Root` operations there. Rust Foundry remains unchanged.

After explicit approval on 2026-09-11, gopls v0.23.0 was installed into ignored host-mounted `bin/`, with its pin owned by [tools/gopls.version](../tools/gopls.version). It introduces no runtime module requirement. `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke` passed on Go 1.27.1. Real gopls completed generated setters and nullable clear operations, omitted invalid non-nullable clearing, returned model-owned ID hover signatures, and resolved definitions into generated consumer files without editing source. The [owning blueprint](03-generation-and-language-tooling.md) and guide describe rerunning the gate. Later implementation milestones and the final framework-wide audit remain outstanding.

### Milestone 04 verification evidence

The [database runtime guide](../docs/guides/database-runtime.md) documents the delivered connector-based pool, parameterized execution, streaming/typed hydration helpers, callback-scoped transactions, savepoints, error metadata, and after-commit outcomes. Pool closure drains both connection owners and after-commit callbacks. Transaction operations observe the begin context, escaped scopes reject reuse, and unfinished operations/rows prevent commit.

Verified on 2026-09-11 using Go 1.27.1 in the existing VM:

- `make fmt` and `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke` passed, including the independent consumer's database compilation example and all previous fixtures.
- Standard-library driver protocol tests cover acquisition exhaustion/deadlines, failed open cleanup, parameter separation, row/iteration/close errors, drain deadlines, callback failure/panic/Goexit, savepoint nesting and poisoning, commit ambiguity, rollback failure, and after-commit failure ordering.
- Runtime review fixes preserve transaction context during calls with independent contexts, serialize forced row cleanup, retain operation ownership through after-commit work, and avoid claiming confirmed rollback when automatic cleanup's result is unavailable. Foundation and database callbacks share the same private panic/Goexit isolation implementation; existing foundation tests remain green.
- The authorized broker provisioned the isolated `foundry_go_test` account/database. Its credentials remain in an ignored private `.env.test`, without overwriting an existing file or exposing values. Initial protocol tests preceded the real database acceptance described below.

Further milestone 04 implementation adds prepared pools and typed lifecycle modules, callback-scoped sessions sharing transaction execution, and disposal of uncertain connections. The [migration and seeding guide](../docs/guides/migrations-and-seeding.md) covers immutable registries, checksums, history inspection, the PostgreSQL advisory-lock runner, and transactional seeders. Providers, migrations, and seeders share private dependency ordering.

- `go test -race ./database/... ./foundation/...` and `make fixture-check generate-check` passed after these additions.
- Protocol fixtures cover failed/concurrent startup, application drain ownership, session reuse/disposal, classifier panic/Goexit, migration drift/history bounds, concurrent runners, lock timeout, partial commits, lost commit responses, failed lock cleanup, and seeder selection/rollback/after-commit behavior.
- The independent consumer declares and inspects migrations through public APIs. Fourteen required compiler-failure fixtures include rejecting a migration ID used as an origin. These metadata tests do not need database access; separate consumer acceptance executes PostgreSQL.

The full `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke` gate passed after the lifecycle, migration and seeder additions. Subsequent command/scaffold work adds `database/command` for migration/seeder parsing and reporting, and `foundry make migration/seeder` through the shared package compiler and publication/recovery path. Scaffold output is consumer-owned; explicit definition registration retains typed application assembly.

- Command protocol tests passed for help without services, read-only status, drift errors, typed seeder selection, uncertain-commit progress and output failure after confirmed commits. The independent consumer compiles the same public adapter.
- Scaffold race tests passed with independent consumer compilation, deterministic rendering, unfinished-work failures, name/file/symlink collisions, stale declarations, cancellation, no-overwrite publication, and creation-only recovery. Existing generator recovery tests remain green.
- Review tightened synchronous inherited cancellation and prevents migration plans from exceeding their own history read bound.

After the command/scaffold additions, the pgx v5.11.0 driver was explicitly approved and installed. The [PostgreSQL adapter guide](../docs/guides/postgresql.md) documents pure explicit configuration, application-owned pools, TLS modes, bounded protocol messages/caches and SQLSTATE classification. Review fixed upstream ambient TLS-file settings leaking into parsing, rejects empty explicit URL ports, and validates private test configuration without sourcing shell code or printing credentials.

`FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke test-postgres` passed on Go 1.27.1 in the existing VM after these changes. The required PostgreSQL gate runs both root and independent consumer modules with race detection and no cached test results.

- Real PostgreSQL tests verify selected database/user and UTC timezone, exact parameter binding, streaming ownership, acquisition exhaustion, cancellation, unavailable endpoints and protocol message bounds.
- Real transaction tests verify rollback, savepoints, after-commit ordering/reacquisition, read-only writes, constraint metadata, deferred commit rejection, serializable conflicts and deadlocks.
- Competing migration runners apply each migration once; status stays read-only, checksum drift blocks changes, a failed pending migration can be corrected and applied, and an explicitly repeatable seeder runs twice without resetting records.
- The [independent consumer](../tests/fixtures/consumer/postgres_test.go) registers the public PostgreSQL module, resolves its typed pool key, executes bound SQL and commits a transaction with after-commit work. Consumer review confirms framework-owned infrastructure and parsing, explicit resource ownership and typed registration, with consumer-owned declarations/scaffolds.
- Tests create unique schemas in only the isolated test database and retain them. No DROP/TRUNCATE/reset is run. Lost commit responses remain protocol fault tests; TLS configuration has unit coverage, while a deployed TLS server was not part of the local acceptance setup.

Milestone 04 is complete. Generated ORM execution begins in milestone 05. Later milestones and the final whole-framework verification and audit remain outstanding. Rust Foundry is unchanged, and no starter, commits, pushes or merges were created.

### Milestone 05 codec slice evidence

The first slice adds [typed database codecs](../docs/guides/database-codecs.md) and `decimal.Decimal`. Concrete codec types preserve named scalars, UUID owners, nullable values and temporal meaning. Failed column decodes leave destinations unchanged. Decimals use canonical finite text, exact bounded arithmetic and JSON strings; the generator emits typed ordered fields/drafts for them.

Generated enums now expose typed codec functions and delegate their SQL `Value`/`Scan` methods to that shared implementation. This reuses the generated membership rule and central scalar range checks. Review found and fixed missing export-data discovery for the codec import during fresh generation; package graph/fresh-checkout acceptance remains green.

Verified on 2026-09-11 with Go 1.27.1 in the existing VM:

- `make fmt generate` and `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke test-postgres` passed after codec and generator consolidation. Generated output is current and documentation links pass.
- Unit tests cover integer bounds, representation mismatches, non-finite floats, nullable states, destination preservation, buffer ownership and temporal precision/meaning. Exact decimal arithmetic passed a 10-second fuzz run against `math/big.Rat` (approximately 1.96 million executions).
- Real PostgreSQL tests round-trip high-precision decimals, UUIDs, named scalar types, all temporal codec families, explicit NULL and present zero. Stored overflow and unsupported numeric representations fail without replacing the destination. The independent consumer additionally round-trips a generated enum and rejects a malformed stored member.
- Eighteen consumer compiler-failure cases include the new codec owner/value mismatches, float-to-decimal mutations and text operators on decimal fields. The ledger fixture demonstrates concrete decimal field and draft types.

The codec slice was followed by the generated read implementation below. This is not milestone 05 completion: model writes, required/default mutation validation and higher-level pagination remain required. The mutation pipeline must be ready for milestone 07 lifecycle integration; partial projections belong to milestone 06.

### Milestone 05 generated read evidence

The [model query guide](../docs/guides/model-queries.md) documents generated query wrappers, concrete primary-key lookup, complete hydration, streaming and selected-window aggregates. `database/query` owns one parameterized PostgreSQL compiler over its private shared AST; model-specific generated code supplies field codecs and full-row decoders, with no per-model SQL implementation. Generation reuses one codec-selection path for enums, predicates and hydration.

Verified on 2026-09-11 using Go 1.27.1 in the existing VM:

- Fresh generation and current-output checks pass. Compiler tests verify quoted declarations, binding order, literal substring escaping, grouping, empty membership, copied arguments, concurrent derivation and depth/node/ordering/parameter bounds. PostgreSQL identifier length is checked before server-side truncation can change its meaning.
- Twenty required consumer compilation failures include wrong model IDs passed to `Find` after fluent derivation and incompatible natural-key types.
- Real PostgreSQL consumer tests hydrate complete user, country and ledger models inside a unique retained schema with transaction-local search-path isolation. They exercise nullable dates and self-reference IDs, exact decimals, filtered lookup, explicit missing results, window aggregates, empty windows, early stream exit and cancellation. A malformed stored enum rejects the entire collected result.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke test-postgres` passed. Real gopls completes generated/promoted query methods, exposes concrete model ID and Optional result types in hover, and resolves Find into generated consumer source without editing it.

Consumer review confirms that ordinary reads contain domain predicates and an explicit executor. Foundry owns binding, SQL, resource release and complete hydration. The write slice below follows these reads. Offset pagination helpers and stable cursor pagination remain required in this milestone. Later milestones and the final framework-wide audit remain outstanding.

### Milestone 05 generated write evidence

The [model write guide](../docs/guides/model-writes.md) documents generated create/update/delete, typed drafts/keys, required fields, database-owned defaults, UUID preparation and full returning hydration. `database.Transactor` lets one mutation pipeline compose into a pool, session or existing transaction. The shared compiler owns assignment/predicate binding and column selection for every model.

Verified on 2026-09-11 in the existing VM with Go 1.27.1:

- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke test-postgres` passed after compiler consolidation and write-outcome review. Real gopls includes Create/Update/Delete in consumer query completion.
- Protocol tests prove validation before transaction acquisition, exactly-one-row hydration, rollback on missing/multiple/malformed returned rows, nested transaction ownership and typed reconciliation candidates for unknown commits and committed after-callback failures. Neither failure triggers an automatic retry.
- Real PostgreSQL consumer tests prove identity/default omission, explicit zero/null values, exact decimals, UUID preparation without draft mutation, filtered writes, unique-constraint recovery, new-record deletion, failed returning hydration and nested rollback. Only newly created isolated fixture records are deleted; schemas and remaining test records are retained.
- The independent consumer's 24 required compile-failure assertions include incompatible write drafts and primary-key owners. Generation is reproducible/current and documentation checks pass.

Consumer review confirms that application writes specify domain values and keys while Foundry owns SQL, transactions/savepoints, complete decoding and error outcomes. The pagination delivery below completes milestone 05. Model lifecycle observers and outbox integration belong to milestone 07 and are not implemented by this slice.

### Milestone 05 pagination and completion evidence

The [pagination guide](../docs/guides/model-pagination.md) documents numbered totals, checked offsets, model-owned cursor requests/tokens, null-aware ordering, stable primary tie-breaking and backward traversal. Generated cursor getters preserve concrete field types and reuse the existing codec emission path. Cursor predicates use the same AST/compiler as other queries. Tokens are bounded, query-scoped positions; they are not signed credentials or cross-request snapshots.

Verified on 2026-09-11 using Go 1.27.1 in the existing VM:

- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke test-postgres` passed. Real gopls completes both pagination methods alongside generated reads/writes. Consumer generation is current and its 27 compile-failure assertions pass, including incompatible cursor owners/requests and a partial response record used as a write draft.
- PostgreSQL acceptance verifies numbered totals, empty and beyond-last pages, six order configurations across three page sizes, nulls, ties, mixed directions, exact decimal order, UUID/date boundaries, backward round trips, query/model token rejection and insertions around an existing cursor position. Malformed lookahead hydration rejects the whole page.
- Protocol tests verify validation before execution, canceled contexts, failed/negative counts, row/cleanup failures and discarded partial items/metadata. Unit tests check offset overflow, cursor structure/codec/scope validation, token-size and sort-count bounds.
- Ten-second targeted cursor fuzzing passed with 321,388 executions. A local two-field cursor planning/compilation baseline measured approximately 6.97 microseconds, 6,594 bytes and 144 allocations per operation on the VM's linux/arm64 runtime; this is a development baseline, not a production latency guarantee.
- After the resource-bound tests were added, `make fmt verify` and `go test -race ./database/query` passed. Documentation links, numbering and fixture toolchains remain aligned.

The [milestone checklist](05-typed-model-and-query-core.md#completion-checklist) maps each implementation slice to its evidence. Consumer review confirms model-owned public inputs/results, slim domain calls, explicit execution ownership, and documented runtime consistency limits. Milestone 05 is complete. Milestone 06 begins relations and advanced queries; lifecycle hooks remain in 07. Later framework milestones and the final framework-wide verification/audit remain outstanding.

### Milestone 06 direct-relation evidence

The [relation guide](../docs/guides/model-relations.md) documents `relation.One`/`Many`, ordinary Go key declarations, generated relation sets and typed slot attachment. Shared `ModelField` metadata now serves both cursors and relation keys. Direct belongs-to, has-one and has-many use the existing query AST/compiler with batched `IN` predicates; no relation-specific SQL compiler or string-key application API was introduced.

Verified on 2026-09-11 using Go 1.27.1 inside the existing project VM:

- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke test-postgres` passed. gopls completes relation fields and query loading methods, shows concrete source/target types in hover, and resolves relation-field definitions into generated consumer code without editing source.
- Real PostgreSQL consumer assertions verify one batched belongs-to query for four parents, configured two-key has-many batches, three queries for a nested self-relation, nullable inverse keys, ordered/empty collections, natural-key inverse relations, typed scopes, singular-cardinality failures, LoadMissing query avoidance, and errors without partial parent mutation.
- Shared limits bound depth, fetched rows and attached model expansion. Tests exercise cancellation before SQL, invalid/duplicate graphs before parent reads, fetched-row overflow and duplicate-parent collection expansion.
- The independent consumer's 31 required compilation failures include incompatible relation keys, source/target owners and scope predicates. A catalog relation targets a generated model in another package. Fresh self-relation generation is deterministic; invalid target/cardinality/declaration cases publish no output.
- Review consolidated metadata binding and rejected scoped source/target queries at the explicit Bind boundary, preventing discarded predicates. Pointer descriptors are captured by value, typed nil descriptors fail validation, and depth overrides retain their declared bounds. `make fmt verify`, `go test -race ./database/query` and `make test-postgres` passed after that review.

Consumer review confirms that domain code declares relation fields and typed key connections while Foundry owns batching, loading state and attachment. The many-to-many slice follows below. This is not milestone 06 completion: relation aggregates, projections/public joins, advanced SQL, upsert/locking and chunked streaming remain required. `Each` currently rejects eager-loading clauses, and callers use bounded collection reads or explicit batch Load. Later milestones and the final full-framework audit remain outstanding.

### Milestone 06 many-to-many evidence

The [relation guide](../docs/guides/model-relations.md#many-to-many-and-typed-pivots) documents `Through[Target, Pivot]`, generated three-model relation descriptors and complete `Link` values. `ManyToMany` connects two compatible key pairs through one concrete pivot model. Target and pivot filtering, SQL ordering and nested relations retain their model ownership. Generation excludes loaded slots from persisted fields and drafts.

Verified on 2026-09-11 with the existing Go 1.27.1 VM and isolated PostgreSQL fixtures:

- `make fmt verify test-postgres` passed. The required PostgreSQL command ran both framework and independent consumer tests with race detection. Four parent keys with batch size two used one root read and two joined reads. Nested target/pivot relations added batched queries with asserted counts.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke` passed. The new real-gopls probe completes generated many-to-many relation slots, shows all three concrete model types in hover, and resolves definitions into the consumer's generated file without editing it.
- Consumer coverage includes natural pivot primary keys, nullable natural target keys, duplicate edges retained as separate pivots, loaded-empty collections, LoadMissing query avoidance, duplicate-parent key batching, separate target/pivot scopes, deterministic mixed ordering and nested self-relations.
- Runtime failure coverage verifies row/attachment budgets (two model values per attached link), nested pivot depth before parent SQL, cancellation before SQL, ambiguous target joins returning TooManyRows, malformed pivot decoding and complete result discard after another branch succeeds. The same transaction remains usable after a scan failure.
- The consumer now requires 36 invalid public contracts to fail compilation, including mismatched pivot key types, mixed pivot model owners, wrong pivot filters/orderings and invalid link payloads. Fresh generator tests reproduce self-pivot declarations deterministically and publish no files for invalid target/pivot declarations.
- Existing model reads and joined relation reads now converge on one private SELECT node and PostgreSQL compiler. Aliased column equality, combined expression limits and SQL ordering have direct tests. Complete joined hydration reuses generated decoders, rejects borrowed RawBytes, and publishes no tuple when either model fails decoding. No dependency was added.

Consumer review confirms that applications declare typed domain keys/scopes while Foundry owns join aliases, binding, row hydration, batching and loaded state. The relation aggregate slice follows below. Milestone 06 remains in progress: projections/public joins, advanced SQL, upsert/locking and chunked streaming are still required. Lifecycle-aware attachment operations await the lifecycle pipeline; the final framework-wide verification and audit remain outstanding.

### Milestone 06 relation aggregate evidence

The [aggregate guide](../docs/guides/model-aggregates.md) documents generated `relation.Value[V]` slots and model-specific aggregate sets from handwritten `DefineAggregates` methods. `query.Related` checks source/target/result ownership; many-to-many `Pivot()` preserves the pivot input type. `With` and `Using` reuse existing model-owned loading and scope composition. The generator owns attachment and excludes computed fields from persistence and write drafts.

Verified on 2026-09-11 with Go 1.27.1 in the existing project VM:

- `make fmt verify test-postgres` passed initially. After review and the new compiler/gopls assertions, `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke test-postgres` passed. PostgreSQL tests use unique retained schemas and run both framework and consumer with race detection.
- Real PostgreSQL aggregates return exact sums beyond int64 and preserve fractional integer averages. Decimal/float summaries, all-null and absent groups, zero counts, field counts excluding NULL, nullable extrema, text minima and distinct target counts retain their declared result types and loaded states.
- The consumer asserts batched query counts, no related-model hydration, duplicate-parent key deduplication, LoadMissing avoidance, explicit parent/related/pivot scopes, nested aggregate loading, target-versus-pivot measurements and duplicate edge semantics.
- Failure cases cover grouped-row and scalar-attachment budgets, cancellation before SQL, rejection of ignored child-loading clauses, singular/many-to-many cardinality violations, non-finite aggregate results and complete result discard after an earlier branch succeeds. A scan failure leaves the transaction usable.
- The consumer now requires 44 invalid contracts to fail compilation. New cases reject unrelated aggregate input models, mismatched pivot inputs, incompatible result types/nullability, wrong query owners, and sums/averages on text, temporal and enum fields. Fresh aggregate generation is reproducible and invalid declarations publish nothing.
- Real gopls completes generated aggregate fields, exposes `AggregateRelation[User, int64]` in hover and resolves definitions into the consumer's generated source. No source edits are used for inspection.
- The shared SELECT node now supports aggregate selections and grouping. Numeric field descriptors expose operations appropriate to their type; integer/decimal inputs cast to numeric before SUM/AVG, while float summaries remain float64. Review preserved invalid binding errors through Using and corrected floating signed-zero grouping with a canonical model-key encoder and focused tests. Existing pivot identity checks share that encoder. No dependency was added.

Consumer review confirms that domain code declares concrete computed fields and typed expressions while Foundry owns grouping, aliasing, codecs, attachment, batching and failure behavior. Aggregates currently execute separately per selected slot and key batch. Scalar-query aggregates and declared grouping/projections follow below. Public joins/HAVING, parent relation predicates, subqueries/CTEs/windows, upsert/locking and chunked streaming remain milestone 06 work. The later framework milestones and final full-framework audit remain outstanding.

### Milestone 06 declared projection evidence

The [projection guide](../docs/guides/model-projections.md) documents handwritten `//foundry:projection` result structs, generated selection fields and complete decoders. `ProjectionQuery[Input, Result]` retains the input model's scope while returning the separate result type. Field/aggregate `Value()` expressions and explicit nullable promotion preserve concrete values and SQL nullability. The generator reuses model codec discovery, imported enum metadata and the existing package publication pipeline.

Verified on 2026-09-11 inside the existing Go 1.27.1 project VM:

- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke test-postgres` passed. The required PostgreSQL gate ran framework and independent consumer tests with race detection against isolated retained schemas.
- The consumer reads a deliberately reduced table containing only selected model columns, proving that projections do not fall back to whole-model hydration. Results retain model-owned IDs, imported enum codecs, nullable values and explicit SQL output aliases.
- Grouped reports preserve exact sums and fractional averages; result Count counts groups and honors limit/offset. Scalar aggregate queries over empty inputs produce one complete zero-count/NULL-summary record. Streaming callback errors close rows, canceled contexts avoid SQL, and malformed stored values discard collected results while leaving the transaction usable.
- The consumer now requires 52 invalid contracts to fail compilation. Projection assertions reject unrelated source scopes, wrong values/nullability, foreign result mappings, incompatible grouping/filter owners, grouping an aggregate and model writes on projected queries. The partial-record write assertion uses an actual generated projection type.
- Missing selections, duplicate mappings/grouping and ungrouped ordinary selections fail before SQL. Fresh generation is deterministic, invalid declarations publish nothing, and generated scope/decoder locals avoid shadowing consumer named types.
- Real gopls completes projection selection functions, shows concrete result and input-source types in hover, and resolves definitions into the consumer's generated reports package without source edits.
- Model, relation and projection reads use one SELECT compiler with ordered expression selections. No dependency was added.

Consumer review confirms that applications declare the desired record and typed field expressions while Foundry owns aliases, binding, grouping, complete decoding and stream cleanup. Aggregate ordering/HAVING follow below. Public joins/self-aliases/outer nullability, advanced SQL, projection page/cursor helpers, upsert/locking and chunked model streaming remain required in milestone 06. Milestones 07–24 and the final framework-wide verification and audit are still outstanding.

### Milestone 06 HAVING and aggregate ordering evidence

[Typed group filtering and ordering](../docs/guides/model-projections.md#filter-and-order-groups) retain the projection's input model. Aggregate comparisons produce `HavingPredicate[Model]`; ordinary row predicates require explicit `Grouped` conversion and validation against GROUP BY. Counts expose integer ranges; nullable numeric/extremum aggregates compare concrete typed values and expose NULL checks. Boolean existence exposes equality and membership. These aggregate capabilities retain the same result codecs through `Related` and `Value()`.

Verified on 2026-09-11 in the existing Go 1.27.1 VM:

- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke test-postgres` passed, covering root and independent consumer tests, real PostgreSQL, race detection and current generated output.
- Consumer reports rank totals with a typed key tie-breaker, filter input rows before aggregation, combine group conditions with AND/OR/NOT and count the filtered result window. Scalar tests preserve empty-input, NULL, boolean existence and empty-membership behavior through HAVING.
- All 61 negative public-contract fixtures fail compilation. New cases reject wrong HAVING model/value/nullability, boolean range comparisons, row/group predicate interchange, mixed group owners, aggregate ordering on model reads and foreign-model projection ordering.
- Shared compiler tests verify parameter order and captured values across WHERE/HAVING/window clauses, immutable query derivation, combined expression budgets, implicit grouping, undeclared sources, ungrouped columns, invalid descriptors and non-finite bindings. Private forged aggregate row predicates are rejected before SQL.
- Real gopls completes equality, range, NULL and ordering methods on an integer sum, shows its decimal argument and model-owned HavingPredicate result, and resolves the method definition in the installed framework source.

Review kept one comparison AST, binding implementation and clause compiler for row and aggregate conditions. Projection orders use the same SELECT node as model and relation reads; their broader expression capability cannot enter model ordering. No dependency was added. The typed join slice follows below; milestone 06 and the complete framework/final audit remain unfinished.

### Milestone 06 typed join and alias evidence

The [join guide](../docs/guides/model-joins.md) documents typed model aliases, scoped generated field sets, inner/left/right/full joins, composite/alternative ON conditions and source windows. Go scope identity includes the alias tag, model and join kind. Outer sides require nullable model scopes and generated nullable field sets. Existing nullable scopes remain nullable through a chained join. Generated projection builders infer the joined scope and retain complete typed selection checks without requiring consumers to spell compound generic types.

Verified on 2026-09-11 with Go 1.27.1 in the existing project VM:

- `make fmt generate fixture-check test-postgres` passed for the initial join runtime. After composite ON conditions and the compiler/gopls cases were settled, `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make verify race agent-smoke test-postgres` passed. PostgreSQL verification includes root and independent consumer modules with race detection and unique retained schemas.
- The consumer executes all four join kinds, self-relations, natural keys, joined grouping/HAVING and three-source chains. It verifies one-query projection reads, existing nullable fields, nullable imported enums, model-owned IDs, row multiplication and unmatched rows on either outer side.
- Filtered/ordered/windowed right inputs preserve left rows. Composite/alternative ON conditions and final WHERE predicates demonstrate their distinct semantics; source and outer parameters retain SQL order. Callback errors release rows, canceled contexts avoid I/O, incomplete selections fail before SQL and invalid stored enums discard collected results.
- All 79 negative public-contract fixtures fail compilation. Join cases reject wrong model field sets, alias/key/side ownership, outside or unlifted scopes, wrong ON predicates, non-nullable outer field access, inner-scope reuse on an outer join and loss of nullability through a chain. Fluent projection setters retain value types.
- Fresh generation compiles scoped joins and projection builders before any generated files exist, handles consumer names that collide with generated scope locals, remains deterministic and publishes nothing for invalid outer-scope code. Base model field-set aliases retain the original model-owned API.
- Real gopls completes ordinary/nullable joined field sets and fluent projection setters, shows concrete join/model/nullability types in hover and resolves definitions into the consumer's generated source without editing it.
- Compiler tests cover source windows and binding order, immutable chains, repeated alias names/identities, nil/invalid inputs, wrong runtime alias scopes, accidental outer references and cyclic nested SELECT bounds. Derived SELECT compilation restores its enclosing source scope and shares expression/parameter budgets. No dependency was added.

Consumer review confirms explicit domain aliases/keys and result selection while Foundry owns scoped field generation, join/nullability composition, SQL and decoding. Derived record sources and nested declared join results extend this slice below. Cross/lateral joins and non-equality column operators remain advanced-query work, alongside parent relation predicates, CTEs/unions/windows/distinct, upsert/locking, projection pagination and chunked model loading. Milestones 07–24 and the final framework-wide verification/audit remain outstanding.

### Milestone 06 derived records and uncorrelated subquery evidence

The [subquery guide](../docs/guides/model-subqueries.md) documents complete projections as aliased sources, generated record-owned fields, nullable derived joins and typed single-value/IN/EXISTS/scalar queries. `ValueQuery[Input, Value]` shares the projection runtime; the sealed single-value boundary preserves concrete values while separating the inner input scope from an outer predicate. The same SELECT AST/compiler owns nested SQL and parameter numbering. Rust references inspected include `FromItem::Subquery`, `Expr::Subquery`, `Condition::Exists` and their compiler cases; Rust's correlated examples remain a requirement for the next Go slice.

Verified on 2026-09-11 with Go 1.27.1 in the existing project VM:

- `make verify race test-postgres` passed, followed by `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make agent-smoke`. Review added direct nullable-scalar NULL/empty cases and strengthened cyclic-query/shared-parameter-budget coverage; the final `make fmt verify test-postgres` passed with race detection against root and consumer modules.
- The independent consumer executes grouped/HAVING/windowed derived sources, generated column aliases, imported enum predicates, nullable report joins and a complete joined report used as the right side of another join. Exact decimal and model-owned ID values survive all these boundaries.
- Membership covers matching/non-matching values, nullable subqueries, SQL NOT IN behavior, explicit removal of NULL inputs and empty sets. EXISTS preserves the row returned by a scalar count over empty input and observes HAVING/window removal. Scalar selection preserves NULL, empty input, nullable codecs, typed IDs and zero counts; multiple rows return SQLSTATE 21000 and are recovered through an explicit savepoint.
- Callback errors close value streams, canceled contexts never reach the executor, invalid stored enums discard partial collected values, and subsequent operations retain a usable transaction after local decoding failure. The consumer does not prefetch inner query results to compose the outer SQL.
- All 92 negative public-contract fixtures fail compilation. New cases reject wrong derived record owners, source predicates, nullable scopes and enum operators; wrong subquery IDs/values/input predicates; implicit nullable membership; scalar result/scope mismatches; and EXISTS predicates used by another outer scope.
- Fresh generation compiles derived-field access before generated output exists, reuses model field/codec emission, rejects invalid owners and generated-name collisions before publication, remains reproducible and passes stale-output checks. Derived column nullability comes from the same declared field type.
- Real gopls completes derived and outer-nullable report fields and `InQuery`/`InNullableQuery`; hover preserves alias/join/model/value types, and definitions resolve into the consumer's generated reports or installed framework source.
- Compiler checks cover nested parameter order, immutable query derivation, isolated SELECT scopes, membership requalification without altering inner fields, nil/zero/incomplete/eager inputs, negative windows, cyclic/deep queries and a shared PostgreSQL parameter bound. No new dependency was installed.

The enlarged monolithic consumer test package initially had its compiler process killed in the 4 GiB VM. A lower-GC-threshold diagnostic build passed. Advanced-query fixtures now live in their own consumer test package, and normal verification/race/PostgreSQL commands pass without compiler overrides.

Consumer review confirms that applications provide typed source/result declarations and query expressions while Foundry owns SQL scope, codecs, nullability, binding and execution. These APIs currently reject accidental correlation. Explicit correlated scopes, parent relation predicates, CTEs/unions/windows/distinct, non-equality/cross/lateral joins, upsert/locking, projection pagination and chunked model loading remain milestone 06 work. The remaining framework milestones and final framework-wide verification/audit are not complete.

### Milestone 06 correlated subquery evidence

The [correlation guide](../docs/guides/model-correlations.md) covers explicit outer/inner scopes, typed column comparisons and correlated EXISTS, membership and scalar selections. Correlated sources/values retain outer ownership and cannot enter ordinary standalone execution interfaces. Generated fields preserve their model/value types and nullable join state. All SQL still passes through the shared SELECT AST/compiler and codec contracts.

Verified on 2026-09-11 with Go 1.27.1 in the existing project VM:

- `GOFLAGS=-p=1 make verify race test-postgres` passed, followed by real gopls acceptance using the existing approved executable. After review added nullable-inner-join, nested-grouping, ON visibility and cyclic-qualification tests, `make fmt` and `GOFLAGS=-p=1 make verify test-postgres` passed. The PostgreSQL gate runs both root and independent consumer modules with race detection.
- The independent PostgreSQL consumer verifies per-user counts and exact nullable sums; correlated EXISTS/NOT EXISTS and ordinary/nullable membership; nested parent/grandparent references; equality/range column operators; grouped scalars and HAVING; per-row scalar limits versus globally windowed aliased sources; SQLSTATE 21000 recovery; and cancellation before execution.
- Nullable records on both outer and inner joins preserve absent rows and matched values. Correlated predicates constrain normal typed updates. A nested predicate on a many-to-many self-relation follows framework alias qualification without changing its independent inner sources.
- All 111 public-contract compile-failure fixtures pass. New cases reject wrong outer/inner/model scopes, unadapted predicates/selections, incompatible column values/IDs, enum ranges, nullable scope erasure, wrong correlated membership/scalar owners, and attempts to execute or erase the outer requirement of a correlated query.
- Real gopls completion, hover and definition locate generated correlated fields, scope-owned value methods and typed column range operators in the actual consumer workspace. Fresh generator overlay tests compile representative correlated APIs, and generated output remains current.
- Compiler tests preserve shared parameter ordering, ordinary subquery isolation, nested grouping requirements and immutable derivation/qualification. ON subqueries cannot reference later joins. Outer-only aggregates, shadowed/mismatched source names, invalid descriptors and cyclic qualification fail explicitly before database execution. No dependency was installed.

Verification initially encountered VM build-cache exhaustion and a cold parallel compiler process being killed. Only the disposable Go build/test cache was cleaned. Serial package compilation (`GOFLAGS=-p=1`) kept subsequent verification within the existing 4 GiB VM; database state, module downloads and installed tools were retained. The contributing guide documents this resource setting.

Consumer review keeps typed model operations as the normal application boundary. Manual correlation is the advanced composition API; simpler parent-relation predicates remain the next consumer-experience slice. Milestone 06 still requires those predicates, CTEs/unions/windows/distinct, generalized non-equality ON and cross/lateral joins, upsert/locking, projection pagination and chunked model loading. Model hooks/accessors/mutators remain milestone 07; later framework milestones and the final framework-wide verification/audit are not complete.

### Milestone 06 relationship existence filter evidence

The [relationship-filter guide](../docs/guides/model-relationship-filters.md) documents generated `WhereHas`/`WhereDoesntHave` and descriptor `Exists()` predicates for belongs-to, has-one, has-many and many-to-many. Rust `ModelQuery::where_has` and `where_has_many_to_many` are the behavior reference. The Go API composes existing typed descriptors and preserves concrete model query wrappers; it introduces no application SQL strings, alias declarations or untyped result maps.

Verified on 2026-09-11 with Go 1.27.1 in the existing project VM:

- Targeted query/generator tests passed, followed by `GOFLAGS=-p=1 make generate verify test-postgres`. The PostgreSQL command ran both root and consumer tests with race detection. Real gopls acceptance then passed with the approved installed executable, including new relationship-filter, predicate and typed-slice probes.
- PostgreSQL verifies direct/inverse relations, missing nullable keys, has-one existence without arbitrary hydration, alternative predicates and nested self-relations through four levels. Existence executes in one parent query, does not duplicate parents and leaves loaded slots unset.
- Many-to-many acceptance covers duplicate links, natural target keys, nullable/missing pivot targets, pivot filters, nested self-links, and relationship predicates inside both target and pivot scopes. The same scoped descriptor can filter parents and eager-load its children in two statements.
- Typed lookup methods remain available after filtering. Normal updates respect relationship predicates and return NotFound outside the scope. Cancellation and invalid related enum values fail before executor access.
- All 116 negative public-contract fixtures pass. New cases reject relationships owned by another model, aggregate slots used as existence relationships, relationship predicates placed on another model, and wrong lookup IDs after a generated `WhereHas` call.
- Fresh generation compiles relationship-filter calls before generated files exist, retains the concrete model query wrapper and reproduces current output. Real gopls resolves generated filter methods, parent-owned predicates and `All` returning a concrete model slice.
- Query review/tests verify deterministic alias allocation across nested self-relations and explicit correlated sources, captured descriptor immutability, typed target/pivot key checks, invalid/zero/nil metadata, and bounded cyclic/wide alias analysis. Existence intentionally uses filter clauses only; ordering/eager clauses remain available on the unchanged descriptor for loading. No dependency was installed.

Consumer review follows the idiomatic Go query/model/collection separation: builders compose without I/O, `First` returns an optional model, `RequireFirst` returns a model or error, and `All` returns `[]Model`. Milestone 06 remains in progress: CTEs/unions/windows/distinct, generalized non-equality ON and cross/lateral joins, upsert/locking, projection pagination and chunked model loading are still required. Model hooks/accessors/mutators remain in milestone 07. The full framework and final verification/audit are not complete.

### Milestone 06 nonrecursive CTE evidence

The [CTE guide](../docs/guides/model-ctes.md) documents reusable read-only definitions from complete model/projection records, typed aliases and immutable materialization choices. Rust's `Cte`, materialization options and `with_cte` compiler behavior were inspected alongside [PostgreSQL's WITH semantics](https://www.postgresql.org/docs/18/queries-with.html). Go references carry their definitions; dependency ordering and repeated-definition handling live in the framework rather than application registration code.

Verified on 2026-09-11 with Go 1.27.1 in the existing project VM:

- Query/generator tests, `GOFLAGS=-p=1 make verify` and `GOFLAGS=-p=1 make test-postgres` passed. PostgreSQL verification includes root and independent consumer tests with race detection. Generated output remains current.
- The PostgreSQL consumer verifies shared/dependent definitions, materialization options, grouped projections with HAVING and windows, nullable outer joins, enum/decimal codecs, count/existence, streaming callback cleanup and cancellation. A shared definition compiles once while separate aliases retain their field scopes.
- CTEs can contain internal relationship correlations and be referenced by explicit correlated subqueries, relationship existence filters, direct/through eager loading and relation aggregates. Scoped model updates/deletes preserve shared parameter numbering and reject writes outside their predicate.
- All 121 negative public-contract fixtures pass. New cases reject changing a CTE's record owner through explicit conversion, using another model's fields, exposing model writes on a CTE descriptor, substituting wrong model IDs and erasing nullable value types.
- Fresh generation checks CTE use of generated projection fields before outputs exist. Real gopls checks completion, hover and definition for CTE materialization and generated scoped fields. The full probe run passed existing probes and the new materialization probe; the new field probe initially expected an incorrect wrapper name. After correcting it to the existing `ScalarField[Alias[..., User], Status]` contract, its focused rerun passed.
- Review keeps dependency planning and automatic alias analysis on one bounded AST traversal. Tests cover descriptor immutability, conflicting definitions/materialization, invalid/zero/nil inputs, physical-table shadowing, dependency cycles and the shared PostgreSQL parameter bound. No dependency was installed.

Consumer review preserves separate query definitions, result records and ordinary Go slices. Applications declare typed records and compose their domain queries while Foundry owns SQL dependencies, binding, codecs and execution. Recursive CTEs require explicit typed self-reference and set-operation contracts and remain unfinished, alongside unions, windows/distinct, broader join conditions, upsert/locking, projection pagination and chunked model loading. Milestone 06, later framework milestones and the final framework-wide verification/audit are not complete.

### Milestone 06 typed set-operation evidence

The [set-operation guide](../docs/guides/model-set-operations.md) documents union, intersection and difference, including duplicate-preserving variants. Rust `Query::union`/`union_all`, `ProjectionQuery`, `SetOperationNode` and the PostgreSQL compiler's operand parentheses were inspected as behavior references. Go inputs retain complete result ownership, and combined records expose a new typed scope independently of either input.

Verified on 2026-09-11 with Go 1.27.1 in the existing project VM:

- `GOFLAGS=-p=1 make verify test-postgres` and real `make agent-smoke` with the approved gopls executable passed. PostgreSQL verification runs root and independent consumer tests with race detection. Generated output remains current; all 130 negative public-contract cases pass.
- PostgreSQL verifies all six operations, duplicate and NULL multiplicities, independent input/result windows, ordered optional/required first results, zero limits, selected-window counts/existence and immutable composition.
- Complete model results and shared projection records from different input models retain IDs, enums, nullable values and exact decimals. Combined records can be filtered, projected, aggregated, aliased, joined, placed in CTEs and used as outer correlation scopes. Shared CTE definitions are emitted once across the set tree.
- Single-value set queries retain their sealed value contract after ordering and pagination. Membership/scalar subqueries and scoped model updates remain typed. Relationship predicates discover nested CTE/set dependencies. Cancellation and callback failures release rows; failed model decoding discards partial collections.
- New compiler cases reject incompatible record owners, changing a set's record type by conversion, input/output scope mixing, wrong model IDs, nullable erasure, mismatched projection records, model writes on sets and multi-column record sets used as scalar queries.
- Fresh generation compiles model/projection set operations and value composition using generated fields. Real gopls completion, hover and definition verify fluent model unions, generated combined-record fields and concrete combined-value expressions in the independent consumer workspace.
- Review moved common projection/set result execution into one private reader, retained one shared compiler/binding path, and extended the bounded AST walker across set operands. Tests cover invalid/zero/nil inputs, inconsistent layouts, operand order/window preservation, shared parameter bounds, cyclic private ASTs and zero output expressions rejected before executor access. No dependency was installed.

Consumer review confirms ordinary typed builders and slices, with explicit output fields for post-combination operations. Recursive CTEs, windows/distinct, broader SQL value/conditional/JSON expressions, generalized non-equality ON and cross/lateral joins, upsert/locking, projection pagination and chunked model loading remain milestone 06 work. Lifecycle hooks/accessors/mutators remain milestone 07. The full framework and its final verification/audit are not complete.

### Milestone 06 complete record selection evidence

Delivered `SelectRecord(source, scope)` through the existing read-only projection runtime. A complete model selection returns its model type; a complete report selection returns its declared report type. Builders perform no I/O until execution, `First` is optional, `RequireFirst` requires a record, and `All` returns an ordinary typed Go slice. The [join guide](../docs/guides/model-joins.md#select-a-complete-model) owns the consumer contract.

Verified on 2026-09-11 inside the existing VM using Go 1.27.1:

- `GOFLAGS=-p=1 make fmt verify test-postgres` passed formatting, vet, root and consumer behavior tests, generation checks, documentation checks and isolated PostgreSQL acceptance with race detection. All 135 negative-compilation fixtures passed, including wrong selection scopes, nullable join sides, wrong collection result types, unadapted join scopes and model writes on read-only selections.
- Real PostgreSQL verifies complete model/report decoding, renamed columns, nullable values and enums; duplicate join multiplicity; preserved unmatched left/right records; optional/required first results and zero limits; selected-window counts/existence; CTE/set composition; canceled operations, callback cleanup and discarded partial results after codec failures.
- Root checks exercise runtime alias/layout mismatches, missing metadata/decoders, eager-loading rejection, later joins that make preceding sources nullable, immutable windows/parameters and original decoder reuse. Fresh generation compiles model/report selections and joined model sets without checked-in output.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOFLAGS=-p=1 make agent-smoke` passed all completion, hover and definition probes in 90.324 seconds, including the concrete model slice returned from a joined selection.

Review retained one shared SELECT compiler and result reader. Source scopes carry private ordered metadata from existing declarations; they do not define another schema or infer partial models. Nullable outer sides require declared projections, and model relation/computed slots remain unloaded. The initial negative-compilation attempt encountered a full VM disk; clearing only the 17 GB rebuildable Go compilation cache restored space, and the full gates above then passed. No dependencies were installed.

This completes the joined record-selection boundary needed for recursive model queries. Recursive CTEs and the other remaining milestone 06 capabilities are still pending; no recursive execution or milestone 07 lifecycle behavior is implied. The full framework implementation and final audit remain outstanding.

### Milestone 06 recursive CTE evidence

Delivered `RecursiveCTE` and `RecursiveAllCTE` with complete typed anchors, a construction callback receiving `RecursiveSelf[Record]`, and ordinary `CommonTable[Record]` results. Model and declared-report steps share the existing record compatibility checks, set compiler, CTE dependency planner, generated codecs and execution runtime. The [recursive CTE guide](../docs/guides/model-recursive-ctes.md) owns the public contract and limitations.

Verified on 2026-09-11 inside the existing VM using Go 1.27.1:

- `GOFLAGS=-p=1 make fmt verify test-postgres` passed formatting, vet, root/consumer behavior, generation freshness, documentation checks and isolated PostgreSQL acceptance with race detection. All 142 negative-compilation fixtures passed. New cases reject wrong step/self model types, self-type conversions, execution/writes on self references, wrong generated fields and incompatible natural keys.
- PostgreSQL verifies complete multi-level model hierarchies, empty and multiple anchors, traversal predicates, UNION deduplication, UNION ALL duplicate paths, unchanged-record cycles, declared report aliases/codecs, nullable fields, natural keys and derived working-table sources. Shared recursive definitions compose with sets, membership and relationship predicates, optional/required reads, result windows and streaming.
- Additional PostgreSQL cases accept supported recursive set operands, preserved sides of left/right joins and independent aggregate subqueries. Root checks reject misplaced/repeated/missing/escaped self references, dependency capture, nullable outer sides including later join prefixes, restricted set operands, direct recursive aggregates, malformed ASTs, physical-table shadowing and shared parameter/depth violations.
- Fresh generation compiles recursive report declarations using generated alias fields and complete joined selections. `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOFLAGS=-p=1 make agent-smoke` passed all completion, hover and definition probes in 96.947 seconds, including recursive constructors and generated working-table IDs.
- Stream review inspected the approved PostgreSQL driver's cleanup behavior. The added real regression passed after a further full `make fmt verify test-postgres`: nonterminating Count respects a deadline, and an endless stream can be canceled after its first delivered row while retaining the callback error and returning the pool to usable service.

Consumer review confirms no string result-column lists, raw recursive SQL, additional schema declarations or automatic model hydration outside the existing decoder. The callback constructs the step once; the database performs iteration. Recursive bodies emit a direct top-level UNION through the shared set compiler, while ordinary dependencies retain deterministic single emission. No dependencies were installed.

Runtime limits remain explicit: UNION is not general cycle detection, AST bounds are not iteration bounds, and `NotMaterialized` cannot inline a recursive CTE. Driver row closure may drain remaining results; immediate interruption requires query-context cancellation, which can invalidate its transaction/connection. These behaviors are documented and tested rather than hidden behind an implicit shared-transaction cancellation policy.

Windows/distinct, broader typed SQL value/conditional/JSON expressions, generalized non-equality ON and cross/lateral joins, upsert/locking, projection pagination and chunked model loading remain milestone 06 work. Model lifecycle hooks/accessors/mutators remain milestone 07. The full framework implementation, final verification and framework-wide audit remain outstanding.

### Milestone 06 distinct read evidence

Delivered `Distinct` and PostgreSQL `DistinctOn` through the shared complete-selection compiler and read runtime. Model entrypoints return read-only `ProjectionQuery[Model, Model]` values with the original generated decoder; projections, scalar values, combined records and explicit correlations retain their result and scope types. The [distinct guide](../docs/guides/model-distinct.md) owns the public contract.

Verified on 2026-09-11 inside the existing `foundry-go` VM:

- `GOMEMLIMIT=2GiB GOFLAGS=-p=1 make fmt verify test-postgres` passed root/consumer checks, all 146 negative-compilation fixtures, deterministic/current generation, documentation validation and the full PostgreSQL race suite.
- PostgreSQL verifies complete joined-model deduplication, declared projection hydration, nullable and enum values, grouping/HAVING, parameterized selected scalar/aggregate ordering, optional/required/streamed reads, distinct result-window counts, CTEs, sets, membership and explicitly correlated keys. `DistinctOn` selects the ordered complete record per group, including unselected keys and PostgreSQL's accepted shortened/permuted ordering prefix.
- Compiler checks reject empty/repeated/foreign/zero keys, invalid modes, ungrouped keys, invalid ordering prefixes, unselected distinct ordering, mismatched scalar bindings, missing model metadata and eager input. They verify immutable key derivation, bounded matching work, shared placeholder ordering and outer-reference requalification. Consumer type failures cover wrong key/predicate owners, incompatible single-value results and attempts to write through a distinct read.
- Fresh generation compiles representative model, projection and value distinct calls from handwritten declarations. No generator-owned output or dependencies changed.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=2GiB GOFLAGS=-p=1 make agent-smoke` passed all real-gopls completion, hover and definition probes in 101.640 seconds, including promoted model distinct methods and their concrete read-only return types.

The first ordinary consumer build had its compiler process killed. Retrying with the documented soft Go memory setting passed the full suite without reducing fixtures or disabling race instrumentation. The VM environment and database services were not replaced or reconfigured.

Consumer review confirms builder/model/slice separation, generated field completion, complete model hydration and ordinary error handling. Distinctness is applied before the query's limit/offset; aliasing creates an explicit outer boundary when deduplication must follow an already limited input. Computed distinct keys, window functions, broader typed SQL expressions, generalized join conditions and cross/lateral joins, upsert/locking, projection pagination and chunked model loading remain milestone 06 work. Lifecycle behavior remains milestone 07, and the full framework implementation and final audit remain outstanding.

### Milestone 06 core window evidence

Delivered scope-owned `WindowFor` definitions and typed ranking, distribution, navigation and aggregate window expressions. Generated projection records retain their complete result types and codecs. ROWS/GROUPS frames, current/unbounded RANGE positions and exclusions share the SELECT compiler and read runtime. The [window guide](../docs/guides/model-windows.md) owns the public contract and current limits. Rust `WindowSpec`, frame AST/compiler code and window compiler tests were inspected as behavior references alongside PostgreSQL's window/frame documentation.

Verified on 2026-09-11 with Go 1.27.1 in the existing `foundry-go` VM:

- `GOMEMLIMIT=2GiB GOFLAGS=-p=1 make fmt verify test-postgres` passed root/consumer checks, generation freshness, documentation validation and isolated PostgreSQL acceptance with race detection. All 156 negative-compilation fixtures passed, including window scope, result type/nullability, fallback values, predicate placement and incompatible frame boundaries.
- PostgreSQL verifies partition ranking and peer distributions, bucket codecs, running/moving/whole frames, exclusions, legal empty frames, exact nullable sums/averages, zero counts/existence, navigation independent of frames, nullable fallbacks and negative lag offsets. Typed outer projection filters, CTEs, sets, distinct selected-window ordering, grouped aggregate inputs, explicit correlations and recursive steps compose through the existing AST.
- Count/Exists preserve selected result windows, zero limits remain empty, and callback errors release streaming rows. Invalid declarations, canceled contexts and malformed fallback values fail before executor access. Compiler tests cover same-level window nesting, independent scalar windows, grouping requirements, dependencies, captured outer requalification, parameter ordering, immutable derivation and bounded cyclic/private AST traversal.
- Fresh generation compiles representative window expressions from handwritten declarations. Three new consumer projection records use the existing generator pipeline and owned manifest; no second result-schema mechanism was introduced.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=2GiB GOFLAGS=-p=1 make agent-smoke` passed all real-gopls completion, hover and definition probes in 108.743 seconds, including scope-inferred window builders and concrete rank result types in the independent consumer workspace.

Manual review reused aggregate SQL emission and the existing AST walker, correlation renamer and projection execution. Frame positions now have a different underlying representation from numeric row/group offsets, so an explicit Go conversion cannot erase their distinction; a negative-compilation fixture verifies that boundary. No dependencies were installed.

The first full verification attempt exhausted the VM's disk while compiling consumer packages. Inspection found 16 GB in the rebuildable Go compilation cache. Clearing only that cache with `go clean -cache` restored 17 GB of free space; the full checks passed afterward and again after the frame-type review fix. Source, generated files, downloaded modules, credentials and databases were preserved.

Consumer review retains the builder/model/slice separation: `All` executes the current query and returns a typed slice; `Count` executes a database count and `len` counts loaded items. Existing numbered/cursor pagination remains documented in the [pagination guide](../docs/guides/model-pagination.md). The [HTTP blueprint](08-http-validation-and-responses.md#automatic-pagination-at-the-http-boundary) now explicitly requires automatic typed pagination input, defaults/bounds, DTO mapping and links; those HTTP adapters are not implemented yet.

Computed partition/distinct keys, typed numeric/temporal RANGE distances, aggregate FILTER expressions, named WINDOW declarations, broader SQL value/conditional/JSON expressions, generalized non-equality ON and cross/lateral joins, upsert/locking, simple offset/projection pagination and chunked model loading remain milestone 06 work. Lifecycle hooks/accessors/mutators remain milestone 07. The full framework implementation, final verification and framework-wide audit remain outstanding.

### Milestone 06 simple and projected page evidence

Delivered model `SimplePaginate` and numbered/simple pagination for complete projection, set and single-value results. Existing `PageRequest` bounds and `Page[Result]` metadata are reused; `SimplePage[Result]` exposes items, number, size and lookahead-based `HasMore`, without invented totals. The [pagination guide](../docs/guides/model-pagination.md) owns ordering, nested-input and consistency contracts. Rust model/projection `paginate` implementations and Laravel's simple-pagination behavior were inspected as references.

Verified on 2026-09-11 with Go 1.27.1 in the existing VM:

- `GOMEMLIMIT=2GiB GOFLAGS=-p=1 make verify` passed formatting, vet, root and independent consumer checks, all 160 negative-compilation fixtures, generation freshness and documentation validation. New public type failures cover model/projection page ownership, nullable selected results and unavailable totals on simple pages.
- `GOMEMLIMIT=2GiB GOFLAGS=-p=1 make test-postgres` passed the full root and consumer suite with race detection. PostgreSQL checks count-free reads, natural keys, exact last/empty/beyond-last pages, grouped/HAVING totals, nullable/enum/decimal hydration, joins, DISTINCT ON winners, deduplicated values, sets, nested CTE/operand limits and window evaluation before the page offset.
- Eager simple/cursor pages load relations only for returned models. Forward/backward cursor acceptance and a strict related-row budget prove the hidden lookahead row cannot consume that budget. Invalid hidden rows still fail decoding; related-read/count/read failures return zero pages and preserve errors.
- Unit checks exercise request/offset bounds, missing or invalid ordering, immutable window derivation, negative/maximum counts, count-before-read failure handling and clearing references retained by hidden rows. Fresh generation compiles explicit model and projection page result types without checked-in generated output.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=2GiB GOFLAGS=-p=1 make agent-smoke` passed all real-gopls completion, hover and definition probes in 115.192 seconds, including concrete model simple-page and declared-report numbered-page results in the independent consumer workspace.

Manual review centralized numbered-page execution/metadata and simple/cursor lookahead disposal. Projection pages reuse the existing complete result reader and SELECT compiler; their counts retain grouped/distinct/set semantics and winner ordering. Explicit outer ordering is required because report/join uniqueness cannot generally be inferred. Nested input limits remain part of the counted result. No dependency or generated schema mechanism was added.

The first PostgreSQL attempt exhausted VM disk space during consumer compilation. Inspection found about 14 GiB of rebuildable Go cache and no running compiler. Clearing only that cache restored 16 GiB of free space, and the full PostgreSQL command then passed. The contributing guide now records this recovery procedure; source, generated files, downloaded modules and databases were preserved.

Consumer documentation also clarifies native typed slices and standard in-memory sorting. Sorting a loaded slice is separate from database ordering before pagination; a custom collection wrapper is not required for typing or ordinary slice utilities. Automatic HTTP pagination adapters remain milestone 08 work. Projection cursors, chunked model loading, window/value-expression extensions, generalized joins and upsert/locking remain milestone 06 work. Lifecycle remains milestone 07; full framework implementation, verification and final audit are still outstanding.

### Milestone 06 bounded model iteration evidence

Delivered `Chunk`/`EachChunked` for stable offset windows and `ChunkByID`/`EachByID` for declared primary-key traversal, including descending natural keys. Model `Each` now supports eager relations and aggregate slots through bounded batches. The [iteration guide](../docs/guides/model-chunks.md) owns callback, ordering, consistency and resource contracts. Rust chunk/primary-key iteration and its callback tests were inspected as references; Go preserves ordinary error/panic behavior and rejects invalid sizes or incompatible key orderings explicitly.

Verified on 2026-09-11 with Go 1.27.1 in the existing VM:

- `GOMEMLIMIT=768MiB GOFLAGS=-p=1 make verify` passed root and independent consumer tests, all 164 negative-compilation cases, formatting/vet, fresh generated output and documentation checks. New compiler cases reject another model's slice or individual callbacks for each traversal method.
- `GOMEMLIMIT=768MiB GOFLAGS=-p=1 make test-postgres` passed the full root and independent consumer suite with race detection. New chunk acceptance checks selected offsets/limits, empty windows, retained slices, primary-key boundary capture before callback edits, updates that change matching status, descending natural keys, eager query counts/budgets, and same-transaction work inside callbacks after rows close. It also checks later-read failures, malformed batch rows, singular relation failures, cancellation and callback errors/panics.
- Root tests check immutable query derivation, bounded sizes, offset overflow, key metadata/codec failures, nil callbacks, canceled contexts and constant-size boundary predicates over repeated batches. Fresh generation compiles all four methods against newly generated models.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all completion, hover and definition probes in 102.751 seconds. New probes inspect `ChunkByID` and `EachChunked` with concrete user slice/model callbacks; moved join/projection probes remain green.

Manual review reused stable model ordering, complete model hydration/eager loading, generated key getters/codecs and the existing keyset predicate compiler. Offset and key traversals share one batch loop. Key boundaries are captured before callbacks, parent/related rows close before publication, and related-row budgets reset per batch. Callers still own retained slices, external effects, transaction isolation and stable stored keys.

The first monolithic consumer build exceeded the shared 4 GiB VM's memory ceiling, including after lowering the Go heap target. Join and projection acceptance tests were moved into feature packages, following existing fixture organization; their shared query counter moved into consumer `internal/queryfixture`. All assertions and gopls probes were retained. Compilation and normal verification then passed with the smaller memory target. No environment replacement, dependency installation or database reset was needed.

Projection cursors, computed partition/distinct keys, typed RANGE distances, aggregate FILTER/named windows, broader typed value expressions, generalized joins and upsert/locking remain milestone 06 work. Lifecycle behavior remains milestone 07. The full framework implementation, verification and final audit remain outstanding.

### Milestone 06 typed upsert and batch insert evidence

Delivered generated `Upsert`, `CreateMany` and `UpsertMany` over the shared insert compiler and transaction/returning runtime. Model-owned conflict policies support composite fields, named constraints, incoming values, typed constant/NULL assignments, skipped conflicts and conditions on the existing row. The [upsert guide](../docs/guides/model-upserts.md) owns the consumer contract. Rust conflict builders and locking/upsert acceptance were inspected; PostgreSQL's INSERT/SELECT references were checked for database semantics. Row locking remains a subsequent implementation slice.

Verified on 2026-09-11 with Go 1.27.1 in the existing VM:

- `GOMEMLIMIT=768MiB GOFLAGS=-p=1 make verify` passed formatting/vet, root and independent consumer checks, all 175 negative-compilation cases, generation freshness and documentation checks. New cases reject wrong conflict targets/updates/conditions, incompatible values/nullability, policy/draft/result owners and conversion between model-owned policies.
- `GOMEMLIMIT=768MiB GOFLAGS=-p=1 make test-postgres` passed the full root and independent consumer suite with race detection. New acceptance covers per-row default/zero/NULL states, mixed insert/skip batches, composite and named targets, retained UUIDs, all-default batches, a target table named `excluded`, and simultaneous upserts returning the same stored identity.
- PostgreSQL checks prove rollback on duplicate update targets and a later malformed decimal in RETURNING, with the outer transaction usable afterward. Protocol tests cover too few/many rows, partial decoding, row-cleanup failure, empty batches without transaction acquisition, and uncertain commits retaining typed optional or slice candidates.
- Compiler tests cover deterministic column unions and DEFAULT cells, identifier quoting, target alias allocation, conditional CTE discovery/parameter order, immutable policies, invalid target/update metadata, required fields and bounds. Fresh generation compiles the new single and batch methods from handwritten model declarations without relying on checked-in output.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all completion, hover and definition probes in 127.225 seconds, including generated optional-model upserts, typed batch drafts/results and nullable conflict assignments.

Manual review consolidated insert validation, binding and returned-row handling instead of creating model-specific SQL paths. Normal creates reuse the insert plan; ordinary mutations and upserts share transaction outcome handling. Conflict aliases use the existing bounded name allocator and correlation qualification. Batches preserve source drafts and validate fully before opening a transaction. SQL upserts and bulk inserts retain explicit set-based lifecycle semantics; milestone 07 owns hook-aware alternatives.

The first full verification exhausted VM disk space during consumer compilation. The stopped-build inspection found about 14 GiB of rebuildable Go cache. Clearing only that cache restored about 15 GiB of free space; the complete verification and PostgreSQL commands then passed. Source, generated code, downloaded dependencies and databases were preserved. No new dependency was installed.

Row locking, projection cursors, computed partition/distinct keys, typed RANGE distances, aggregate FILTER/named windows, broader value/conflict expressions and expression/partial-index conflict targets, and generalized joins remain milestone 06 work. Model lifecycle remains milestone 07. The full framework implementation, verification and final audit remain outstanding.

### Milestone 06 transaction-scoped row locking evidence

Delivered model, projection and scalar locking reads over the shared SELECT compiler and complete-result runtime. All four PostgreSQL row strengths support waiting, `NoWait`, `SkipLocked` and restoring ordinary waiting. Generated locked model queries retain exact natural/model-ID lookup types; typed `Of` scopes select preserved join inputs. Every locked terminal requires `*database.Tx`. The [row-locking guide](../docs/guides/row-locking.md) owns the consumer and failure contracts. Rust lock builders and contention acceptance, and PostgreSQL SELECT/explicit-locking documentation, informed the implementation.

Verified on 2026-09-12 (Asia/Kuala_Lumpur) with Go 1.27.1 in the existing VM:

- `GOMEMLIMIT=768MiB GOFLAGS=-p=1 make verify` passed the full framework and independent consumer checks, including all 186 required compilation failures and current generated output. New negative cases reject pool executors, wrong natural/model IDs, unrelated predicates/results/targets, nullable join targets, and locked builders used as ordinary subquery, count or mutation APIs.
- `GOMEMLIMIT=768MiB GOFLAGS=-p=1 make test-postgres` passed the full root and consumer suite with race detection. Locking acceptance verifies real contention, skip-locked result slices, compatible/incompatible strengths, commit release, savepoint rollback/release, OFFSET locks, zero limits, cancellation during a waiting read, and recovery through a savepoint after `NOWAIT` failure.
- PostgreSQL also verifies typed outer-join targets without locking the other side, ordinary derived-source lock propagation, CTE-backed predicates, concrete scalar results, read-only transaction errors, eager parent loading and stream cleanup. Protocol tests cover partial hydration/cleanup failure, caller-owned rollback, closed/nil transactions and default primary-key ordering.
- Compiler tests reject invalid/duplicate/unknown targets, outer-join null extension, non-row results, CTE-only targets and ambiguous schema-qualified `OF` references. Strengths, wait modifiers, filters, windows, bindings and immutable derivation remain intact. Fresh-generation acceptance compiles the new APIs without checked-in generated code.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all completion, hover and definition probes in 132.960 seconds, including transaction-only slice reads, generated natural-key lookup and typed join-lock targets.

Manual review reused ordinary model-first ordering and the existing read/row-cleanup runtime. Join null extension is checked in linear time. Explicit derived targets with no underlying lockable table are rejected. Locked builders remain terminal read plans so source composition cannot silently discard their transaction requirement. Documentation distinguishes client-side decode/callback failures from server aborts, cancellation and connection loss; closing rows is not an explicit unlock operation. Bounded `All` is the consumer path for locking and then writing on the same transaction.

An initial race rebuild exhausted VM disk space. After the build stopped, inspection identified about 15 GiB of rebuildable Go cache; clearing only that cache restored about 15 GiB free. The complete standard and PostgreSQL verification then passed. No dependencies, source, generated files, credentials or database data were removed, and no dependency was installed.

Milestone 06 remains in progress: projection cursors, computed partition/distinct keys, typed RANGE distances, aggregate FILTER/named windows, broader value/conflict expressions and expression/partial-index conflict targets, generalized joins, and explicit transaction-preserving locking within CTE/subquery definitions or with multiple per-input lock clauses remain required. Model lifecycle remains milestone 07. Full framework implementation, verification and the final framework-wide audit remain outstanding.

### Milestone 06 result cursor pagination evidence

Implemented `CursorFor` and `ValueCursorFor` over completed model/projection, CTE, set and scalar sources. Generated output scopes retain exact field/result types. Required `UniqueBy` declarations identify complete result rows and append missing ascending tie-breakers. The [result cursor guide](../docs/guides/result-cursor-pagination.md) owns public examples, uniqueness assertions, nested-query semantics, native slice behavior and error contracts. Rust model cursor behavior and PostgreSQL ordering semantics informed the shared boundary implementation.

Verified on 2026-09-12 (Asia/Kuala_Lumpur), using Go 1.27.1 in the existing VM:

- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make verify` passed for the final code, including all 194 negative compilation cases, fresh-generation acceptance, current generated output and documentation checks. New compiler cases reject wrong result/request owners, source/output field-scope interchange, incompatible nullable scalar requests, writes and unrestricted source composition.
- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make test-postgres` passed the full framework and independent consumer suite with race detection. Result cursors verify forward/backward traversal, page-size changes, mixed/nullable ordering, all-NULL terminal boundaries, exactly full last pages, single-query execution, preserved source windows, and complete model/projection/scalar decoders.
- PostgreSQL composition checks cover aliases, ordinary/recursive CTEs, sets, grouped exact-decimal totals, composite joined identities, distinct winners including their selected order IDs, and window values that retain their original positions across pages. Malformed lookahead rows and oversized endpoint tokens discard the entire page and release row ownership.
- Shared unit/protocol checks verify immutable builders, source-window parameter order, required field codecs/getters, non-nullable key validation, changed input/identity fingerprints, bounded appended identity fields, canceled/rejected requests, driver and cleanup failures, and zero-page failure results. Fresh generation compiles the new record/scalar APIs without checked-in generated files and remains deterministic/current.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all real-gopls completion, hover and definition probes in 123.700 seconds. New probes retain `CursorRequest[UserSummary]`/`CursorPage[UserSummary]` and the generated nullable field's `CursorScope[UserSummary]`, resolving definitions into the installed framework and generated consumer source. Existing join probes remain green after the test split.

Review shares field extraction/codecs between model and projection metadata and propagates them through existing source composition. The model and result paths share nullable boundary decoding, order reversal, bounded token construction and page navigation. The outer cursor uses the existing SELECT compiler and complete-result row cleanup. Consumer query results and page items remain ordinary typed slices. No dependency was installed.

Verification encountered disk exhaustion from accumulated ordinary/race compilation caches. Clearing only disposable Go build cache after builds stopped restored space. A large join acceptance package also had its compiler killed, including on a lower-memory retry. Splitting it into self/filtered, inner/grouped, outer/chained and natural-key packages preserved all 25 original assertions and shared schema/seeding through `queryfixture.RunJoins`. Every split package passed PostgreSQL/race checks. After a final cache cleanup, the complete PostgreSQL gate passed with the code unchanged; no source, generated output, credentials or database data was removed. [Contributor guidance](../docs/guides/contributing.md) records bounded compiler concurrency, soft memory limits and cache recovery.

Milestone 06 remains in progress. Required remaining work includes computed partition/distinct keys, typed RANGE distances, aggregate FILTER/named windows, broader value/conflict expressions and expression/partial-index conflict targets, generalized joins, and transaction-preserving locking within CTE/subquery definitions or with multiple per-input lock clauses. The full framework and final framework-wide audit are not complete.

### Milestone 06 aggregate filter evidence

Delivered [typed aggregate filters](../docs/guides/model-aggregate-filters.md) for ordinary grouped calculations, windows and generated relation slots. `Filter` accepts model/scope-owned row predicates and preserves exact/nullable result codecs and ordered comparison capabilities. Existing row predicates, CTE discovery, correlation qualification, resource bounds and SQL binding remain shared. Handwritten model declarations and fetched native slices retain their existing shapes.

Verified on 2026-09-12 using the existing VM's Go 1.27.1:

- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make fmt generate verify` passed formatting/vet, root and consumer behavior, fresh-generation acceptance, all 200 negative-compilation cases, output freshness and documentation checks. Six new cases reject wrong model predicates, group predicates as row filters, wrong field/comparison values, boolean ordering and filters applied after window evaluation.
- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make test-postgres` passed the complete root and independent consumer suite with race detection. New acceptance covers independent grouped measures/HAVING, exact averages, false/NULL filtering, empty results, filtered window frames, grouped windows, CTE dependencies and correlated counts.
- Relation acceptance covers filtered direct/target/pivot aggregates, correlated target qualification, duplicate edges and explicit loaded states. Filtering out every measured row still rejects singular-cardinality violations and ambiguous through joins.
- Compiler regression tests cover immutable derivation, ordered bindings, FILTER-before-OVER placement, grouped-window validation, invalid fields/predicates, resource bounds and nested correlation ownership. Outer-only ordinary aggregate filters fail before PostgreSQL can change the owning SELECT.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all completion, hover and definition probes in 147.288 seconds. New probes preserve the filter's `Predicate[Order]` scope, nullable aggregate wrapper, decimal comparison argument and `HavingPredicate` result while resolving definitions into the installed framework source.

Manual review preserved the shared aggregate renderer and typed wrapper capabilities, and validates direct filter fields before relation alias qualification. The consumer uses ordinary generated field predicates in `Sum().Filter(...).Value()` or `Filter(...).Over(...)`; no raw SQL or custom collection is required. The fresh-generation fixture also compiles filtered aggregates and windows without checked-in output.

The first PostgreSQL run was interrupted when accumulated Go build cache nearly filled the VM disk. After its processes stopped, clearing only disposable compilation cache restored 13 GiB free. The complete PostgreSQL gate then passed on unchanged source. Source, generated files, credentials and database data were retained; no dependency was installed.

Milestone 06 remains in progress: computed grouping/partition/distinct keys, typed RANGE distances, named windows, broader value/conflict expressions and expression/partial-index targets, generalized joins and transaction-preserving composed locks remain required. The full framework and final framework-wide audit are still outstanding.

### Milestone 06 typed conditional expression evidence

Delivered [typed conditional expressions](../docs/guides/conditional-expressions.md): row values, CASE, COALESCE, NULLIF, explicit nullable promotion and codec-owned standalone parameters. Selected-value counterparts compose with aggregates and windows. Row and selected phases retain distinct Go contracts while sharing AST traversal, validation, SQL rendering, qualification, dependency discovery and result decoding. Rust's CASE/value AST and compiler, together with PostgreSQL's conditional/type-resolution semantics, informed this implementation.

Verified on 2026-09-12 using Go 1.27.1 in the existing VM:

- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make fmt verify` passed after the final fixture split. All framework and independent consumer checks pass, including fresh-generation acceptance, 216 negative-compilation cases, current generated output and documentation checks. New negative cases reject wrong owners, values, IDs and nullability, unfinished CASE builders, and aggregate/window values entering row-only APIs.
- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make test-postgres` passed the complete framework and consumer suite with race detection. Acceptance covers first-true CASE branches, unknown conditions, empty strings versus NULL, single nullable layers, numeric ordering of parameter-only branches, empty aggregate fallbacks, grouped/window calculations and computed model-write predicates.
- PostgreSQL composition verifies selected scalar subqueries, CTE discovery, explicit correlations, declared projections and aliased outputs. Direct/target/pivot relationship filters preserve computed values and nested captured references through qualification. Standalone parameters round-trip exact decimals, typed IDs, boolean/integer/float/text/binary values and temporal types through their declared SQL representations.
- Compiler and runtime checks cover immutable derivation, one-time parameter encoding, owned byte buffers across repeated compilation, invalid values before I/O, depth/node limits, nested-window rejection and grouping boundaries. Empty `In()` validates its computed operand while discarding bindings for SQL replaced by `FALSE`, preserving parameters before and after it.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all real completion, hover and definition probes in 163.340 seconds. New probes preserve concrete `Predicate[User]`, `RowExpression[User, int]` and `Case[User, string]` contracts and resolve definitions into the installed framework source. Existing correlation probes remain green.

Review kept codec representation metadata below the PostgreSQL compiler and preserved it through nullable/validated adapters. Custom codecs explicitly declare parameter representation; no raw SQL type string enters the public parameter API. The same structural visitor supports compilation, analysis and qualification. Native model/result slices and the shared execution/cleanup runtime remain the consumer boundary. The final framework-wide audit is still a later required step.

Verification exposed two environment limits: the correlation fixture's compiler was killed for memory exhaustion, and combined ordinary/race caches plus temporary build files exhausted the 20 GiB VM disk. Moving nullable joined correlations into a smaller package preserved the assertion body exactly and reused the existing isolated fixture. Ordinary verification then passed; clearing only stopped-build compilation cache before the full PostgreSQL/race run restored sufficient space, and that complete gate passed. [Contributor guidance](../docs/guides/contributing.md) records this verification sequence. Source, generated output, credentials and database data were retained; no dependency was installed.

Milestone 06 remains in progress. Required work includes computed grouping/partition/distinct keys, typed RANGE distances, named windows, broader arithmetic/text/JSON and conflict expressions, expression/partial-index targets, generalized joins and transaction-preserving composed locks. Model lifecycle remains milestone 07; the full framework objective is not complete.

### Milestone 06 computed query key evidence

Implemented [computed query keys](../docs/guides/computed-query-keys.md): `RowExpression.Group()` extends existing row keys; `Expression.Key()` supplies selected keys to `PartitionByValues` and `DistinctOnValues`. Aggregate values remain excluded from `GroupBy` by Go types. One bounded canonical-expression index supports grouping, distinct ordering and partition duplicate detection. Matching includes encoded values and reuses parameter identities within one SELECT, including grouped children inside larger expressions. SQL rendering, dependency discovery, correlation qualification and result execution reuse the existing AST.

Verification on 2026-09-12 in the existing Go 1.27.1 VM:

- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make verify` passed all framework and independent consumer checks, including 224 negative-compilation cases, fresh-generation acceptance, current generated output, formatting, vet and documentation checks. New compiler cases reject wrong model keys, selected aggregate keys in row groups and explicit key-owner conversions.
- Root and consumer behavior checks cover parameter-aware key matching, grouped subexpressions, invalid duplicate keys, aggregate partitions, nested-window rejection, immutable descriptors and cursor identities restricted to declared output fields. Empty membership folding invalidates cached keys whose parameter slots were discarded.
- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make test-postgres` passed the complete framework and consumer suite with race detection. Computed-key acceptance verifies grouped report hydration/HAVING, nested grouped parameter reuse, integer keys versus output ordinals, NULL/empty keys, distinct winner precedence, aggregate/window keys, combined values, CTE discovery and explicit correlations.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all real completion, hover and definition probes in 173.515 seconds. New probes preserve `Group[User]`, `ProjectionKey[User]` and scope-owned window signatures and resolve their definitions in the framework source from the consumer workspace.

Review also corrected custom codec byte copying to preserve non-nil empty buffers versus nil. Unit coverage retains mutable-buffer ownership, and real PostgreSQL bytea/NULL round trips pass with race detection.

The first ordinary verification attempt exhausted disk space during consumer compilation without an assertion failure. After it stopped, `go clean -cache` restored 13 GiB and the complete ordinary gate passed. Only rebuildable compilation cache was cleared. Clearing that cache again before the separate PostgreSQL/race gate kept both runs within disk capacity, and the full database gate passed. Source, generated output, credentials and database data remain intact, with no new dependency installed. The hardening blueprint now includes representative consumer build/compiler-memory and language-tooling measurements separately from full-suite costs.

Milestone 06 remains in progress. Typed RANGE distances, named windows, broader value/conflict expressions, expression/partial-index targets, generalized joins and transaction-preserving composed locks remain required. Model lifecycle and the remaining framework milestones are still outstanding.

### Milestone 06 typed RANGE and named window evidence

Delivered [typed RANGE frames and named windows](../docs/guides/window-ranges-and-names.md) through the shared SELECT AST. Numeric builders retain the concrete distance type and codec; temporal builders use immutable calendar/elapsed `temporal.Interval` values. Nullable ordering keeps non-nullable distances. Completed frames remain ordinary typed window descriptors. Named descriptors carry their definitions, emitted once per SELECT with dependencies first and PostgreSQL inheritance validation.

Verification on 2026-09-12 in the existing Go 1.27.1 VM:

- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make verify` passed all framework and independent consumer checks, including 234 negative-compilation cases, fresh-generation acceptance, current generated output, formatting, vet and documentation checks. Ten new compiler cases cover incompatible scopes, numeric/temporal inputs, distance types, nullable distances, ordinary versus typed boundaries, explicit boundary conversion and named-window ownership.
- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make test-postgres` passed the complete framework and consumer suite with race detection. RANGE acceptance covers integer/decimal/float distances, descending order, physical smallint/real columns, exact values beyond float precision, NULL peers, empty frames, exclusions and ordering grouped rows by ordinary aggregates. Temporal cases distinguish calendar days from elapsed hours across DST, verify leap-year month ends, and preserve interval meaning under different PostgreSQL interval styles.
- Named-window acceptance covers shared definitions and bindings, inheritance, distinct selection, result counting, aggregate partitions, CTEs used inside definitions, independent nested SELECT names and explicit correlations. Compiler tests reject duplicate names, cycles, forbidden inheritance, nested windows and invalid distances before execution, including zero-limit queries. Distance encoding captures mutable codec output once and preserves immutable statement arguments.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all completion, hover and definition probes in 190.556 seconds. New probes retain concrete model/numeric/interval types in range boundaries, return the ordinary scoped window from `Between`, and resolve `Named` into the framework definition from the consumer workspace.

Review retained one structural walker for dependency discovery, with local SELECT discovery for named definitions. Correlation qualification preserves shared definition identity without mutating original descriptors. Numeric distance encoding reuses existing parameter capture and codec validation. Calendar components remain distinct from elapsed time; time-of-day ranges reject calendar components that PostgreSQL would otherwise ignore. Database arithmetic overflow and physical-schema mismatches remain runtime errors, as documented in the guide.

Only disposable stopped-build cache was cleared between verification modes. No dependency was installed and no database reset or destructive cleanup was run. Milestone 06 remains in progress: broader arithmetic/text/JSON and conflict expressions, expression/partial-index conflict targets, generalized joins, transaction-preserving composed locks and multiple per-input lock clauses remain required. Model lifecycle, later framework milestones and the final framework-wide audit are still outstanding.

### Milestone 06 typed calculation evidence

Implemented [typed arithmetic and text calculations](../docs/guides/scalar-calculations.md) through one closed operation node in the shared expression AST. Numeric operations retain compatible concrete types and codecs, with explicit exact-decimal and approximate-float conversions. Text transformations return ordinary strings rather than claiming to preserve a validated domain. Nullable operations, concrete comparison values, row predicates and selected HAVING predicates retain separate typed contracts.

Computed row ordering now uses the existing model `Order` contract, including normal reads, numbered/simple pages, offset chunks, transaction-scoped reads and direct/through relation loading. Target and pivot expressions share immutable alias qualification. Model cursor and primary-key iteration boundaries reject arbitrary computed identities; declared output fields remain the result-cursor contract. AST traversal owns grouping, named-window and CTE discovery, bounds, correlations and parameter binding.

Verification on 2026-09-12 in the existing Go 1.27.1 VM:

- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make verify` passed all framework and independent consumer checks, including 251 negative-compilation cases, fresh-generation acceptance, current output, formatting, vet and documentation checks. Seventeen new compiler fixtures reject incompatible scopes, values, numeric/text operands, nullable assignments and row/selected phase confusion.
- Focused PostgreSQL calculation and composition tests passed. They cover exact arithmetic beyond float precision, physical smallint/real fields, integer division, nullable calculations, Unicode text, escaped patterns, variadic NULL/empty behavior, computed ordering, complete projection hydration, derived aggregation, grouped/window values, CTE discovery, explicit correlations and target/pivot relation ordering. SQL arithmetic failures remain typed database errors, and invalid narrow results fail the complete collected read.
- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make test-postgres` passed the complete framework and independent consumer PostgreSQL suite with race detection, including the calculation and composition fixtures. Existing joins, relationships, CTEs, sets, conditional values, grouping, windows, pagination, locking and upserts remained green.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all real completion, hover and definition probes in 195.758 seconds. Five new probes retain concrete numeric/string/nullable values, model-owned row predicates, selected HAVING predicates and computed model orders, resolving definitions into the installed framework source from the consumer workspace.

An initial consumer compilation exhausted VM disk space after its negative-compilation tests passed. Once that process stopped, clearing only the disposable Go build cache restored capacity and the full ordinary verification passed. The cache was cleared again before the separate PostgreSQL/race suite. No dependency was installed, and source, generated code, credentials and database data remain intact.

Milestone 06 remains in progress. Typed temporal/JSON operations, generalized expression comparisons, conflict calculations and expression/partial-index conflict targets, generalized joins and transaction-preserving composed locks remain required. Model lifecycle, later framework milestones and the final framework-wide audit are still outstanding.

### Milestone 06 value comparison and join evidence

Implemented [typed value comparisons and join conditions](../docs/guides/value-comparisons-and-joins.md). One binary operand node now serves generated column comparisons, computed row/selected comparisons and ON clauses. Public functions retain operand value types and row/selected phases; join conditions retain distinct left/right ownership. Ordinary NULL comparisons and explicit NULL-aware comparisons preserve their SQL meaning.

`CrossJoin` uses a distinct typed scope, preserving independent source windows, complete result decoders and existing outer nullability. Row-scalar constructors retain inner SELECT phases, existing cardinality errors and explicit correlation contracts. Shared traversal and qualification handle operands containing CTEs, aggregate calculations and nested correlated queries, including relationship predicates and target aliases.

Verification on 2026-09-12 in the existing Go 1.27.1 VM:

- Query compiler tests and fresh-generation acceptance passed. The independent comparison consumer compiled, and all 268 negative-compilation fixtures passed; seventeen new cases cover wrong scopes/values, row/selected confusion, nullable mismatches, invalid ordered/text operands, join-side ownership, cross-scope conversion and scalar-result identity types.
- Focused PostgreSQL comparison and join tests passed. They cover ordinary and NULL-aware row/selected comparisons, computed inner/outer joins, source-window-preserving Cartesian products, inherited nullability, complete-record locking, scalar cardinality failures, CTE discovery, correlated row aggregates and direct/through relationship aliasing.
- Four new real gopls completion/hover/definition probes passed in 14.215 seconds. They inspect binary row comparisons, computed join-side predicates, Cartesian model slices and scalar-row signatures in the consumer workspace.
- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make verify` passed the full framework and independent consumer checks, including all 268 invalid-code cases, fresh generation, formatting, vet, current generated output and documentation validation.
- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make test-postgres` passed the complete framework and independent consumer suite with race detection, including the new comparison fixtures and all existing relations, joins, projections, calculations, CTEs, sets, windows, pagination, locking and upserts.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all real completion, hover and definition probes in 205.067 seconds. The four new probes retain concrete operand types, join-side ownership, model slices and nullable scalar-row signatures in the consumer workspace; existing probes remained green.

The first PostgreSQL run exposed the backend's FULL JOIN planner restriction for a pure inequality. Acceptance now verifies both a supported equality-plus-range condition and the unsupported shape's SQLSTATE `0A000`, isolated with a savepoint. The guide and public FullJoin documentation explain this limitation; no condition is silently replaced. Compiler-checked Go contracts do not imply that every predicate can be executed by every database planner.

Review preserved one binary comparison node and operator table for existing column predicates and the new public functions. Both operands participate in validation, grouping discovery, CTE discovery and immutable correlation qualification. Cartesian joins reuse the existing source boundaries and result decoders. Only the disposable Go build cache was cleared between stopped verification modes; no dependencies were installed and no database data was reset or destroyed.

Milestone 06 remains in progress. Lateral joins, typed temporal/JSON operations, conflict calculations and expression/partial-index conflict targets, and transaction-preserving composed locks remain required. Model lifecycle, later framework milestones and the final framework-wide audit are still outstanding.

### Milestone 06 lateral join evidence

Implemented [typed lateral joins](../docs/guides/lateral-joins.md) with complete correlated model/record selections, generated fluent report selectors and explicit outer ownership. Inner/left/cross forms preserve per-parent filters, ordering, limits and offsets, optional typed ON conditions, output multiplicity and null extension. Correlated record/source types cannot execute independently or enter ordinary aliases, CTEs or joins.

The SELECT compiler captures only declared preceding sources for lateral evaluation, before grouping at the containing level. Ordinary derived inputs retain isolated scopes. Existing traversal, correlation qualification, generated mapping/decoding and result execution remain the shared implementation.

Verification in the existing Go 1.27.1 VM:

- Focused query tests and fresh-generation acceptance passed; independent lateral consumer compilation passed. Fresh generation exercises both correlated fluent reports and complete model selection.
- Focused PostgreSQL acceptance passed for per-parent windows, complete model decoding, empty aggregate results/HAVING, windows, optional ON conditions, inherited nullability, CTE discovery, result CTEs/sets, pagination and explicit transaction locks.
- All 283 negative-compilation fixtures passed in the full consumer run. Fifteen new cases reject wrong outer scopes, ordinary join/source conversion, standalone correlated execution, nullable record erasure, incorrect ON ownership and invalid generated projection values/scopes.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all real completion, hover and definition probes in 216.995 seconds. Four new probes inspect correlated record selection, retained lateral ownership, generated fluent report selection and typed HAVING in the consumer workspace.
- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make verify` passed the complete framework and independent consumer checks, including fresh generation, generated-name collision rejection, vet, formatting, current generated output and documentation validation.
- The full PostgreSQL/race command passed every framework package and all consumer packages through calculations and set operations, including the lateral fixtures. It then exhausted VM disk space while building the final upsert/window packages. The stopped build released its temporary space. The remaining upsert/window packages then passed separately with the same PostgreSQL/race settings (`-race -count=1 -timeout=2m`) in 1.120 and 1.136 seconds respectively. Every framework and consumer package therefore has passing PostgreSQL/race evidence; the original combined command itself ended with a build-resource failure.

Review retained a single projection emitter for ordinary and correlated selectors, common record mapping/decoding, and existing immutable query derivation. Compiler checks enforce preceding-source visibility before adding the lateral alias. Qualification tests verify that renamed correlation operands and requirements do not mutate their originals. Only disposable build cache was cleared between stopped verification modes; no dependency was installed or database reset performed.

Milestone 06 remains in progress. Typed temporal/JSON operations, conflict calculations and expression/partial-index targets, and transaction-preserving composed locks remain required. Later framework milestones and the final framework-wide audit are outstanding.

### Milestone 06 temporal calculation evidence

Implemented [typed temporal calculations](../docs/guides/temporal-calculations.md) through the existing scalar operation AST: calendar/clock extraction, truncation, explicit timezone conversion and local resolution, calendar versus elapsed arithmetic, exact Unix-millisecond conversion and transaction time. Date, wall-clock and instant types retain their distinct Go contracts. Nullable and selected-expression forms preserve SQL NULL, aggregates, windows and source ownership.

Rust references were the implemented `Sql::date_trunc`, `extract`, `to_timestamp_millis` and `now` helpers in `src/database/query.rs` and their compiler metadata. Go uses closed typed operations, explicit timezone/resolution values and exact integer epoch handling; consumers do not provide SQL function names or raw fragments. Interval literals reuse the existing `temporal.Interval` representation and its separate calendar/elapsed components.

Verification in the existing Go 1.27.1 VM:

- Query compiler tests passed for invalid units/zones/policies/precision, invalid representations and arity, zero-limit validation, immutable qualification, bound parameters and selected aggregate/window phase checks.
- Focused PostgreSQL temporal acceptance passed in 0.116 seconds. It covers every extraction family, exact epoch boundaries near years 1/9999, negative fractional milliseconds, timezone-independent truncation, DST gaps/overlaps, leap-month clipping, 23-hour calendar days versus 24 elapsed hours, clock wrapping, nullable results, CTEs, correlations, windows, transaction-stable time and failure recovery without partial collected results.
- `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make verify` passed all framework and independent consumer checks, fresh generation, vet, formatting, current generated output and documentation validation. All 297 negative-compilation cases passed; fourteen new cases reject temporal type/unit/zone/interval errors, mixed ownership, nullable mismatches and row/selected phase confusion.
- `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed every real completion, hover and definition probe in 251.807 seconds. Three new probes confirm typed instant/zone/unit inputs, calendar extraction results and selected temporal expressions in the actual consumer workspace.
- Required PostgreSQL/race verification passed every package returned by `go list ./...` in the framework (33 packages) and independent consumer (35 packages). Packages ran in batches of six using `-mod=readonly -race -count=1 -timeout=2m`, with the same required-database environment as `make test-postgres`. Batching limited retained temporary build files; the new temporal consumer passed in 1.197 seconds, and all existing query/consumer suites remained green.

Review retained the shared expression traversal, codecs and generated model/projection contracts. Integer milliseconds are split into whole days and a bounded remainder before conversion, avoiding floating-point conversion of the whole epoch. Local resolution explicitly selects PostgreSQL behavior and does not silently change the stricter application temporal API. Public comments distinguish fractional seconds from the millisecond/microsecond component returned by PostgreSQL. Only disposable Go build cache was cleared between stopped verification modes; no dependencies were installed or database data reset.

Milestone 06 remains in progress. Interval-valued model codecs and result expressions, typed JSON operations, conflict calculations and expression/partial-index targets, and transaction-preserving composed locks remain required. Later framework milestones and the final framework-wide audit are outstanding.

### Milestone 06 interval query evidence

Implemented [typed interval queries](../docs/guides/interval-queries.md): model/projection codecs, generated interval fields and drafts, nullable SUM/AVG, interval arithmetic/components, timestamp and clock differences, and dynamic temporal shifts. Existing query ownership, result decoders, aggregate/window phases and parameterized compilation remain shared.

PostgreSQL compares intervals using 30-day months and 24-hour days. Direct, through and aggregate eager-loading keys now honor that equality without changing stored model components. Exact canonical keys use a sufficiently wide representation; cursor transport retains the actual field value and existing primary-key tie-breaking.

Focused verification in the existing Go 1.27.1 VM passed all four PostgreSQL output styles, integer/duration bounds, zero/NULL/omitted mutations, stored decode failures, normalized differences, DST/calendar behavior, interval summaries/windows, natural-key lookup/upsert/delete, relation matching and cursor ties. Fresh generation passed from handwritten declarations. Ten new negative-compilation cases passed. Two 10-second fuzz runs completed 804,168 arbitrary-text parses and 750,449 canonical round trips without failure.

Review replaced repeated epoch SQL expansion with exact integer-millisecond interval parsing. The previous temporal acceptance still passes, including extreme years and window composition; an unrepresentable maximum-int64 epoch now reports PostgreSQL interval-overflow SQLSTATE `22015`. Scalar compilation now carries an expansion-work budget through expression planning as well as final SQL generation.

`GOMEMLIMIT=1536MiB GOFLAGS=-p=1 make verify` passed the complete framework and independent consumer checks, including all 307 negative-compilation fixtures, fresh generation, vet, formatting, current output and documentation validation. Calendar arithmetic overflow is verified as SQLSTATE `22008`; elapsed results beyond Go's duration range fail decoding without aborting an otherwise valid SQL transaction. The focused interval and existing temporal PostgreSQL suites passed in 0.138 and 0.113 seconds after review.

`FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed all real consumer completion, hover and definition probes in 263.407 seconds. Three new probes confirm interval field summaries, explicit-zone dynamic shifts and nullable selected interval inputs.

Required PostgreSQL/race verification passed every package returned by `go list ./...` in the framework (33 packages) and independent consumer (36 packages). Six-package batches used `-mod=readonly -race -count=1 -timeout=2m` and the required private test-database environment. The interval consumer passed in 1.260 seconds; existing temporal, relation, projection, comparison, cursor, locking, set and window suites also passed. Only disposable Go build cache was cleared between stopped verification modes; no dependency was installed and no database was reset.

Consumer review confirms that handwritten interval fields produce concrete generated setters, predicates, summaries and result types. Shared codecs and aggregate construction own validation and decoding; ordinary model values retain their calendar/elapsed components. Milestone 06 remains in progress; typed JSON operations, conflict calculations/index targets and transaction-preserving composed locks are still required. Later framework milestones and the final framework-wide audit remain outstanding.

### Milestone 06 JSON value and field evidence

Implemented [typed JSON models](../docs/guides/json-models.md) with immutable `value.JSON[Payload]` snapshots, generated model/projection fields and drafts, JSONB codecs, whole-document predicates, containment and JSON-kind expressions. Handwritten payload types own the schema. Optional properties, SQL NULL, JSON null and invalid zero values remain distinct; ordinary reads return concrete models and native slices.

Strict parsing bounds bytes, depth, nodes and numeric expansion. Shape checks cover required/unknown properties, exact property names, fixed arrays, scalar ranges, custom JSON/text codecs and canonical typed map keys. Review reproduced and fixed a panic involving promoted marker methods on pointer wrappers, and added concurrent decoding coverage. Quoted scalar fields cannot hide composite or null values. Runtime errors omit payload contents, and failed scans/unmarshals preserve their destinations.

`GOMEMLIMIT=1536MiB GOFLAGS=-p=1 make verify` passed the complete framework and consumer checks after these fixes, including all 317 negative-compilation fixtures, fresh JSON generation, reproducibility/current-output checks, vet, formatting and documentation validation. Ten new compiler-failure cases preserve JSON payload types, model owners, nullability, query phases and field capabilities. A 10-second canonical JSON fuzz run completed 43,158 executions without failure.

Focused PostgreSQL acceptance passed typed writes, omitted/cleared updates, JSON null versus SQL NULL, array containment versus equality, complete projections, exact decimals, natural-key matching, eager relations, CTEs and explicit correlated values. Invalid stored payloads discard collected results; invalid parameters fail before execution. The post-pointer-fix focused run passed in 0.075 seconds. `GOMEMLIMIT=384MiB GOFLAGS=-p=1 make test-postgres` then passed every framework and consumer package with race detection; the JSON consumer passed in 1.129 seconds.

`FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed every real consumer completion, hover and definition probe in 219.182 seconds. Three new probes verify generated JSON field operators, nullable JSON-kind results and selected JSON inputs using gopls 0.23.0.

Two unbatched consumer verification attempts exhausted the 20 GiB VM disk as caches accumulated; neither reported an assertion failure before running out of space. Verification now discovers every package and runs bounded batches, using the Makefile's batch-size setting for ordinary and required PostgreSQL/race gates. Runner tests verify complete coverage, preserved test flags and failed/empty discovery rejection. Clearing only stopped-build cache and using batches allowed the full ordinary gate to pass. The first PostgreSQL/race run at a 1536 MiB Go memory target had its compiler killed while building an existing comparison fixture. That fixture passed at 384 MiB, followed by the full gate at the same setting. Source, credentials and database data were retained; no dependency was installed.

Milestone 06 remains in progress: generated JSON property/array paths, conflict calculations/index targets and transaction-preserving composed locks remain required. Model lifecycle and all later framework milestones, including the final framework-wide audit, remain outstanding.

### Milestone 06 generated JSON path evidence

Implemented [generated JSON properties](../docs/guides/json-models.md#typed-properties-arrays-and-maps) with `Properties()` for ordinary payload structs and typed `At` methods for arrays/maps. Nested and recursive declarations retain the query owner, map key type and concrete payload. Nullable scalar extraction supplies typed text/ordered filters; snapshot, existence and kind operations keep missing values and JSON null explicit. Quoted scalar tags decode to their logical Go value before inspection. All operations reuse the shared AST/compiler and bound parameters.

Runtime validation and source generation now share bounded field/tag promotion in `internal/jsonshape`. Fresh discovery retains custom serializer signatures without requiring generated code in their bodies; custom shapes stay opaque and framework enum scalar codecs retain validation. Metadata enumeration checks its budget before reading fields. The installed Go 1.27.1 toolchain selects the newer JSON implementation by default; malformed tag names are rejected rather than relying on compatibility behavior that can produce a partial property name.

Focused framework JSON checks, deterministic fresh generation and required PostgreSQL consumer acceptance passed. The PostgreSQL run passed in 0.112 seconds, covering recursive nested access, typed integer/string map keys, negative/out-of-range array indices, exact decimals, quoted numbers/strings, SQL NULL versus JSON null, child snapshot decoding, CTE/alias scopes and correlated values. Eight new negative-compilation cases passed, protecting owner, scalar, map-key, array-index, property-name, payload and query-phase contracts.

Review reproduced a generated receiver shadowing a legitimate payload type named `f`. Generated receivers and key parameters now use the existing local-name allocator after resolving imports; a fresh-checkout regression with `f`, `p`, `field`, `path` and `key` types passes. Further regressions reproduced flat/nested field-name collisions and a public path type disappearing after an unrelated field was added. Escaped path components now preserve field boundaries, and recursive reuse is limited to ancestors. Fresh and repeated generation, array/map entry naming and the added-field regression pass.

`GOMEMLIMIT=1536MiB GOFLAGS=-p=1 make verify` passed after the naming review, including the full framework and consumer checks, all 325 compiler-rejection cases, current generated output, formatting, vet and documentation validation. `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" GOMEMLIMIT=768MiB GOFLAGS=-p=1 make agent-smoke` passed every completion, hover and definition probe in 258.171 seconds. The three new probes cover generated properties, typed map entries and nullable text filters.

`GOMEMLIMIT=384MiB GOFLAGS=-p=1 make test-postgres` passed after the naming review, covering every discovered framework and independent consumer package with required PostgreSQL and race detection. The JSON consumer passed in 1.171 seconds; all existing consumer suites remained green. This completes the generated JSON path verification gate. Conflict calculations/index targets and transaction-preserving composed locks remain milestone 06 work. Later framework milestones and the final whole-framework audit remain outstanding.

### Milestone 06 conflict calculation evidence

Implemented [typed conflict calculations](../docs/guides/model-upserts.md#calculating-conflict-updates) through `Query.ConflictRows`, generated `FieldsAt` accessors, `SetConflictValue` and `WhereRows`. Stored/proposed records share `ConflictRow[Model]`, distinct from ordinary model queries. Destinations require a generated field and its exact value type, including nullability. Independent and explicitly correlated scalar queries preserve their inner SELECT phase. Assignment dependencies use the existing CTE planner, alias allocator, expression traversal and transaction execution; no parallel SQL compiler or generator schema was added.

The full query suite passed with race detection in 1.281 seconds using `GOMAXPROCS=1 GOGC=25 GOMEMLIMIT=256MiB GOFLAGS=-p=1`. The final independent upsert consumer passed required PostgreSQL/race checks in 1.238 seconds, covering exact decimal calculations, database defaults and BEFORE INSERT trigger changes, conditional skipping, batch behavior, nullable clearing/fallback, scalar/CTE/correlated assignments, cardinality rollback and a physical table named `excluded`.

Initial compilation of the large framework test package exceeded the VM's memory limit. Assignment construction now uses a typed package function rather than adding another scope to every field's method set; a single compiler worker allowed the complete query suite to pass. A focused compiler-rejection run then exposed cold-cache timeouts leaving compiler descendants running. The fixture harness now cancels its owned POSIX process group and includes a descendant-pipe cleanup regression. Only identified test-owned processes and disposable build cache were cleaned; source, credentials and database data were retained.

`GOMAXPROCS=1 GOGC=50 GOMEMLIMIT=384MiB GOFLAGS=-p=1 make verify` passed the full framework and independent consumer checks, including all 335 compiler-rejection cases, the descendant cleanup regression, current generated output, formatting, vet and documentation validation. Ten new rejection cases cover destination, model/value ownership, nullability and row/selected phases. The first full run exposed a mismatched expected compiler diagnostic; the invalid destination was correctly rejected. The corrected expectation passed in the subsequent full run.

The three new real-gopls probes passed completion, hover and definition checks in 13.512 seconds using the existing approved executable and the independent consumer workspace. They cover conflict row scopes, typed assignment construction and the combined update condition. The fluent condition probe now anchors its actual consumer call. Consumer review confirms generated typed fields for both stored/proposed rows, concrete destination/value types and ordinary upsert terminals. Expression/partial-index conflict targets and transaction-preserving composed locks remain milestone 06 work. Later framework milestones and the final whole-framework audit remain outstanding.

### Milestone 06 expression and partial-index target implementation

Implemented [typed index targets](../docs/guides/model-upserts.md#expression-and-partial-index-targets) with `OnConflictKeys` and `TargetWhere`. The existing row keys, expression validation and compiler own model scope, scalar operation rendering and predicate structure. A private schema-literal mode renders index constants and unqualified index columns while preserving ordinary execution bindings. Validation rejects subqueries (including those hidden inside CASE), selected aggregates/windows, duplicate keys, foreign declarations and invalid named/catch-all partial targets. Typed nil field descriptors now fail validation without panicking.

Focused compiler checks passed in 0.003 seconds, covering immutable derivation, target/update parameter separation, invalid and empty-batch targets, escaping and resource bounds. Required PostgreSQL literal round trips passed in 0.021 seconds across both string-literal settings, including signed integer limits, float extremes, quoted UTF-8, nil/empty bytes and timestamps with offset seconds. The complete upsert consumer passed in 0.158 seconds, including generic plans, expression/composite/partial targets, quoted constants in both index expressions and predicates, independent inserts outside the partial predicate, update-condition skipping, unmatched-index rollback and all earlier upsert behavior. An initial acceptance failure was traced to an extra escaped backslash in the handwritten test migration; correcting that physical index made it match the intended typed declaration. Five new compiler-rejection cases passed in 0.478 seconds.

The first full repository gate passed all framework packages and all 340 compiler-rejection cases, then exhausted VM disk space while building a consumer expression package. The identified verification process tree was stopped. Cleanup removed only stopped temporary builds containing Foundry-Go import metadata and disposable Go build cache, recovering 15 GiB without changing source, credentials, dependencies or database data. The complete rerun passed with three-package batches and one compiler worker: every framework and consumer package, all 340 compiler-rejection cases, current generated output and repository documentation checks.

Two new real-gopls probes passed completion, hover and definition checks in 9.055 seconds for typed index keys and partial-index predicates. Review also found that the Make package runner continued after a failed batch. A regression reproduced the failure before the fix; the corrected runner and shared batch tests passed in 0.011 seconds. A later full verification session no longer has an available result and is not claimed as additional evidence.

The required PostgreSQL/race run initially lost its query compiler to the VM's memory limit before executing tests. Retrying with inlining disabled specifically for `github.com/weiloon1234/Foundry-Go/database/query` (`-gcflags=github.com/weiloon1234/Foundry-Go/database/query=-l`) passed the entire query package in 1.353 seconds and the entire upsert consumer in 1.267 seconds. Both retained race detection, real PostgreSQL, one compiler worker and the existing 256 MiB Go memory target. These are complete query/upsert package checks, not a claim that every framework package's PostgreSQL/race gate was rerun for this slice. Index-target verification is complete. Transaction-preserving composed locks remain milestone 06 work; later milestones and the final framework-wide audit remain outstanding.

### Milestone 06 per-input lock clause evidence

Implemented `RowLock[Scope]` descriptors and projection/value `LockRows`, retaining the existing transaction-only `LockedResult` terminals and shared lock validation. Each clause has its own preserved input targets, strength and wait policy. Deriving an all-clause wait modifier copies the clause list. Targets are copied on construction, and explicit owner conversions fail because the descriptor retains its owner in its representation. Combined query/clause validation work is bounded. Empty, nil, repeated, unknown and nullable targets fail before SQL.

Initial focused compiler tests passed in 0.003 seconds; the entire row-locking PostgreSQL consumer passed in 0.357 seconds, including independent table policies and overlapping strength/wait precedence. Six new compiler-rejection cases passed in 0.583 seconds. Three real-gopls probes passed completion, hover and definition in 12.081 seconds for scope-owned clauses, independent policies and transaction-required terminals.

Manual review then found a pre-existing validation gap: a window wrapped in a calculation could pass the lock validator. A regression reproduced that failure before the fix. The validator now uses the shared bounded expression walker to inspect nested windows while keeping scalar subqueries in their independent SELECT phase. All focused lock compiler tests passed again in 0.003 seconds, including CASE conditions, computed ordering and an allowed independent scalar-window query. A consumer regression additionally checks that pre-SQL rejection leaves the transaction usable.

The final gate used required PostgreSQL: complete `make verify`, followed by the full query and lock-consumer packages with race detection. Query-package inlining remained disabled for the VM's memory limit. Disposable compiler cache was cleared before this run to keep sufficient space for complete fixture coverage. Output and final exit status are retained under ignored `.cache/lock-clauses-verification.*`. No new dependency, environment replacement or database cleanup was performed.

The first final-gate attempt stopped in generator rollback tests: the temporary log runner had propagated a restrictive creation mask, while four manually constructed fixture plans assumed mode `0644`. The generator correctly detected the metadata mismatch. Fixture plans now snapshot actual files through the existing reader, permission-change scenarios explicitly change the observed mode, and rollback/recovery assertions verify restored permissions. The four focused families passed under both `022` and `077` masks in 0.017 seconds each. The runner now creates only its log with private permissions and retains the caller's process mask. Production generation behavior was unchanged. The failed gate is archived separately.

The complete rerun passed every framework and consumer package, all 346 compiler-rejection cases, generated-output checks and documentation validation. The generator suite passed in 184.776 seconds and consumer root suite in 33.547 seconds. Required PostgreSQL/race then passed the entire query package in 1.356 seconds and the lock consumer in 1.624 seconds. The retained final exit status is zero. Consumer review confirms independent table policies with concrete scopes and transaction-required terminals. This completes per-input clause verification; the transaction-composition work below remains within milestone 06.

### Milestone 06 transaction composition in verification

The framework now carries lock clauses on their owning SELECT node, with a private compiler permission check and transaction-required record/CTE/alias/projection wrappers. It reuses the existing CTE planner, alias construction, metadata, mappings and result reader. Inner/left/right/full/cross joins retain transaction ownership through the existing scope mapping. Generated transaction projection factories share variant metadata with ordinary and correlated factories, including symbol collision checks.

Before publication, an isolated overlay library and public probes passed CTE/derived lock placement, dependency ordering, nil-transaction rejection, join scope mapping and immutable ordinary-source checks. Three focused compiler tests passed in 0.002 seconds: ordinary low-level generic reads/projections cannot compile nested locks; outer aggregates retain a CTE's own lock phase; and nested lock validation shares one statement budget. A temporary generated-consumer process lost its result handle, so it is not claimed as successful verification.

After publication, the new independent [transaction consumer](../tests/fixtures/consumer/transactionqueries/) generated its APIs and passed a current-output check. Its first compilation exposed two fixture errors: computed joins require `OnEqual`, and the fixture CTE helper must accept the complete transaction record-source contract. After correction, required PostgreSQL acceptance passed in 0.240 seconds, covering bounded skip-locked claims, generated records/aggregates, Count/Exists locks, nested CTEs and independent join strengths, derived locks, savepoint release/rollback, cancellation and validation before SQL. Expanded join/nullability, compiler-rejection, generation, gopls and full repository checks remain in progress. Scalar/predicate composition and later milestones remain outstanding.

Expanded required PostgreSQL acceptance passed in 0.253 seconds, adding left/right/full/cross joins with generated nullable pair projections. All 18 new compiler-rejection cases passed in 1.699 seconds, and the entire query package passed in 0.107 seconds. One diagnostic expectation was corrected to use the compiler's canonical package name rather than the fixture's import alias. Fresh/repeated/current-output projection generation and invalid declaration/collision tests passed in 68.129 seconds after removing an import-alias assumption from a test assertion. Three real-gopls probes passed completion, hover and definition checks in 15.023 seconds for generated transaction projections, Tx-only terminals and CTE materialization. Recursive consumer generation and final repository/race gates remain pending.

Transaction-required scalar, membership and EXISTS composition was first staged in an isolated `.cache/transaction-value-overlay/`. Separate sealed value/subquery interfaces reuse existing SELECT compilation, codecs and scalar constructors. A shared membership operand replaces the field-only internal node, retaining computed expression traversal, binding order and qualification. A small generated-consumer compilation probe passed in 0.001 seconds. Required PostgreSQL prototype tests passed in 0.110 seconds for computed membership, lock ownership, EXISTS, empty/one/multiple scalar rows, cardinality errors and rollback, nullable scalars and three-valued membership. Five focused compiler regressions passed in 0.002 seconds, including ordinary low-level read/count/exists/scalar bypass rejection, computed membership argument order, immutable qualification and nil operand validation.

Recursive consumer generation for the CTE/join increment updated 20 files, and its current-output and documentation checks passed. The scalar prototype was then published to framework source with canonical PostgreSQL acceptance, 14 additional compiler-rejection cases and three additional gopls probes. Complete verification of the combined transaction composition is now required, including the shared operand change's effects on ordinary queries. Explicit correlation/lateral composition and all milestone 06 completion requirements still need review before closing the milestone. Later milestones and the final framework-wide audit remain outstanding.

The combined canonical PostgreSQL transaction consumer passed in 0.345 seconds, all 32 transaction compiler-rejection cases passed in 17.938 seconds, the entire query package passed in 0.109 seconds and all six transaction real-gopls probes passed in 29.292 seconds. The complete repository gate is now running, followed by required PostgreSQL/race checks for query, transaction composition and existing row-lock consumers. Results are retained in ignored `.cache/transaction-composition-final.*`; no full-gate or final-race success is claimed before that pipeline completes.

That full gate subsequently passed all framework packages, including the complete real-gopls suite in 429.156 seconds and generator suite in 195.436 seconds. Consumer root passed in 37.553 seconds with all 378 compiler-rejection cases; advanced-query, chunk and comparison consumers also passed. The gate then stopped because the VM could not create another temporary build directory. It did not complete remaining consumers or final race checks. The failed log/exit status is archived as `.cache/transaction-composition-attempt-1.*`. After confirming all Go processes had stopped, clearing only the 15 GiB disposable build cache restored 14.65 GiB of free space. Source, downloaded dependencies, installed tools, credentials and database data were retained.

An isolated correlation overlay shares ordinary/transaction scope binding, immutable predicates, outer/inner scope helpers and lateral input construction. It adds non-executable transaction-correlated record/value queries, SELECT-owned lock policies and generated correlated projections. The library, generation/current-output check and public nested-correlation/lateral probe passed; initial required PostgreSQL checks passed in 0.085 seconds for all three lateral kinds, parent/child projections, NULL matches, per-parent locks, skip-locked claims, nested ancestors, cardinality/savepoint errors and invalid lock targets. An expanded nullable-parent probe could not start when the disk filled. Its rerun, compiler guards and generator tests are in progress; the correlation changes remain outside framework source.

After cache recovery, expanded PostgreSQL prototype acceptance passed in 0.108 seconds, including a correlation whose parent is an already-nullable lateral result. Five focused transaction compiler regressions passed in 0.001 seconds, and fresh/repeated/current-output plus generator collision tests passed in 79.456 seconds. The increment was then published to framework source with the [transaction correlation guide](../docs/guides/transaction-correlations.md), 21 additional compiler-rejection cases and five real-gopls probes. Canonical PostgreSQL, ordinary correlation/lateral compatibility, typing, query, generator and gopls checks are running. The full repository gate and final race checks remain required for the combined transaction work; milestone 06 and later milestones are not complete.

Canonical PostgreSQL checks passed for the transaction consumer in 0.436 seconds and the ordinary correlation, nullable correlation and lateral consumers in 0.035, 0.017 and 0.059 seconds. The compiler correctly rejected an invalid lateral ON scope; its fixture expected a different diagnostic phrase. After correcting that assertion, the focused transaction/correlation/lateral compiler-rejection suite passed in 6.855 seconds and the complete query package passed in 0.112 seconds.

Real-gopls verification exposed a separate language-client cleanup failure: the two-second shutdown grace could cancel a completed inspection while the server was still stopping. A protocol regression reproduced a valid response followed by that cancellation. The client now permits a five-second bounded grace and labels shutdown/exit errors; caller deadlines still interrupt cleanup. Slow shutdown, unresponsive shutdown/exit and caller-cancellation regressions passed with the existing inspection tests in 13.460 seconds. All five new real-gopls probes then passed completion, hover and definition checks in 23.766 seconds. Current generation and documentation checks passed. Logs and terminal exit statuses are retained in ignored `.cache/transaction-correlation-canonical.*`, `.cache/transaction-correlation-targeted-rerun.*` and `.cache/transaction-correlation-tooling.*`; the first two record the corrected failures, and the final tooling run exited zero.

The [milestone completion review](06-relations-and-advanced-queries.md#completion-review) now maps its requirements to concrete independent consumer fixtures and records later lifecycle/extension integration gates explicitly. The complete repository and final race gates remain required for the combined transaction and language-client changes. No milestone completion is claimed from these targeted checks.

The complete repository gate subsequently passed on Go 1.27.1 with required PostgreSQL and the real gopls executable selected. `make TEST_PACKAGE_BATCH_SIZE=3 verify` covered every framework and independent consumer package, all 399 compiler-rejection cases, current generated output and documentation validation. The language-tooling suite passed in 473.560 seconds, generation tests in 200.406 seconds, consumer root in 55.072 seconds and the complete transaction consumer in 0.416 seconds. The normal gate took 2705.3 seconds using one compiler worker, a 256 MiB Go memory target and query-package inlining disabled. This is a verification-environment measurement, not application performance evidence.

The sequential run is retained in ignored `.cache/transaction-composition-complete.log`; its complete repository phase exited zero. Final query/language-client and transaction/correlation/locking race checks are now running. Before that phase, clearing only stopped disposable build cache restored 14.64 GiB of free space. No whole-pipeline success or milestone completion is claimed until the final race checks finish.

### Milestone 06 completion evidence

The full sequential verification finished with exit zero. After the complete repository gate above, race detection passed the query package in 1.360 seconds and language-client protocol/process tests in 20.666 seconds. Required PostgreSQL/race passed the complete transaction, row-lock, ordinary correlation, nullable correlation and lateral consumers in 1.757, 1.609, 1.064, 1.038 and 1.099 seconds. The final consumer race phase took 420.6 seconds and left 12.69 GiB free. The terminal result is retained in ignored `.cache/transaction-composition-complete.exit`.

The [consumer review and completion checklist](06-relations-and-advanced-queries.md#completion-review) establish the delivered milestone boundary: ordinary model calls retain generated fields and native slices; declared projections remain complete result types; advanced scopes preserve ownership and nullability; locks cannot enter unrestricted execution paths. The implementation shares its SQL AST, compiler, codecs, result readers and scope helpers. Guides were corrected where historical wording still described delivered cursor and expression capabilities as future work.

Milestone 06 is complete. Automatic lifecycle behavior, soft-delete relation integration and the remaining bulk-mutation source APIs are required in milestone 07; the database parity follow-ups below remain required in their named milestones. Milestones 07–24 and the final whole-framework verification/audit are outstanding. This milestone completion does not complete the active framework goal.

### Milestone 07 automatic field mutator increment

`Mutate<Field>(FieldType) (FieldType, error)` now connects handwritten value-receiver methods to generated field metadata. Nullable fields pass only present scalar values to their method; omitted fields and database defaults are preserved. The generator checks method signatures on fresh checkouts and rejects unknown persisted fields, wrong types and pointer receivers before publication. Non-model helper methods remain part of the complete overlay and may reference generated drafts.

Normal create/update, explicit bulk/upsert draft inputs and literal conflict assignments share the same immutable assignment transformation and SQL compiler. Transformations run once inside the write transaction, before final codec validation. Bulk operations retain their separate observer semantics. The destination's own proposed value is already normalized; other SQL conflict expressions targeting a field with a Go mutator are rejected before execution. No lifecycle hook, read accessor, change-set, soft-delete, outbox or audit implementation is claimed by this increment.

The targeted sequence in ignored `.cache/milestone07-mutators-targeted.log` exited zero: query tests passed in 0.111 seconds, required PostgreSQL mutator acceptance in 0.027 seconds, the three new compiler-rejection cases in 0.298 seconds, and both new real-gopls completion/hover/definition probes in 10.527 seconds. Fresh/repeated/current generator tests and nine invalid-signature cases passed separately in 4.742 seconds. Recursive generated output and documentation checks passed. The [consumer guide](../docs/guides/model-mutators.md) states the delivered same-type normalization contract and remaining lifecycle/password-input work.

Review additionally bounded field lookup to linear work, preserved all participating value-type checks before user code, and added coverage for non-model helper signatures, normalized uniqueness and mixed upsert batches. The first complete run passed the core packages and all real-gopls probes (481.027 seconds), then was intentionally interrupted during generator tests: comparison with Rust's `apply_assignment_write_mutators` exposed a conflict-literal/computed-expression bypass. Regression tests reproduced both cases before the fix. This interruption is not a successful complete verification.

Conflict literals now share model assignment capture and transformation, including exact nullable types. Computed assignments to mutated fields are rejected; a field's own proposed value remains supported. The complete canonical query suite, including these regressions, passed in 0.065 seconds. Expanded required PostgreSQL mutator and existing upsert consumers passed in 0.036 and 0.161 seconds. Fresh/invalid mutator generation passed in 5.773 seconds, the three compiler-rejection cases in 0.299 seconds, and three real-gopls probes (draft, automatic method and nullable conflict assignment) in 15.375 seconds. Recursive generated output and documentation validation passed; the complete targeted pipeline exited zero, recorded in ignored `.cache/milestone07-mutators-conflict-check.exit`. The final complete repository/race checks remain outstanding. Milestone 07 and the full framework goal remain in progress.

The 4 GiB verification VM killed the query test compiler while it emitted runtime type/debug metadata. The canonical query suite passed with query-package inlining and DWARF generation disabled (`-gcflags=github.com/weiloon1234/Foundry-Go/database/query=-l -dwarf=false`), alongside the existing one-worker and memory-target settings. This is a test-build accommodation: compiler contract checks and runtime assertions remain enabled, but an unmodified default build of the complete query test package has not passed in this VM. Diagnostic overlays were not used in the passing run, and no VM, service or dependency changes were made.

The next complete attempt passed root packages, the full real-gopls suite (480.650 seconds), generation (243.040 seconds) and all 402 then-current compiler-rejection cases (40.087 seconds). It exited 2 after 993.1 seconds when the VM killed the `comparisonqueries_test` compiler during consumer verification; subsequent consumers and the final race phases did not finish. This is not a successful complete gate. The canonical comparison fixture subsequently passed in 0.115 seconds with the same inlining/DWARF accommodation applied to consumer packages as well (`-gcflags=foundry.test/consumer/...=-l -dwarf=false`). This targeted success does not complete the interrupted full gate.

### Milestone 07 typed field-change primitive

`database/lifecycle` now owns `FieldChange[T]` and `CompareField`, retaining typed before/after values and independent assigned/changed states. Comparison validates codec representations, distinguishes an absent model from a NULL field, and preserves interval calendar components instead of applying relation-key SQL equivalence. Routine formatting omits values. The [guide](../docs/guides/model-changes.md) states the explicit primitive boundary and custom-value copy semantics; automatic model-wide capture and hooks are not delivered by it.

The initial semantic race tests passed in 1.015 seconds, focused vet passed, and the required PostgreSQL mutator consumer passed in 0.047 seconds. Its new change test uses actual generated writes to verify normalized equality, NULL transitions and database defaults without invoking mutators again. Three compiler-rejection cases for incompatible values, NULL wrappers and model-owned IDs passed in 0.230 seconds, bringing the suite to 405 cases. The targeted pipeline exited zero, recorded in ignored `.cache/milestone07-field-changes-targeted.exit`.

Review added custom-codec tests for nil versus empty byte buffers, mixed driver representations and non-finite numbers. The expanded race suite passed in 1.012 seconds, vet passed, and real-gopls completion/hover/definition probes for a field change and its nullable snapshot passed in 9.180 seconds. Documentation validation passed; ignored `.cache/milestone07-field-changes-review.exit` records exit zero. The full gate remains required after lifecycle integration, and milestone 07 remains in progress.

### Milestone 07 generated model changes

The generator now emits model-specific change sets, field sets and comparison functions through the shared lifecycle comparator. For example, `CompareUser` accepts optional stored `User` snapshots and an effective `UserDraft`; `UserChanges.Fields().Email` retains `FieldChange[string]`. Nested field sets support persisted names such as Before/After without colliding with snapshot methods. Generated snapshots copy persisted values only, excluding ignored state and relation loads/aggregates. Existing-model comparisons reject changed or assigned primary keys. Failed comparisons publish no partial data.

Fresh-checkout/repeated/current generation and symbol-collision preservation passed in 10.277 seconds. The first fresh-checkout check exposed a missing generated lifecycle import in dependency discovery; the shared graph's generated-dependency list now includes it. Recursive generation published 24 updated owned files in 45.2 seconds. The model-field extraction race test passed with the lifecycle suite in 1.009 seconds.

The independent PostgreSQL mutator/change consumer passed in 0.049 seconds; model-wide snapshot/identity/failure behavior passed in 0.002 seconds. Five new compiler-rejection cases passed in 0.505 seconds, rejecting wrong model snapshots, wrong drafts, wrong nullable results, misspelled fields and ignored fields. Four real-gopls probes passed in 20.486 seconds. The complete generator suite passed in 254.127 seconds, followed by current recursive output and documentation checks. Ignored `.cache/milestone07-model-changes-targeted.exit` records exit zero.

Focused vet passed for lifecycle/generator code and generated models/mutator consumers. Race detection passed the independent model-change tests in 1.008 seconds and the required PostgreSQL mutator/change consumer in 1.082 seconds. Documentation validation passed; ignored `.cache/milestone07-model-changes-review.exit` records exit zero. Consumer review confirms concrete models, drafts and field results without field-name strings, snapshot privacy, and an explicit comparison boundary. Automatic write capture and before/after hooks are still required, and the full milestone/repository gate is not complete.

### Milestone 07 declared model hooks and automatic write capture

The [model hooks guide](../docs/guides/model-hooks.md) defines the explicit `hooks=Factory` annotation and generated `<Model>Hooks` callbacks. Factories run once per normal write inside the transaction; reads and set-based writes do not invoke them. The existing AST, hydration, transaction/savepoint and outcome pipeline owns bounded row locking, before-hook draft reconstruction, once-per-assignment mutators, automatic typed changes, post-write veto and after-commit registration. Indexed mutation inputs separate typed draft reconstruction from assignment-presence inspection, leaving future distinct input/storage transforms possible.

Fresh/repeated/current-output hook generation and rejection of invalid factories/callbacks passed in 12.553 seconds. Recursive generation updated 26 owned files. Its first attempt correctly rejected publication when a consumer package was added during the run; the completed fixture set was regenerated successfully. The canonical query suite passed in 0.108 seconds. Driver tests for factory panic/Goexit, bounded ambiguous/missing/malformed locked snapshots, closed rows, unknown commits and post-commit reconciliation passed in 0.001 seconds.

Required PostgreSQL hook acceptance passed in 0.223 seconds and existing mutator regressions in 0.042 seconds. Tests cover a typed default introducer lookup on the supplied transaction, generated UUID/hook/default assignment tracking, normalized unchanged values, NULL, empty-patch completion, exact callback order, per-write factory state, rollback of related model writes, panic/Goexit, cancellation, recursion bounds, parent-savepoint rollback, scoped/missing rows and bulk/upsert skipping. After-commit failure preserves committed data. One initial recursion assertion inspected the redacted outer database message; it now verifies the retained typed framework diagnostic through `errors.As`.

Focused driver and query hook checks passed with race detection in 1.016 and 1.014 seconds; the required PostgreSQL hook consumer passed with race detection in 1.331 seconds. Four new compiler-rejection cases passed in 0.383 seconds, rejecting another model's draft/snapshot/change set and a misspelled callback. Two real-gopls completion/hover/definition probes passed in 10.282 seconds for hook drafts and callback signatures. Documentation links and fixture Go requirements passed. These runs use the constrained VM compiler profile already recorded above; they do not establish acceptable default-flag build resources.

The first full-gate attempt passed formatting/vet and core package tests before being deliberately stopped during language-tooling acceptance: the VM had only 0.73 GiB free and a 14 GiB Go build cache. The exact verification script and descendants were stopped, absence of live build tools was verified, and only the disposable Go build cache was cleared, restoring 14.6 GiB free. Source, installed tools, downloaded dependencies and database data were retained. The stopped attempt is not a passing gate.

The subsequent full `make TEST_PACKAGE_BATCH_SIZE=3 verify` passed in 1859.9 seconds on Go 1.27.1 with required PostgreSQL and actual gopls enabled. Every core/consumer package passed, including all 414 compiler-rejection cases, the previous comparison-fixture resource failure, full gopls acceptance (503.993 seconds), the complete generator suite (268.212 seconds), current recursive generation, formatting/vet and documentation checks. The full query race suite then passed in 1.374 seconds, followed by required PostgreSQL hook and mutator consumers with race detection in 1.313 and 1.076 seconds. The VM retained 6.50 GiB free. This verifies the declared-hook increment with the documented constrained compiler profile; default-flag consumer resource acceptance remains a separate production-hardening requirement.

This is not milestone 07 completion. Retrieval/read accessors, application/plugin observer registration, soft deletion/restoration/force deletion, distinct plaintext/hash inputs, hook-aware bulk and relation/source-mutation integrations, events/outbox and audit remain required, as do later milestones and the final whole-framework audit.

### Milestone 07 observer construction support

Source review established that composing complete per-observer hook pipelines would interleave `Saving` and operation-specific callbacks incorrectly. The [observer integration contract](07-model-lifecycle-events-and-audit.md#observer-registration-integration) now requires shared typed drafts/changes, ordering by stage, construction-time dependencies, immutable database ownership and propagation through sessions/transactions/savepoints.

An isolated foundation prototype added explicit `ResolveAll[T]` using the existing sealed resolver and service construction graph. Existing and new foundation race tests passed in 1.075 seconds, followed by vet. The reviewed addition is now applied to canonical `foundation`, with an independent consumer for two providers and typed contribution results. The initial canonical foundation/testkit and consumer race checks, a result-type compiler rejection, a real-gopls probe and documentation validation passed.

Follow-up review reproduced a pre-existing constructor-exit cleanup gap: `runtime.Goexit` escaped `Build` and left the captured resolver able to resolve single services and contribution lists. Shared constructor invocation now uses the existing callback-isolation helper and deferred resolver sealing. The complete foundation/testkit race suites, constructor-exit regression and vet passed after the fix; the independent contribution consumer passed with race detection in 1.012 seconds, the result-type compiler rejection in 0.057 seconds, and the real-gopls completion/hover/definition probe in 6.202 seconds. Documentation validation also passed. The compiler-rejection catalog now contains 415 cases; the new case passed independently, while the earlier full gate covered its then-current 414 cases.

This addition does not yet supply observer declarations, database binding or multi-observer dispatch, and the earlier full declared-hook gate does not prove full repository verification of subsequent constructor changes. Continue that integration before the next full gate; do not mark milestone 07 complete from these constructor checks alone.

### Milestone 07 observer declarations and database ownership

The next increment implements `lifecycle.Observer[M,H]`, opaque declarations, immutable observer sets, and `database.RegisterObserver` through the existing foundation construction graph. Contributions target a specific pool key and retain provider dependency order. Pool binding is a separate private service constructed before boot; observer constructors can receive the prepared database without circular construction. Manual binding is available only once on a directly prepared pool. Registrations and binding reject duplicates, missing/cyclic dependencies, incompatible hook types, nil factories and post-freeze changes. Each pool carries its set through sessions, transactions and child savepoints, including custom transactor wrappers.

Targeted observer race tests passed for lifecycle (1.013 seconds) and database ownership (1.011 seconds), followed by vet. Broader foundation, testkit, lifecycle, database and required PostgreSQL adapter race suites subsequently passed in 1.070, 1.011, 1.006, 1.115 and 2.434 seconds respectively; vet also passed. The independent PostgreSQL observer-ownership consumer passed with race detection in 1.030 seconds. Two compiler-rejection cases passed in 0.194 seconds, rejecting another model's hook factory and dependency constructor. The catalog now contains 417 cases; these two passed independently, the previous contribution case passed separately, and the last full gate covered its then-current 414 cases. Declared-hook and mutator consumers passed with race detection in 1.336 and 1.076 seconds. Actual gopls completion/hover/definition for registration passed in 5.781 seconds, followed by documentation validation. Review kept the internal binding service's type private so it cannot appear in unrelated `ResolveAll[struct{}]` contributions. The constrained VM profile remains in use; these results do not establish default compiler resource acceptance or a full repository gate for the current tree. Complete the generated integration before the next full gate.

That ownership increment established binding and propagation without changing automatic callback dispatch. The generated integration below connects those capabilities. Milestone 07 and the full framework goal remain incomplete.

### Milestone 07 generated observer dispatch

Generation now provides `<Model>Observer`, `New<Model>Observer` and `Register<Model>Observer`, fixing both the identifier owner and concrete hook-factory result. Every generated model has an observer adapter, including models without a local factory. The shared write pipeline resolves the actual supplied transaction's immutable observer set, rejects a registered model whose stale/manual definition lacks an adapter, and retains a one-statement mutation path for observer-free wrappers. Ready pools may prove their sets are frozen; prepared pools cannot use that shortcut while binding/startup could still occur.

The generated adapter constructs each factory once after the existing-row lock, then runs callbacks by stage with the model-local hooks first. It reconstructs one draft, applies field mutators once, compares the captured stored snapshots once, and registers after-commit callbacks individually. The public [observer guide](../docs/guides/model-hooks.md#provider-observers) and independent `observerqueries` fixture describe this concrete consumer experience.

The canonical query suite passed in 0.121 seconds, then again in 0.112 seconds after the prepared-pool guard and protocol regressions. Fresh/repeated/current hook and observer generation, invalid factories and generated-name collisions passed in 16.389 seconds. Recursive generation updated 29 owned files; a later recursive current-output check passed. Driver tests with race detection passed in 1.012 seconds, covering missing adapters, factory errors/panic/Goexit, transaction ownership, rollback and a no-observer wrapper's single mutation statement. Required PostgreSQL observer dispatch passed with race detection in 1.354 seconds, alongside existing declared hooks (1.314 seconds) and mutators (1.078 seconds). The observer fixture covers complete create/update/delete stage order, shared assignment/change data, missing rows, unannotated models, session/wrapper/nested ownership, bulk skipping, veto/panic/Goexit, cancellation, related-write rollback, parent rollback, later callbacks after an after-commit failure, and concurrent operation-local factories.

Two generated-API compiler rejection cases passed in 3.451 seconds for a different model's observer identifier and factory. The catalog now contains 419 cases; the last full gate covered 414, with the five subsequent contribution/observer cases checked separately. Actual gopls completion/hover/definition passed in 11.000 seconds for generated observer registration and the existing typed hook callback. Documentation checks passed. These are targeted integration results under the recorded constrained VM compiler profile. A fresh full repository gate and query race suite are still required for the combined changes; default compiler resource acceptance remains separate. This does not complete milestone 07 or the full framework.

## Rust module coverage

Inventory source: `src/lib.rs`, `src/public.rs`, public guides, and acceptance tests. The [source reconciliation](../docs/guides/parity-reconciliation.md) maps every row to current Go source, guides and representative acceptance sources, distinguishing the accepted milestone 23 baseline from unverified milestone 24 additions. A mapping alone does not assert runtime equivalence or new verification.

| Rust module | Go destination | Owning milestone(s) |
| --- | --- | --- |
| `foundation` | `foundation`, root application assembly | 02 |
| `config` | `config` and feature-owned configuration structs | 02 |
| `kernel` | Kernel interfaces plus each owning feature's runtime | 02, 08, 12–15, 23 |
| `logging` | `logging`, `observability`, `tracing`, `health`, `maintenance`, protected `diagnostics` | 02, 24 |
| `support` | `model` IDs, `value`, `clock`, `temporal`, `collection`, cryptographic and coordination packages | 02, 09, 20 |
| `app_enum` | `enum`, generated named values, codecs and enum descriptors | 03, 20 |
| `database` | `database` and its query/model/driver subpackages | 04–07 |
| `events` | `events` and transaction-aware integration | 07, 12 |
| `audit` | `audit` using typed mutation snapshots | 07 |
| `http` | `http` | 08 |
| `validation` | `validation` | 08 |
| `redis` | Redis adapter and shared backend contracts | 09 |
| `cache` | `cache`, shared `keyspace` namespace/codecs | 09 |
| `support::lock` | `lease`, `lease/memory`, Redis owner-checked adapter; cancellation-aware heartbeat and owned cleanup replace detached drop behavior | 09 |
| `auth` | `auth`, providers, credentials and policies | 10 |
| `storage` | `storage` with local and S3/R2 adapters | 11 |
| `jobs` | `jobs` | 12 |
| `scheduler` | `schedule` | 13 |
| `websocket` | `websocket` | 14, 15 |
| `email` | `email` and driver packages | 16 |
| `notifications` | `notifications` | 17 |
| `imaging` | `imaging` | 18 |
| `attachments` | `attachments` | 18 |
| `metadata` | `metadata` | 18 |
| `translations` | `translations`; locale infrastructure shared with `i18n` | 18, 20 |
| `settings` | `settings` | 18 |
| `countries` | `countries` reference model/data | 18 |
| `datatable` | `datatable` | 19 |
| `i18n` | `i18n` | 20 |
| `http_client` | `httpclient` around standard HTTP transports | 20 |
| `contract` | `contract`, normalized manifest | 03, 21 |
| `openapi` | OpenAPI adapter over the manifest | 21 |
| `typescript` | TypeScript generator and SDK transport | 21 |
| `plugin` | `plugin` and typed feature contributions | 02, 22 |
| `cli` | Framework CLI runtime and `cmd/foundry` developer tool | 02, 23 |
| `testing` | `testkit` plus external fixtures | Every milestone, 23 |
| `prelude`, `public` | Explicit Go package exports and root assembly | 01, 02; no Go prelude |

Rust's `__private` and `__reexports` are implementation mechanisms, not consumer features. Map `foundry-macros` and `foundry-build` into the generator; map `tools/foundry-agent` into gopls tooling; replace `foundry-api-doc` with Go package documentation and compiled examples. Include `examples`, acceptance tests, consumer/plugin fixtures, and release checks as evidence sources.

### Database parity follow-ups from the completion review

Earlier source review identified these required capabilities. Their current disposition is below; they are not deferred milestone 25 extensions. The linked source reconciliation keeps historical gaps separate from accepted implementation and pending production verification.

| Reference behavior | Current evidence and required owner |
| --- | --- |
| `INSERT ... SELECT`, joined update assignments and delete source filters | Completed in 07 and covered by the accepted combined baseline. [Typed insertion](../docs/guides/model-insert-from-query.md) and [source writes](../docs/guides/model-source-writes.md) preserve source/destination ownership, ambiguity rollback and explicit set-based lifecycle semantics. Runtime evidence sources include `database/query/insert_select_test.go` and `source_write_test.go`, generator and PostgreSQL consumer suites |
| Query plans and explicit execution analysis | Completed and accepted in [23](23-developer-tooling-and-testing.md). [Plan inspection](../docs/guides/query-plans.md) reuses compiled bindings, distinguishes planning from execution and retains required transaction ownership; `database/query/explain_postgres_test.go` covers actual PostgreSQL behavior |
| Optional read pool, explicit primary reads and combined pool health | Complete in accepted milestone 24; native routing, consumer/compiler and editor checks passed in the [production acceptance](../docs/production-acceptance.md). [Routing](../docs/guides/database-routing.md) provides typed optional endpoints, primary-owned transactions, combined bounds and separate health without SQL-text guessing. Replica lag is outside this proof |

### Milestone 07 combined observer verification

The complete `make TEST_PACKAGE_BATCH_SIZE=3 verify` passed in 1920.5 seconds with required PostgreSQL and actual gopls enabled. This includes all core/consumer packages and the current 419 compiler-rejection cases, complete gopls acceptance (517.835 seconds), the full generator suite (282.817 seconds), recursive current output, formatting/vet and documentation validation. The updated explicit-getter and automatic getter/mutator field-notice contracts were included in the documentation gate; their implementation is subsequent work, not covered by this result.

Full query race tests then passed in 1.366 seconds, followed by required PostgreSQL observer, declared-hook and mutator race suites in 1.355, 1.332 and 1.094 seconds respectively. The ignored `.cache/milestone07-observer-dispatch-complete.exit` records terminal zero. The VM retained 6.35 GiB free. These checks used the documented constrained compiler profile; default-flag build-resource acceptance remains in milestone 24. Milestone 07 and the whole-framework goal remain incomplete.

### Milestone 07 explicit getters and field awareness

The [getter guide](../docs/guides/model-accessors.md) makes `Access<Field>() (Result, error)` a concrete, ordinary Go model method. Generation discovers its actual signature beside persistence mutators and maintains field notices without a casts map or extra configuration. Stored IDs, nullable values, query keys and model fields stay intact; consumer DTOs explicitly select a getter result. Source comments and generated descriptor/draft documentation share the same checked metadata. Readable completion output now retains actual LSP documentation.

Focused generator checks passed in 12.697 seconds for source preservation, repeat/current output, notice removal, fresh getter signatures, invalid declarations, rollback and valid/invalid journal recovery. Review fixed an unused import, then refined source-token comparison to account for Go's optional semicolon before a closing brace when formatting compact declarations. The expanded checks subsequently passed in 10.630 seconds, including forced publisher termination, active-publisher exclusion, concurrent source edits and unsafe journal classifications. Recursive generation updated nine files, including managed notices in handwritten model files; subsequent generation wrote no files and current-output checking passed.

The PostgreSQL mutator/getter consumer passed with race detection in 1.097 seconds, proving explicit typed DTO output, preserved stored models/query identity and NULL handling. Two compiler-rejection cases passed in 0.194 seconds, bringing the catalog to 421 cases. Actual gopls completion/hover/definition and readable/JSON field documentation passed in 22.627 seconds for ordinary raw fields with getter-only, setter-only and combined behavior, existing field help, generated descriptors and draft setters. A concrete getter-return-type/definition probe passed in 4.508 seconds. The first field-doc probe had compared JSON-escaped Markdown underscores directly; it now checks decoded displayed text while retaining the server response unchanged.

The ignored `.cache/milestone07-field-documentation-targeted.exit` records terminal zero. The subsequent complete `make TEST_PACKAGE_BATCH_SIZE=3 verify` passed in 1966.2 seconds with required PostgreSQL and actual gopls enabled. It includes all root and consumer packages, the 421 compiler-rejection cases, the full language-tooling suite (547.304 seconds), the full generator suite (298.797 seconds), current generated output, formatting/vet and documentation validation.

The full generator race suite then passed in 322.631 seconds, followed by the readable completion-documentation race check in 1.009 seconds. The ignored `.cache/milestone07-field-documentation-complete.exit` records terminal zero; the VM retained 5.82 GiB free. Verification used the previously documented constrained compiler profile, so default-flag build-resource acceptance remains outstanding in milestone 24. The explicit-getter and automatic field-awareness increment is verified. Retrieval events, remaining lifecycle operations, events/outbox/audit, later milestones and the final framework-wide audit remain incomplete.

### Milestone 07 read observer ownership

The database runtime now retains its immutable observer registrations on returned `Rows`. Forwarding executors preserve the actual pool, session, transaction and savepoint ownership, including after stream closure and scope expiry. Inspecting this metadata does not construct hook factories or keep a connection open. This is infrastructure for retrieval dispatch; it does not implement retrieval callbacks or their operation lifetime.

The source-overlay prototype passed its wrapper, pool-isolation, closed-scope, concurrent-inspection and stream-failure tests with race detection in 1.009 seconds. Promotion checked that the original source hashes still matched. The canonical full database and lifecycle race suites then passed in 1.113 and 1.005 seconds. The independent PostgreSQL observer consumer passed with race detection in 1.386 seconds, including typed model reads through an executor wrapper, retained registrations after transaction expiry and no write-factory construction. Affected runtime/consumer vet and documentation checks passed. The ignored `.cache/milestone07-row-observer-ownership/canonical.exit` records terminal zero. A further complete repository gate remains required for the subsequent combined lifecycle changes.

### Milestone 07 separate retrieval registration

The existing registry now retains write and retrieval factories in separate groups for each exact model type. Different concrete hook types coexist across the two kinds; incompatible types within one kind and duplicate names across the pool are rejected. Lookup remains immutable, ordered and independent of factory invocation. Existing database registration and row ownership carry both kinds without another application container. This increment does not dispatch retrieval callbacks.

Full database and lifecycle race suites passed in 1.119 and 1.009 seconds. The independent PostgreSQL observer consumer passed with race detection in 1.398 seconds while registering both kinds for `Record` and retrieval-only factories for `Effect`, the model written by observer side effects. Runtime/consumer vet, recursive current-generation checking and documentation validation passed; the ignored `.cache/milestone07-retrieval-observer-registry/canonical.exit` records terminal zero. A follow-up assertion explicitly checks that normal writes and their side effects leave the retrieval-factory counter untouched.

The subsequent complete `make TEST_PACKAGE_BATCH_SIZE=3 verify` passed in 1968.9 seconds with required PostgreSQL and actual gopls. It covers all root and independent consumer packages, including the follow-up write assertion and all 421 compiler-rejection cases (consumer root: 50.729 seconds), the full language-tooling suite (548.714 seconds), the full generator suite (291.220 seconds), current generated output, formatting/vet and documentation validation. The ignored `.cache/milestone07-retrieval-ownership-complete.exit` records terminal zero; the VM retained 7.07 GiB free. The previously recorded affected runtime/lifecycle/consumer race checks accompany this complete gate. These results use the constrained compiler profile and do not complete retrieval dispatch or milestone 07.

### Milestone 07 retrieval callback lifetime

`Rows.WithObserverScope` now separates callback work ownership from the SQL stream. Closing rows permits further SQL on the same transaction/session while callback cancellation continues to follow the actual query caller and owning scope. Pool shutdown drains retained callbacks; prematurely returning from a session, transaction or savepoint cancels and waits for them before framework completion or commit. A standard-library automatic rollback may discard a connection before callback exit, so the contract prevents reuse and does not promise to retain an already discarded physical connection. The primitive itself does not dispatch model retrieval hooks.

The first source-overlay test exposed an incorrect `InUse == 1` assertion after automatic rollback. Inspection of the installed `database/sql` implementation confirmed that drivers without both session-reset and connection-validation interfaces have their connection discarded. Corrected tests cover retained framework ownership, no idle connection, no commit/parent return, and expired executor rejection across sessions, transactions and savepoints. The full overlay passed with race detection in 1.055 seconds; original source hashes were checked before promotion.

Canonical database, lifecycle and required PostgreSQL adapter race suites passed in 1.160, 1.015 and 2.449 seconds. The full independent PostgreSQL observer consumer passed with race detection in 1.418 seconds, including the new single-connection callback scenario: typed create/find work after stream closure, successful transaction/savepoint completion, callback veto rolling back its side effects, and expired callback contexts. Runtime/consumer vet, recursive current-generation and documentation checks passed. The ignored `.cache/milestone07-observer-scope/canonical.exit` records terminal zero.

The subsequent complete `make TEST_PACKAGE_BATCH_SIZE=3 verify` passed in 1978.7 seconds with required PostgreSQL and actual gopls. All root and consumer packages passed, including the full language-tooling suite (549.684 seconds), full generator suite (295.906 seconds), consumer root and all 421 compiler-rejection cases (51.466 seconds), current generated output, formatting/vet and documentation validation. The ignored `.cache/milestone07-observer-scope-complete.exit` records terminal zero; 7.07 GiB remained free. The recorded affected runtime/consumer race results accompany this gate. Verification uses the constrained compiler profile; default-flag build-resource acceptance remains in milestone 24. The callback lifetime increment is verified. Generated retrieval declarations/dispatch, complete-model result propagation, bounded streaming behavior and the remaining milestone 07 scope are still required.

### Milestone 07 generated retrieval dispatch

Generated models now expose concrete `<Model>RetrievalHooks` and typed observer registration. The `retrieval=Factory` directive declares a local factory independently from write hooks. Complete model records carry their retrieval adapter through ordinary/locked reads, relations, projections of complete records, aliases, CTEs, sets and pagination. DTO/scalar reads and write-owned hydration skip retrieval dispatch. Set results preserve their first input's existing decoder and lifecycle declaration. Callback factories run once per nonempty fetch/batch and selected model role, after rows close; callbacks retain the caller's executor and the verified row callback lifetime. `Each` uses bounded batches when retrieval callbacks may run. Stored fields and explicit getters remain separate. See the [retrieval guide](../docs/guides/model-retrieval.md).

The isolated runtime prototype passed seven race-test groups in 1.030 seconds, covering complete-record propagation, callback failures, actual owner registries, stale adapters, depth limits, bounded streaming and excluded result types. The generator prototype passed its five selected write/observer/retrieval groups in 23.516 seconds, including fresh/repeated/current generation and invalid factories, callbacks and reserved names. Both overlays verified unchanged original source hashes before promotion.

Canonical generation and complete database/lifecycle/query race suites passed (1.204, 1.006 and 1.369 seconds). Required PostgreSQL observer/hook/mutator consumer races passed (1.409, 1.332 and 1.080 seconds), followed by affected vet and recursive current-generation/documentation checks. The ignored `.cache/milestone07-retrieval-dispatch/canonical-core.exit` records terminal zero. A new independent `retrievalqueries` fixture subsequently passed its five PostgreSQL race-test groups in 1.862 seconds: injected callback ordering and wrapper I/O, direct/through/explicit relation loads, error/panic/Goexit/cancellation rollback, model/record batch windows and early stop, and page/cursor/join/CTE/set/locked/DTO boundaries. Five new required compiler-rejection cases passed in 0.529 seconds, bringing the catalog to 426. Two actual-gopls probes passed in 11.548 seconds, checking callback/registration completion, concrete hover signatures and definitions in the consumer workspace without edits. Consumer vet and recursive current-generation/documentation checks passed; `.cache/milestone07-retrieval-dispatch/consumer-retrieval.exit` records terminal zero. The subsequent combined gate is recorded below. This is not milestone 07 completion.

The first combined attempt stopped at cold-cache vet with unresolved query types. The named source files were subsequently confirmed present in the VM, byte-identical to the host, and included in Go's package inventory. Focused query vet then passed without source changes, followed by detailed full-root vet on the retry. The cause of the first failure is not established; retain its ignored `milestone07-retrieval-dispatch-complete-98915` log/exit evidence. The replacement `make TEST_PACKAGE_BATCH_SIZE=3 verify` passed in 2001.0 seconds with required PostgreSQL and actual gopls. All root and consumer packages passed, including the full language-tooling suite (563.539 seconds), full generator suite (304.355 seconds), consumer root and all 426 compiler-rejection cases (53.268 seconds), and the retrieval consumer (0.556 seconds). Current-generation and documentation checks passed; 6.85 GiB remained free. The full generator race suite subsequently passed in 339.826 seconds (360.6 seconds wall time). `.cache/milestone07-retrieval-dispatch-complete.exit` records terminal zero for the complete process; 5.56 GiB remained free. Retrieval dispatch is verified; the remaining milestone 07 work and final framework-wide audit are still required. These results use the previously documented constrained compiler profile; default-flag resource acceptance remains in milestone 24.

## Usage lessons to preserve

Rust Foundry-Starter demonstrates small kernel bootstrap functions, domain-owned models and invariants, service orchestration, thin HTTP DTO boundaries, centralized semantic identifiers, and generated frontend contracts. Preserve those responsibilities without copying Rust module syntax or application-specific business code.

Improve string-based lifecycle field inspection, repeated relation metadata, runtime guard/model mismatches, and duplicate authenticated-model loading. Avoid promising that a language change alone fixes storage: prove the provider contracts in milestone 11.

## Cross-cutting acceptance

Use the [common completion gate](README.md#common-completion-gate). Include negative compilation tests for wrong owners, values, IDs, relations, and payloads; isolated PostgreSQL/Redis/provider integration; race tests; protocol fuzzing; cancellation and resource-limit tests. Never claim exactly-once delivery or atomic DB/object-store commits.

Milestone status must separate implementation from verification. Record unavailable infrastructure as an outstanding check, not a passing result. Review each milestone's concrete consumer experience before the next begins; the active full-implementation objective authorizes sequential continuation after that review.


### Milestone 07 distinct mutation inputs

The shared mutation pipeline now accepts a concrete input type distinct from the stored field type. Generated drafts and before hooks retain that input; final codec validation, comparisons, model keys and change snapshots retain stored values. Generated field wrappers override only conflict-literal `Set`, preserving the existing operators and scopes. Pending inputs cannot bind to SQL before transformation. Nullable inputs retain omission, NULL and explicit zero. Reused drafts and conflict policies transform their original input once per attempt. Automatic field notices expose the input/storage distinction alongside getters and setters; generated draft formatting omits captured values. The [public guide](../docs/guides/model-mutators.md) and [independent consumer](../tests/fixtures/consumer/inputqueries/models.go) define the available API.

The isolated prototype passed the full query race suite and focused generator tests before promotion; original source hashes matched before and after that run. Focused generator acceptance took 26.628 seconds and includes import-name collisions, fresh/current output and invalid declarations. The sixteen framework files were then promoted; the ignored prototype baseline must not be replayed over canonical source.

Canonical generation passed, followed by the full query race suite (1.364 seconds), required PostgreSQL input/mutator/hook/observer/retrieval consumer races (1.079, 1.078, 1.331, 1.415 and 1.872 seconds), affected vet and recursive current-generation/documentation checks. `.cache/milestone07-mutation-input/canonical-core.exit` records terminal zero. The input consumer checks before-hook input values, stored assigned/changed snapshots, omission/NULL/zero, explicit getters, hook-added defaults, veto rollback and reusable bulk/conflict inputs.

Five new required compiler-rejection cases passed in 0.513 seconds, bringing the catalog to 431. Three actual-gopls probes passed in 15.317 seconds, inspecting concrete draft/conflict setter signatures and combined input/getter notices beside the handwritten field. Canonical focused generator tests passed in 25.549 seconds, followed by current-generation/documentation checks. `.cache/milestone07-mutation-input/semantic.exit` records terminal zero. Complete repository verification for this increment is recorded below. Checks use the constrained compiler profile; default-flag build-resource acceptance remains in milestone 24. Milestone 07 and the whole-framework objective remain incomplete.

A further fresh/current-generation regression passed in 12.896 seconds, preserving JSON properties, nullable JSON fields, aliases and conflict setters with distinct inputs, plus a custom input for a model-owned UUID key. Documentation validation passed; `.cache/milestone07-mutation-input/capabilities.exit` records terminal zero. This verifies generated API composition, not PostgreSQL default-UUID behavior.


The subsequent `make TEST_PACKAGE_BATCH_SIZE=3 verify` passed in 2093.6 seconds with required PostgreSQL and actual gopls. All root and consumer packages passed, including the full language-tooling suite (576.488 seconds), full generator suite (339.002 seconds), all 431 consumer compiler-rejection cases (consumer root: 52.754 seconds), and the input consumer (0.041 seconds). Current-generation and documentation checks passed; 6.67 GiB remained free. The full generator race suite subsequently passed in 373.638 seconds (394.2 seconds wall time). `.cache/milestone07-mutation-input/complete.exit` records terminal zero for the complete process; 5.23 GiB remained free. Distinct mutation inputs are verified. Subsequent clock/timestamp acceptance is recorded below. Milestone 07 and the whole-framework audit remain incomplete. These results use the constrained compiler profile; default-flag build-resource acceptance remains in milestone 24.


### Milestone 07 managed model timestamps

[Managed timestamps](../docs/guides/model-timestamps.md) recognize the persisted `CreatedAt`/`UpdatedAt` pair, with explicit opt-out, exact instant types, column mapping and automatic field notices. Database modules inherit application time; optional overrides and clock ownership follow pools, sessions, transactions and savepoints. The shared write path supplies timestamp assignments after before hooks and before once-only mutators. Bulk statements share one sample, and conflict updates copy the normalized proposed update time. Caller inputs remain reusable, changes reflect stored results, and physical deletion leaves timestamps alone.

The isolated runtime/PostgreSQL race suites passed (`database`: 1.188 seconds; query: 1.381 seconds; PostgreSQL: 2.438 seconds), followed by timestamp generator tests in 11.932 seconds and affected vet. After checking original hashes, the 21 implementation/test sources were promoted and four handwritten consumer files added. Canonical recursive generation passed in 115.5 seconds, the full query race suite passed, and required PostgreSQL time/input/mutator/hook/observer/retrieval consumer races completed in 56.9 seconds wall time. The new timestamp consumer passed in 1.081 seconds. Affected root/consumer vet and current-generation/documentation checks passed. `.cache/milestone07-model-time/canonical-core.exit` records terminal zero. Clock callback failure, compiler and actual-gopls acceptance is recorded next; the complete repository gate remains outstanding. Milestone 07 and the whole-framework goal remain incomplete.

The expanded timestamp consumer race suite also passed, including custom clock panic and `runtime.Goexit` on normal and bulk writes, payload-safe failures, rollback and parent-scope reuse. Two compiler-rejection cases passed for invalid clocks and timestamp setter types, bringing the catalog to 433. Three actual-gopls probes passed for generated timestamp setters, managed notices on the actual stored field and the transaction clock definition. Focused generator tests, opt-out notice removal, current generation and documentation checks passed. `.cache/milestone07-model-time/semantic.exit` records terminal zero (consumer command: 4.1 seconds wall time; compiler command: 26.4; gopls command: 15.8; generator command: 11.5). The complete repository gate and full generator race suite are next, using the constrained compiler profile. Default-flag build-resource acceptance remains in milestone 24.

The complete `make TEST_PACKAGE_BATCH_SIZE=3 verify` gate passed in 2149.3 seconds with required PostgreSQL and actual gopls. All root/consumer packages passed, including the full language suite (590.252 seconds), full generator suite (354.723 seconds), all 433 compiler-rejection cases (consumer root: 53.502 seconds), and the expanded timestamp consumer (0.062 seconds). Current generation and documentation checks passed; 6.49 GiB remained free. The same process is running the full generator race suite; its final combined exit is not yet available. Soft-delete work remains isolated in ignored text overlays with canonical source hashes checked unchanged. Milestone 07 and the full goal remain in progress.

The full generator race follow-up passed in 390.791 seconds (411.6 seconds wall time). `.cache/milestone07-model-time/complete.exit` records terminal zero for the complete process, with 4.99 GiB free. Managed timestamps are verified through independent consumer, compiler/gopls, PostgreSQL, race and complete repository acceptance. The next soft-delete increment remains unpromoted and uncompiled. Milestone 07 is still open for soft deletion/restoration/force deletion, relation and bulk-source writes, events, outbox and audit. These checks used the documented constrained compiler profile; default-flag resource acceptance remains in milestone 24.

### Milestone 07 soft-delete implementation evidence

The [soft-delete guide](../docs/guides/model-soft-deletes.md) documents conventional `DeletedAt`, typed restore/force-delete methods, independent target/pivot visibility and operation-aware lifecycle changes. The implementation shares the existing AST, codecs, automatic assignment path and transaction/observer ownership.

- Pre-promotion runtime/PostgreSQL races passed: database 1.191s, query 1.375s, PostgreSQL 2.409s and lifecycle 1.012s (144.4s combined wall time). An initial generator expectation depended on the private import alias `value`; the generated package correctly used `foundryvalue`. Removing that text-only assertion retained typed compilation checks. Focused soft-delete/timestamp generation subsequently passed in 30.164s (31.5s wall), followed by affected vet in 2.0s.
- Canonical recursive generation passed in 116.3s and the query race suite in 1.381s. Required PostgreSQL consumer races passed for softqueries, timequeries, inputqueries, mutatorqueries, hookqueries, observerqueries and retrievalqueries (81.8s combined wall). Softqueries passed in 1.197s, covering stage order, stored changes, after-commit operation identity, conflict/veto/outer rollback, cancellation and query visibility. Root affected vet passed in 0.3s and consumer affected vet in 24.3s.
- Source review corrected alias direct-source optimization and projection scope validation so neither path can discard automatic visibility. Consumer acceptance verifies direct/through/pivot scopes, aggregates, EXISTS, singular self-relations, DTOs, CTEs, sets, numbered pages, keyset iteration, locks and outer-join NULL preservation.

Canonical current-generation/documentation checks passed (98.5s wall), completing the core runner with terminal zero. Five required compiler-rejection cases passed in 7.034s (107.2s wall after reclaiming rebuildable Go cache), bringing the catalog to 438. Five actual-gopls probes passed in 26.709s (27.3s wall), verifying special write IDs, operation metadata, pivot scopes and automatic managed/getter documentation on the raw `DeletedAt` field. Their current-output/documentation follow-up passed in 79.4s wall, completing the semantic runner with terminal zero. The complete repository gate remains pending for this increment. The same constrained VM compiler profile applies. Milestone 07 remains open for relation/bulk-source integration, events, durable outbox and audit; later milestones and the final framework-wide audit remain outstanding.

The first full soft-delete gate stopped at Go's default 10-minute package timeout in `internal/agent` (719.4s total wall time). The active probe had run for two seconds and the complete serial consumer gopls test had accumulated 9m37s, rather than exceeding its per-operation deadline. The failure log is retained. `TEST_TIMEOUT` now owns an explicit 20-minute package budget across Make's ordinary, race, PostgreSQL and agent-smoke gates; individual gopls operation contexts remain 45 seconds. The PostgreSQL helper receives that same budget instead of its obsolete fixed two-minute timeout. Command propagation/failure-stopping tests, affected vet and documentation checks passed before the full-gate retry. This is a verification-harness correction, not proof that the full gate has passed.

The full-gate retry passed `make TEST_PACKAGE_BATCH_SIZE=3 verify` in 1893.0 seconds with required PostgreSQL and actual gopls, leaving 6.15 GiB free. The full gopls package passed in 611.347s, the complete generator package in 373.504s, and the consumer root in 44.107s with all 438 compiler-rejection cases. All consumer packages passed, including softqueries (0.114s) and timequeries (0.064s), and generated output/documentation checks passed. The full generator race follow-up is now running; the combined `complete.exit` has not yet been written. This completes normal repository acceptance for soft deletion, not milestone 07 or the final framework-wide audit.

The full soft-delete generator race follow-up passed in 413.317 seconds (434.1 seconds wall time). `.cache/milestone07-soft-delete/complete.exit` records terminal zero for the combined process; 4.54 GiB remained free. Soft deletion/restoration/force deletion and their query visibility are verified through required PostgreSQL consumers, compiler rejection, actual gopls, full repository and generator race checks. Typed relation-write work is still an unpromoted prototype; remaining milestone 07 work and the final framework-wide audit are not complete. The documented constrained compiler profile applies; default-flag resource acceptance remains in milestone 24.

### Milestone 07 typed relation writes

The isolated relation-write prototype passed the full query race suite in 1.382 seconds (101.5s wall including compilation), focused relation/default/soft-delete/timestamp generator tests in 54.421 seconds (55.8s wall), and affected vet in 1.4s. Its first attempt exposed only a test-double signature mismatch with the existing variadic transaction options; that was corrected and the failed log retained. Original hashes were verified before promoting nine sources and six independent consumer files. Canonical recursive generation passed in 125.9s, followed by the full query race suite in 1.373s (2.0s wall). Required PostgreSQL relation/lifecycle consumer races are running; compiler/gopls and complete repository acceptance remain outstanding. This does not complete milestone 07.

Canonical required PostgreSQL race acceptance passed for linkqueries (1.414s) and the affected soft-delete, timestamp, distinct-input, hook, observer and retrieval consumers (105.8s combined wall time). The relation consumer verifies endpoint locks held through pivot hooks, competing attachments under a partial unique index, stale/natural/nullable keys, self-links, scopes, lifecycle and changes, bounded removals, veto rollback and cancellation with parent reuse. Affected root vet passed in 0.2s and consumer vet in 38.6s. Current-generation and documentation checks passed in 102.8s; `.cache/milestone07-relation-writes/canonical-core.exit` records terminal zero. Five compiler-rejection cases and four actual-gopls probes are installed and awaiting focused verification; the compiler catalog now contains 443 cases. Full repository acceptance remains required.

Five new relation-write compiler rejection cases passed in 15.562 seconds (117.0s wall after reclaiming rebuildable Go cache), bringing required coverage to 443 cases. Four real-gopls probes passed in 22.034 seconds (22.6s wall), verifying completion, concrete model/draft/result signatures and runtime definitions in the independent consumer workspace. Current-output and documentation checks passed in 85.5s; `.cache/milestone07-relation-writes/semantic.exit` records terminal zero. The complete repository gate and full generator race follow-up are now running as one process. Focused acceptance is complete; full acceptance and the remaining milestone 07 work are not.

The first complete relation-write gate stopped with exit2 after1127.8s (11.21GiB free). The full gopls suite passed in638.961s. The complete generator suite then exposed three naming collisions in the new generated draft-default bridge: local `err`/`values` could hide handwritten field types, and receiver `d` could hide an imported input package. The failed log/exit are retained as `.cache/milestone07-relation-writes/complete.*.34908-names`. The bridge now uses the existing emitter local-name allocator for its receiver, defaults, captured values, error and input variables. Existing collision fixtures were expanded for `defaults`, `values`, `err` and `v` import names. Focused regressions and canonical generation checks are running before a full-gate retry; relation writes are not yet fully verified.

The naming fix passed all selected failed/expanded regressions and relation generation in74.067s (75.4s wall), affected vet in0.2s, and generation/current-output/documentation checks in2.8s. No independent consumer output changed. `.cache/milestone07-relation-writes/name-check.exit` records terminal zero. The full-gate retry is now running; its combined exit and generator race result are pending. The isolated per-model batch prototype was rebased onto this naming fix, with other canonical hashes checked unchanged; it remains uncompiled and unpromoted.

The relation-write retry passed `make TEST_PACKAGE_BATCH_SIZE=3 verify` in1333.2 seconds, leaving3.97GiB free. The full generator suite passed in414.099s, the consumer root and all443 compiler rejection cases in44.589s, and every consumer package passed, including linkqueries (0.256s) and softqueries (0.120s). Unchanged root checks, including gopls, reused Go test cache; the full actual-gopls result638.961s is retained from the first attempt. Current generation and documentation checks passed. The full generator race follow-up passed in 456.901s (477.7s wall), leaving 2.19GiB free. `.cache/milestone07-relation-writes/complete.exit` records terminal zero. Relation-write acceptance is complete. The remaining milestone 07 scope and whole-framework audit remain outstanding.


### Milestone 07 per-model batch writes

Generated `CreateEach`, `UpdateEach`, `DeleteEach`, `RestoreEach` and `ForceDeleteEach` return concrete model slices and reuse normal lifecycle behavior. Creation preserves input order and shares UUID/draft preparation with existing set-based creation. Existing-row operations lock and validate a bounded primary-ordered candidate set before callbacks. The callback receives the actual transaction and returns the model's concrete draft; all selected changes and queued after-commit work roll back together on failure. Relation detachment shares this runtime. The [consumer guide](../docs/guides/model-batch-writes.md) records limits and concurrency semantics.

The isolated prototype passed complete query races in 1.373s (132.0s wall after reclaiming rebuildable cache), focused batch/relation/soft-delete/timestamp and generated-name tests in 118.298s (137.4s wall), and affected vet in 18.7s. Original hashes were checked before promoting thirteen runtime/generator/consumer sources, retaining the five previous originals. Canonical generation passed in 143.6s (44 files), followed by complete query races in 1.375s (2.1s wall). Required PostgreSQL consumer races passed in 117.9s combined wall time: linkqueries 1.700s, softqueries 1.194s, timequeries 1.114s, inputqueries 1.080s, mutatorqueries 1.084s, hookqueries 1.325s, observerqueries 1.409s and retrievalqueries 1.850s. Affected root vet passed in 0.3s, consumer vet in 41.1s, and current-generation/documentation checks in 104.4s. `.cache/milestone07-per-model-writes/canonical-core.exit` records terminal zero. Five compiler-rejection cases passed in 0.585s (29.4s wall), bringing the catalog to 448 cases. Five real-gopls probes passed in 25.008s (25.4s wall), confirming concrete drafts, callbacks, result slices, limits and generated definitions. Current-generation/documentation checks passed again in 1.0s. `.cache/milestone07-per-model-writes/semantic.exit` records terminal zero. The full actual-gopls suite subsequently passed in 664.956s during repository verification. The full generator suite also passed in 427.498s, and the consumer root with all 448 compiler-rejection cases passed in 45.601s. The complete `make TEST_PACKAGE_BATCH_SIZE=3 verify` gate subsequently passed in 2136.5s, leaving 2.93GiB free. Every consumer passed, including linkqueries (0.429s), softqueries (0.111s), transactionqueries (0.442s) and upsertqueries (0.171s); current-generation and documentation checks passed. The full generator race follow-up passed in 473.543s (480.0s wall), leaving 1.15GiB free. `.cache/milestone07-per-model-writes/complete.exit` records terminal zero. Per-model batch-write acceptance is complete. The isolated lookup-write prototype is now being checked; remaining milestone 07 work and the final framework-wide audit remain outstanding.


### Milestone 07 lifecycle-aware lookup writes

Generated `FirstOrCreate` and `UpdateOrCreate` preserve concrete model/draft/callback types while sharing normal model persistence. The first primary-ordered lookup locks existing rows before callbacks and skips internal retrieval dispatch. Creation prepares its draft only on the missing branch and checks the stored result against original predicates and visibility. Update callbacks use the actual transaction and the same conversion/depth boundary as per-model batch updates. Physical uniqueness owns competing creations; there is no hidden retry. The [consumer guide](../docs/guides/model-lookup-writes.md) documents branch, scope, result and transaction semantics.

After the batch-write gate passed, the isolated lookup prototype passed complete query races in 1.375s (136.2s wall after reclaiming rebuildable cache), focused lookup/batch/relation/lifecycle/name-collision generation in 122.771s (142.2s wall), and affected vet in 18.4s. Original hashes were checked before promoting ten source/test files, retaining three prior originals. The first canonical consumer run exposed two test expectations that did not match the existing fixture: it registers Saving/Saved rather than Updating/Updated callbacks. The corrected test now asserts the exact registered update order and vetoes the registered post-write Saved hook; runtime and generator code were unchanged. Failed evidence is retained as `.cache/milestone07-lookup-writes/canonical-core.*.97680-hook-fixture`. The canonical retry passed generation in 146.1s, complete query races in 1.373s (2.1s wall), and all eight required PostgreSQL lifecycle consumer races in 26.3s combined wall time (linkqueries 1.963s). Affected root/consumer vet passed in 0.3s/1.1s and current-generation/documentation checks in 1.1s. Five compiler-rejection cases passed in 0.575s (29.4s wall), bringing the catalog to 453; two actual-gopls probes passed in 11.953s (12.3s wall), followed by current-generation/documentation checks in 1.0s. Canonical-core and semantic exit files record terminal zero. The full `make TEST_PACKAGE_BATCH_SIZE=3 verify` gate passed in 2274.9s, leaving 2.75GiB free. The full actual-gopls suite passed in 674.882s, the full generator suite in 477.108s, and the consumer root with all 453 compiler-rejection cases in 50.846s. Every consumer passed, including linkqueries (0.630s), transactionqueries (0.397s) and upsertqueries (0.155s); current-generation and documentation checks passed. The full generator race follow-up passed in 483.704s (490.2s wall), leaving 0.95GiB free. `.cache/milestone07-lookup-writes/complete.exit` records terminal zero. Lookup-write acceptance is complete. The isolated insert-from-query prototype is now under targeted verification; it has not been promoted. Milestone 07 and the final framework-wide audit remain incomplete.


### Milestone 07 typed insertion from queries

Generated `Insert<Model>From` builders retain source and destination ownership through stored-value selectors and literal model drafts. Mutated columns and managed update timestamps have no SQL selector; runtime mapping validation also rejects bypasses. Literal setters and clock conventions run inside the actual transaction. Destination required/default/NULL checks and timestamp presence reuse shared model-write rules, while source SQL, CTEs and binding order use the existing SELECT compiler. `Exec` returns an affected count without collecting models. `Returning` retains bounded complete models and rolls back on excess output without truncating the selected source. See the [guide](../docs/guides/model-insert-from-query.md).

The first isolated query race suite passed in 1.372s (146.2s wall after reclaiming rebuildable cache). The new generator fixture initially omitted required explicit natural-key annotations; correcting its two declarations required no runtime or generator change. The retry passed complete query races in 1.365s (2.5s wall), focused generator tests in 85.248s (86.6s wall), and affected vet in 18.6s. Original hashes were checked before promoting fifteen runtime/generator/test sources, retaining six prior originals. Canonical generation passed in 132.3s (46 written files), followed by complete query races in 1.396s (2.3s wall). The first consumer attempt caught a missing `.Scope()` in the new aliased-CTE fixture; all eight other PostgreSQL consumer families passed. Correcting only the fixture and aligning its inspected generated field comments required no runtime/generator change. The focused retry passed linkqueries races in 2.220s (24.2s wall), root vet in 0.3s, all nine affected consumer vets in 81.5s, and current-generation/docs in 70.5s. Failed evidence remains under `.cache/milestone07-insert-select/canonical-core.*.45994-alias-scope-fixture`. The consumer covers UUID defaults, NULL, exact decimals, typed JSON, setters/timestamps, source visibility/windows/CTEs, rollback, trigger-suppressed rows and count-only sources above the returned-model limit. A review then removed a fixture assumption about batch RETURNING order; the affected PostgreSQL race test passed in 1.042s (22.9s wall). Eight compiler-rejection cases passed in 0.930s (29.5s wall), bringing the catalog to 461. Three new actual-gopls probes and the existing conflict-field probe passed in 22.122s (22.5s wall); current-generation/docs passed in 1.2s. Canonical-core and semantic exit files record zero. Complete repository verification and full generator races are now being started; full acceptance remains required. Joined update/delete-source parity, events, durable outbox, audit and the final framework-wide audit remain outstanding.


The insertion full repository gate passed `make TEST_PACKAGE_BATCH_SIZE=3 verify` in 2206.2 seconds, leaving 1.92GiB free. Actual gopls passed in 688.075s, the full generator suite in 449.354s, and the consumer root with all 461 compiler-rejection cases in 47.072s. Every independent consumer passed, including linkqueries (0.701s), softqueries (0.113s), timequeries (0.067s) and transactionqueries (0.432s); current-generation and documentation checks passed. The full generator race follow-up remains live under session 5730; the combined acceptance result is still pending. Joined update/delete-source code is an isolated prototype and has not been promoted or compiled. Milestone 07 and the whole-framework objective remain incomplete.


The insertion generator race follow-up passed in 495.217 seconds (501.7s wall), leaving 0.06GiB free. `.cache/milestone07-insert-select/complete.exit` records terminal zero for combined acceptance. Typed insertion from queries is verified through required PostgreSQL, compiler rejection, real gopls, complete repository checks and the full generator race suite. The joined-write prototype is now under isolated targeted verification; it has not been promoted. Remaining milestone 07 work and the final framework-wide audit are outstanding. The constrained compiler profile remains applicable; default-flag resource acceptance is tracked in milestone 24.


### Milestone 07 joined source writes

The isolated runtime/generator prototype passed the complete query race suite in 1.394 seconds (145.4s wall after reclaiming rebuildable cache), focused source-write/insertion/lookup/soft-delete/timestamp/name-collision generation in 89.454s (108.8s wall), and affected vet in 18.3s. Its generated public source-write sample compiled from an independent module. Original hashes were verified before promoting fourteen runtime/generator/consumer sources, retaining five prior originals. Canonical regeneration and required PostgreSQL consumer races are now running. The [guide](../docs/guides/model-source-writes.md) documents source windows, destination-key comparison, ambiguity rollback, fixed setters, deletion visibility and bounded results. Twelve compiler-rejection cases and four real-gopls probes are prepared but have not been installed or run. Full acceptance, the remaining milestone 07 scope and the final framework-wide audit remain outstanding.


Canonical source-write generation passed in 150.8s, followed by complete query races in 1.395s (2.3s wall). All nine required PostgreSQL consumer race suites passed in 225.8s combined wall time: linkqueries 2.412s, upsertqueries 1.270s, softqueries 1.197s, timequeries 1.120s, inputqueries 1.075s, mutatorqueries 1.074s, hookqueries 1.333s, observerqueries 1.431s and retrievalqueries 1.864s. Affected root/consumer vet passed in 0.3s/82.9s and current-generation/documentation checks in 70.6s. `.cache/milestone07-joined-writes/canonical-core.exit` records terminal zero. Twelve compiler-rejection cases and four actual-gopls probes are now installed; their focused semantic acceptance is next. Full repository acceptance and the remaining milestone 07 work remain required.


All twelve new compiler-rejection cases passed in 31.4s wall, bringing the required catalog to 473. Four real-gopls probes passed in 22.144s (22.6s wall), verifying concrete primary-key/source types, draft inputs, model results, natural keys and generated definitions. Current-generation/documentation checks passed in 1.4s. `.cache/milestone07-joined-writes/semantic.exit` records terminal zero. The complete repository and full generator race gate is now running as session 63262; full acceptance is pending. No later milestone or final framework-wide audit is complete.


The complete joined-write gate passed `make TEST_PACKAGE_BATCH_SIZE=3 verify` in 2276.2 seconds, leaving 1.99GiB free. Actual gopls passed in 711.389s, the full generator suite in 463.798s, and the consumer root with all 473 compiler-rejection cases in 48.332s. Every consumer passed, including linkqueries (0.857s), softqueries (0.114s), transactionqueries (0.437s) and upsertqueries (0.184s); current-generation and documentation checks passed. The full generator race follow-up passed in 512.993s (519.5s wall), leaving 0.10GiB free. `.cache/milestone07-joined-writes/complete.exit` records terminal zero. Joined-source write acceptance is complete. Typed model references and attribution for events/audit are now an isolated prototype under targeted verification; they have not been promoted. Events, durable outbox, audit and the remaining framework milestones/final audit remain unfinished. The constrained compiler profile still applies; default-flag resource acceptance remains tracked in milestone 24.


### Milestone 07 stored references and attribution

Eleven runtime/generator/consumer sources add typed model references, immutable attribution and shared lossless SQL-value snapshots. The cursor implementation reuses the extracted codec without changing its wire format. The first overlay attempt compiled and passed model races but could not run vet for virtual new-package directories; the implementation was placed in its real directories after verifying the original hashes, retaining two originals. Evidence remains under `.cache/milestone07-events/targeted.*.86206-overlay-directories`.

The first canonical attempt passed all three new package race suites, then the query compiler was killed before tests could run. Its evidence remains under `.cache/milestone07-events/canonical-core.*.45362-compiler-killed`. The measured retry kept single-package compilation and the existing per-package compiler flags, reducing GOGC from 25 to 10 and GOMEMLIMIT from 256MiB to 192MiB. It passed SQL snapshot/model/attribution races in 3.6s combined wall, complete query races in 1.388s (116.3s wall), and focused generator tests in 96.447s (123.7s wall), including independently compiled references, getter notices and name collisions. Peak child RSS was 3759.4MiB. Canonical generation passed in 204.8s (45 files), and five required PostgreSQL consumer race suites passed in 228.1s combined wall: mutatorqueries 1.101s, linkqueries 2.408s, keyqueries 1.107s, jsonqueries 1.170s and temporalqueries 1.200s. Root/consumer vet passed in 5.8s/77.9s and current-generation/documentation checks in 109.4s. `.cache/milestone07-events/canonical-core.exit` records terminal zero. Three compiler-rejection cases and two real-gopls probes are prepared for focused semantic acceptance; full repository acceptance remains required. Event dispatch/provider/after-commit sources and tests are a separate uncompiled prototype under `.cache/milestone07-event-bus`; no event bus, outbox or audit implementation is claimed complete.


Reference semantic acceptance passed: all three new compiler-rejection cases completed in 0.599s (31.1s wall), bringing the catalog to 476. Both real-gopls probes passed in 13.308s (13.8s wall), confirming concrete reference/key owners, generated getter notices and definitions in the installed consumer context. Current-generation/documentation checks passed in 1.9s. `.cache/milestone07-events/semantic.exit` records terminal zero. Complete repository and generator race verification are next; the reference guide retains its full-acceptance notice. The separate event bus prototype remains uncompiled and unpromoted. Remaining milestone 07 work and the full framework/final-audit objective remain active.


Reference full normal acceptance passed `make TEST_PACKAGE_BATCH_SIZE=3 verify` in 2495.3s: actual gopls 786.313s, the generator suite 592.412s, all 476 compiler-rejection cases with the consumer root 49.841s, every independent consumer, and current-generation/documentation checks. The full generator race follow-up reached Go's default ten-minute package timeout (602.529s test / 612.5s wall); it reported no data race, but this is not a race-suite pass. Evidence is preserved under `.cache/milestone07-events/complete.*.82722-timeout`. Only that race suite is being retried with an explicit 30-minute package deadline after confirming no remaining build tools and reclaiming disposable build cache. Runtime/generator/consumer inputs are unchanged; the event bus remains staged until the reference acceptance gate succeeds.


The reference race-only retry passed the full generator suite in 738.367s (770.5s wall) with an explicit 30-minute package deadline, leaving 12.33GiB free. `.cache/milestone07-events/race-retry.exit` and `complete.exit` record terminal zero; the failed default-timeout evidence is preserved. Combined with the existing full normal gate, stored-reference and attribution acceptance is complete. The public guide's pending notice has been removed. No runtime/generator/consumer changes were needed for the timeout retry.

### Milestone 07 typed event bus implementation

Sixteen reviewed runtime, context-helper, test and independent consumer files are now installed from `.cache/milestone07-event-bus`; both modified originals were checked and backed up before installation. Typed topics/listeners, bounded synchronous dispatch, explicit application registration, owned shutdown and after-commit payload/origin capture are implemented but have not yet passed canonical compilation. The first verification runs required PostgreSQL and relevant runtime/consumer races before installing new compiler-rejection and gopls cases. No event-bus completion is claimed yet. Durable outbox and audit remain outstanding; milestones 08–24 and the final full-framework audit remain required.


Event generation/formatting passed in 214.8s. Event, context-link and database race suites passed in 51.4s combined wall (1.074s/1.014s/1.180s test time), followed by complete query races in 1.379s (115.9s wall; peak child RSS 3731.9MiB). The first consumer batch found one unused import in the new event fixture; observerqueries, hookqueries and retrievalqueries races still passed in 1.423s/1.294s/1.826s. That failed attempt is preserved as `.cache/milestone07-event-bus/canonical-core.*.85871-unused-import`.

Removing only the unused fixture import let eventqueries pass its PostgreSQL race test in 3.0s wall. Root/consumer vet passed in 5.9s/0.8s and current-generation/documentation checks in 2.0s. `.cache/milestone07-event-bus/consumer-retry.exit` and `canonical-core.exit` record terminal zero; previously passed runtime/query/consumer races were retained. Five new compiler-rejection cases and three gopls probes are installed, bringing the compiler catalog to 481; focused semantic and full-repository acceptance remain required. The separate outbox prototype has seven formatted, uncompiled staged sources and no generated model output or delivery runtime.


Event semantic acceptance passed: all five new compiler-rejection cases completed in 31.4s wall and all three real-gopls probes passed in 20.0s wall, verifying typed after-commit payloads, model-owned event keys, listener signatures and actual definitions. Current-generation/documentation checks passed in 1.9s. `.cache/milestone07-event-bus/semantic.exit` records terminal zero; the compiler catalog contains 481 cases. The complete repository gate is next. Relevant runtime and consumer races already passed, and this event increment changes no generator implementation, so it does not add a redundant full generator race rerun. Remaining outbox/audit, later milestones and the final framework audit remain required.


The first complete event gate exposed a real cancellation scheduling race: the event shutdown test observed a nil operation error after its bus lifetime had already been canceled. `context.AfterFunc` propagation could still be queued. The run stopped in 131.7s; evidence is preserved as `.cache/milestone07-event-bus/complete.*.17153-cancellation`. The shared context link now snapshots its parent list and synchronously observes parent cancellation when `Err` is checked, closing the operation's `Done` before reporting cancellation. It preserves caller values and the existing parent-error mapping. A gated AfterFunc adapter makes the regression deterministic, including caller mutation of the original parent slice.

The deterministic link regression and original event shutdown test passed 200 repetitions each under race detection in 4.0s combined wall. Complete context-link, event, database and query races passed in 141.8s wall (test 1.010s/1.070s/1.188s/1.387s; peak child RSS 3745.3MiB). Required event/observer/hook/retrieval consumer races passed in 55.1s wall (1.040s/1.424s/1.345s/1.853s). Root/consumer vet passed in 3.9s/20.4s and current-generation/documentation checks in 167.3s. `.cache/milestone07-event-bus/cancellation.exit` records terminal zero. The full repository gate must pass with this fix before event completion; outbox, audit and the rest of the framework/final audit remain unfinished.


The event full retry passed `make TEST_PACKAGE_BATCH_SIZE=3 verify` in 2806.9s, leaving 5.07GiB free. Actual gopls passed in 800.437s, the full generator suite in 595.046s, and the consumer root with all 481 compiler-rejection cases in 62.198s. Every independent consumer passed, including eventqueries (0.018s), observerqueries (0.241s), retrievalqueries (0.541s), transactionqueries (0.440s) and upsertqueries (0.160s); current-generation and documentation checks passed. `.cache/milestone07-event-bus/complete.exit` records terminal zero. Combined with the previously passed runtime/query/consumer races and deterministic cancellation regressions, typed event acceptance is complete. Outbox and audit remain unfinished in milestone 07; milestones 08–24 and the final full-framework audit remain required. The constrained compiler profile and milestone 24 resource acceptance still apply.


### Milestone 07 transactional outbox

Fourteen runtime/model/consumer/build targets were installed after the event gate passed, retaining the previous Makefile. The first run generated the internal storage model but correctly rejected the consumer fixture’s numeric primary key without an explicit declaration (245.0s); evidence remains as `.cache/milestone07-outbox/canonical-core.*.3774-primary-declaration`. Adding `primary=ID` to that fixture fixed the declaration without changing the generator or runtime.

Canonical generation passed in 5.1s. Root outbox/store/event races passed in 64.2s wall (events 1.168s); required consumer races passed in 37.0s wall: eventqueries 1.112s, observerqueries 1.397s and hookqueries 1.308s. Root and consumer vet passed in 4.7s/5.1s; current-generation and documentation checks passed in 2.2s. A separate owned copy removed manifest-owned artifacts, regenerated the framework model and 75 consumer files, checked freshness, and compiled both framework and independent consumer with identical artifacts. That bootstrap passed in 222.1s, with peak child RSS 957.5MiB. `.cache/milestone07-outbox/canonical-core.exit` records terminal zero. Seven compiler-rejection cases and four real-gopls probes are installed; semantic and full-repository acceptance remain outstanding. No delivery runtime or milestone 07 completion is claimed.


Outbox semantic acceptance passed: all seven new compiler-rejection cases completed in 30.8s wall, bringing the catalog to 488 cases. Four real-gopls probes passed in 21.1s wall (20.640s test), proving typed enqueue, stored payloads, model fields and generated ID setters through completion, hover and definitions in the consumer workspace. Current-generation/documentation checks passed in 2.1s. `.cache/milestone07-outbox/semantic.exit` records terminal zero. Full repository verification is next; relevant runtime and consumer races plus clean-checkout bootstrap already passed. No generator implementation changed in this increment. Audit, later milestones and the final framework-wide audit remain required.


Outbox full acceptance passed `make TEST_PACKAGE_BATCH_SIZE=3 verify` in 2553.4s, leaving 3.50GiB free. Actual gopls passed in 819.975s, the full generator suite in 593.181s, and the consumer root with all 488 compiler-rejection cases in 52.289s. Every consumer passed, followed by both framework/consumer current-generation checks and documentation validation. `.cache/milestone07-outbox/complete.exit` records terminal zero. Combined with the preceding runtime/consumer races and clean-checkout bootstrap, transactional outbox acceptance is complete. No generator implementation changed in that increment. Audit is now an isolated uncompiled prototype; milestone 07, milestones 08–24 and the requested final full-framework audit remain unfinished. Default-flag compiler-resource acceptance remains tracked in milestone 24.


### Milestone 07 audit implementation

Audit capture, generated model policies/readers/observers, transactional storage, typed domain actions, model-owned history, origin/area scoping and bounded retention are installed. Storage uses the generated internal audit model and shared query compiler; explicit migrations own its schema. No canonical/full completion is claimed before the following gate.

Prototype capture races passed in24.8s wall (1.061s package). The first generator attempt found a missing compiled-export dependency; adding audit/record to the existing generator package graph fixed it. Focused generator tests passed in31.495s (33.5s wall), vet in2.8s, and full framework/consumer generation/current/docs in420.6s (peak1004.0MiB). Storage generation, root races, vet and current checks passed: root audit1.104s/capture1.071s (58.2s wall), vet19.2s, peak602.3MiB. Consumer generation passed in203.8s; updated runtime races19.5s; independent PostgreSQL consumer races1.356s (19.6s wall), vet0.3s/7.0s and current generation1.9s; peak1007.1MiB. Prototype terminal records are retained under .cache/milestone07-audit, milestone07-audit-storage and milestone07-audit-consumer.

Ten compiler-rejection cases and four actual-gopls probes are installed for canonical verification, including structurally identical model-policy conversion, typed fields/IDs/payloads, transaction ownership and generated getter/setter notices. Canonical focused/full acceptance and the full generator race suite remain required. Milestones08–24 and the final full-framework audit remain unfinished.


Canonical audit acceptance passed: formatting/generation226.0s; root capture/audit races41.2s; independent audit consumer races25.0s (1.354s package), including duplicate-registration rejection; root/consumer vet1.5s/11.6s. All ten new compiler-rejection cases passed in7.783s (87.9s wall), bringing the catalog to498. Four actual-gopls probes passed in25.082s (25.6s wall), including typed field values and generated getter/setter notices. Current-generation and documentation checks passed in138.0s; peak child RSS1016.5MiB. `.cache/milestone07-audit-consumer/canonical.exit` records terminal zero. Review identified ordinary custom model-key codec errors whose messages were not hidden by the shared reference boundary; its regression/fix is being verified before full acceptance. Milestone07 remains active until full repository/generator race gates and completion review pass.


The shared reference review reproduced the ordinary-error message disclosure with a test-only overlay against the prior implementation. `Reference.Identity` and `Reference.Parse` now wrap custom codec failures with safe messages while retaining the original cause. Updated model, attribution, capture and audit races passed in43.0s wall; independent audit consumer races passed in24.2s wall (1.367s package); affected vet passed in19.5s, peak592.2MiB. `.cache/milestone07-audit-review/verify.exit` records terminal zero. Full repository verification and the complete generator race suite are next; no milestone/final-framework completion is claimed.


### Milestone 07 SQL NULL review correction

Completion review found that nil byte slices could be encoded as non-null empty bytes in shared SQL snapshots. Five test-only regressions reproduced the discrepancy in snapshots, codec binding/change comparison, model identities, audit fields and the real PostgreSQL driver boundary. One shared `NormalizeNull` helper now maps nil byte slices to SQL NULL while retaining non-null empty buffers. Snapshot capture, codec binding and model identity reuse it; the existing codec ownership test now explicitly requires both representations.

All five new regressions passed after the fix. Updated codec races passed in3.1s wall, complete query races in111.0s, and the audit/event/computed-key PostgreSQL consumer races in129.3s. Affected vet passed in20.3s and framework/consumer current-generation plus documentation checks in225.5s. Peak child RSS was3662.9MiB under the established serial VM compiler profile. `.cache/milestone07-audit-nil-bytes/verify.exit` records terminal zero; pre-fix evidence and the legacy assertion failure are retained separately. The database proof uses a read-only SELECT.

The earlier full repository run was deliberately interrupted to apply this correction and is not a pass. A fresh full repository gate and generator race follow-up are now running with the correction included. Milestone07 and the full-framework goal remain incomplete until their respective required checks and reviews pass.


### Milestone 07 full verification capacity recovery

The full normal run passed all45 framework packages, including real gopls in867.172s and the generator suite in660.033s. Consumer vet and eight consumer package results also passed before compilation of `correlations/nullable` failed because the VM exhausted build space. The command ended with exit2 after1908.1s, peak3551.2MiB and0.46GiB free. This was a build-capacity failure; it is not a successful `make verify` invocation. Its complete output is retained in `.cache/milestone07-audit-consumer/complete.log.11681-disk-capacity`.

Inspection found no changed Go/module/build inputs since that run began. After all build/test processes exited, clearing only the disposable Go build cache restored14.46GiB free. A guarded continuation preserves the45 root and eight consumer passes, runs the40 remaining consumer packages individually, then runs current-generation/documentation checks and the full generator race suite. It verifies unchanged non-documentation inputs and checks free space between completed commands, never clearing cache under a live compiler. The resumed checks remain in progress; milestone07 and the full-framework goal are still incomplete.


The guarded continuation has now passed all40 remaining consumer packages, completing normal coverage for all45 framework and48 consumer packages across the retained and resumed runs. The consumer main-package pass includes all498 unique compiler-rejection cases; each requires its expected compiler diagnostic and rejects timeouts/unexpected build failures. Framework/consumer generation freshness and documentation checks passed in61.4s. No canonical non-documentation inputs changed during the continuation. The full generator race suite is now running after cache reclamation; milestone07 completion and the final whole-framework goal remain unproven until their remaining gates pass.

### Milestone 07 completion and consumer review

The full generator race suite passed in 740.051 seconds (769.6 seconds wall time), with peak child RSS of 3556.2 MiB and 12.12 GiB free. The continuation's final source fingerprint check passed and `complete.exit` records zero. Together with the retained and resumed normal results, this completes current coverage of 45 framework and 48 independent consumer packages, all 498 compiler-rejection cases, real gopls, generation freshness and documentation. The original capacity-failed `make verify` remains recorded as exit 2; the resumed acceptance does not rewrite that history.

The consumer review confirms that a typed creating hook queries and assigns a default introducer without field-name strings; explicit getters map into separate DTOs while stored identities and fields remain intact; generated field notices identify getter/setter behavior in source and gopls; distinct mutation inputs retain their stored field types; and hooks, timestamps, soft deletion, relation writes and per-model batches preserve transaction and change semantics. Events, outbox records and audit reuse the actual transaction and stored snapshots. Audit registration contributes typed policies and dependencies while Foundry owns persistence, redaction, migrations and bounded retention.

Milestone 07 is complete. HTTP/validation/response implementation begins in 08. Password hashing/authentication, durable outbox delivery, storage, distributed WebSockets and the other 08–24 deliverables remain in their owning milestones. The constrained compiler profile and build-space recovery are documented limitations for production hardening in 24; they do not establish release readiness. The full-framework goal and its final verification/re-audit remain active.

### Milestone 08 HTTP kernel foundation

The initial [HTTP kernel](../docs/guides/http-kernel.md) prepares standard handlers during application construction and binds only when the HTTP kernel is selected. It uses the injected application logger, explicit native I/O/header bounds and one shutdown grace. Handler ownership outlives connection closure, including a synchronous hijacked handler, so application dependencies remain available until active work actually exits. Raw handler panics abort the response without logging their payload; intentional standard aborts remain silent.

Focused HTTP and foundation race tests passed (1.229s and 1.072s), as did the independent HTTP consumer race (1.108s), affected vet and documentation checks. Consumer coverage verifies constructor injection, deferred binding, other-kernel assembly, and dependency retention beyond the caller's shutdown deadline. No third-party dependency was added. Official Go releases were checked on 2026-09-13 and still list Go 1.27.1 as current stable.

This is the kernel portion of slice 1, not milestone 08 completion. Request-body bounds, request IDs, typed recovery/errors, route contracts, decoding, validation, middleware, pagination adapters and file transport remain in the owning blueprint. The initial kernel subsequently passed full repository acceptance, recorded below.

#### HTTP kernel full acceptance

The complete `make verify` passed for the initial kernel with Go 1.27.1, real PostgreSQL and gopls, all 46 framework and 49 consumer packages, all 498 existing compiler-rejection cases, current generated output and documentation checks. The final source fingerprint matched. Total elapsed time was 3573.1s, peak child RSS 3543.8 MiB and remaining build space 12.74 GiB. The capacity guard reclaimed disposable Go cache only between completed commands. The previous launcher-selection failure remains recorded separately; the corrected full run ended with exit zero.

Request limits, shared attribution and typed errors now proceed as the next kernel slice. This acceptance covers the initial kernel; milestone 08 and the full-framework goal remain in progress.

#### HTTP request limits, attribution and built-in errors

The next slice adds [bounded requests and typed public errors](../docs/guides/http-requests.md). It reuses `attribution.RequestID` and immutable origins, starts anonymous request scope, ignores untrusted IDs/forwarding headers, and correlates framework diagnostics with headers/error DTOs. Known oversized requests reject without waiting for their bodies; unknown streams use native bounded reads. A single built-in catalog owns response codes, statuses and public messages. Internal causes remain available through ordinary Go error inspection.

Review preserved security headers when replacing representation headers, matched error headers to attribution, kept classification order, and made cause wrappers comparable through pointers. Runtime HTTP/foundation/attribution races passed (2.539s, 1.078s, 1.017s), as did the public consumer race (1.110s) and affected vet. The consumer fixture exercises these APIs through real TCP. This follows the initial kernel's complete repository acceptance; it does not mark milestone 08 complete.

The subsequent full `make verify` passed for this request slice: all 46 framework and 49 independent consumer packages, all 498 compiler-rejection cases, real PostgreSQL and gopls, all three generation-freshness targets and documentation checks. The source fingerprint matched. The run ended with exit zero after 3050.1s, with peak child RSS 3552.0 MiB and 9.13 GiB free. The real-gopls and generator suites passed in 864.468s and 653.884s respectively. Disposable cache reclamation occurred only between completed commands under the existing constrained compiler profile. This proves the delivered kernel/request/error behavior; typed routing and the remaining HTTP milestone work have separate acceptance.

#### Typed routing and named URLs

The [routing guide](../docs/guides/http-routing.md) describes concrete path descriptors, model-owned IDs, named string/integer keys, immutable scopes and relative URL generation. A private native ServeMux owns matching, precedence, conflicts and method behavior. Foundry rejects duplicate IDs, incomplete bindings and implicit access declarations before returning a router. Shared JSON failures retain native method policy; inspection snapshots use the registered descriptors and mark raw payload boundaries explicitly.

HTTP runtime and consumer race tests passed (3.584s and 1.111s), together with five new compiler-rejection cases and affected vet. A bounded fuzz run passed 12,875 executions after finding and fixing an encoded single-slash ambiguity: incoming ambiguous segments now reject before native route selection, and URL generation rejects the same value. A regression verifies that neither a wildcard handler nor an exact trailing-slash handler receives that input. Ordinary embedded slashes and percent text still round-trip. Internal decoder failures retain safe route/request diagnostics through the kernel's injected logger; panic payloads remain excluded.

These checks ran against the actual public import paths through an isolated source overlay; only matching, tested files were subsequently promoted. Full repository acceptance of this routing code is recorded below. The remaining milestone 08 work continues separately. Route inspection metadata is not yet the complete normalized request/response manifest, and generated DTO validation, request deadlines, automatic pagination and supporting HTTP features remain required.

Formatting, all three generation-freshness targets and documentation checks also passed on the promoted files. Verification commands now anchor their working directory explicitly when the project VM resumes in its parent workspace.


### Milestone 08 typed routing full acceptance

The full `make verify` passed for typed routing with Go 1.27.1, real PostgreSQL and gopls, all 46 framework and 49 independent consumer packages, all 503 compiler-rejection cases, all three generation-freshness targets and documentation validation. The source fingerprint matched. `.cache/milestone08-http-routes/verify.exit` records terminal zero: elapsed 3310.1s, peak child RSS 3555.7 MiB and remaining build space 10.33 GiB. Real gopls and generator suite timings are retained in the verification log. The earlier focused routing race, vet and fuzz checks remain part of this acceptance, including the encoded slash-only ambiguity regression.

This completes acceptance of the delivered routing slice. The automatic path-binding generator is an isolated draft now undergoing its own focused tests; it is not part of this full-run result. Typed endpoint DTOs, JSON/query binding, validation, middleware, automatic HTTP pagination, file transport and full milestone 08 acceptance remain required. The serial constrained compiler profile remains a documented production-hardening limitation in milestone 24. The complete framework goal and requested final re-audit remain active.


### Milestone 08 generated path bindings

[Generated path bindings](../docs/guides/http-path-generation.md) now use handwritten Go structs and the existing shared generator pipeline. The generator emits typed selectors and chooses model-ID, named scalar, enum and complete text codecs. Runtime and generation share one path grammar. Output ownership, fresh-checkout handling, stale checks and whole-package type validation reuse the existing implementation. The independent consumer declares routes with generated descriptors and exercises scoped URLs, imported enum membership, numeric widths and catch-all values.

Focused acceptance passed after correcting an overlay-only vet working-directory limitation. Both failed harness attempts are retained; product sources were unchanged between attempts. The successful run passed HTTP races (3.612s), the four new generator race test functions (14.054s), affected vet, recursive independent-consumer generation (three new files), freshness, consumer HTTP races (1.111s), and four compiler-rejection cases (1.315s package). The runner ended zero after 352.8s, peak child RSS 1145.9 MiB and 8.58 GiB free. Its temporary empty vet working directory was removed, source fingerprints matched, and the 21 tested Go files were promoted with canonical-input guards.

Canonical consumer generation produced the expected three files. After formatting the separate JSON-wire draft reported by the repository-wide formatting gate, the remaining canonical checks passed: formatting, all three generation-freshness targets, documentation, consumer HTTP races (1.117s), and all three actual-workspace real-gopls probes for completion, hover and definition. The resumed canonical runner ended zero after 83.5s, peak child RSS 1145.3 MiB; the initial formatting failure and its successful generation result remain retained separately. Promoted source fingerprints matched.

Full repository acceptance of the generator addition remains required. The earlier full routing result does not cover these changes. The HTTP JSON wire prerequisite remains an isolated draft now undergoing focused checks, and milestone 08 remains in progress.


### Milestone 08 shared JSON wire prerequisite

The shared internal JSON parser now provides explicit transport limits and lossless number lexemes without imposing database-only normalization or NUL restrictions. Database snapshots keep their original canonical decimal, Unicode, NUL, byte, depth and node policies. Both modes share duplicate-key detection, surrogate validation, bounded traversal and overflow-safe counters. Typed HTTP DTO decoding remains upcoming work; this internal prerequisite does not expose models as response schemas.

Isolated races passed for jsonwire (1.060s), jsonshape (1.006s), typed values (1.027s) and database codecs (1.014s), followed by affected vet. A bounded lossless-tree fuzz smoke passed 22,108 executions; its successful 11.025s test is not an exhaustive parser proof. The checker ended zero after 54.1s, peak child RSS 264.3 MiB and 8.17 GiB free. All three tested sources were promoted after canonical/draft fingerprint checks. Full repository verification now covers this change together with the generated path bindings. Milestone 08 and the full-framework goal remain in progress.

### Milestone 08 path generation and JSON wire full acceptance

The combined addition passed `make TEST_PACKAGE_BATCH_SIZE=1 verify` using the existing constrained compiler profile and cache-capacity guard. All 47 framework packages and 49 independent consumer packages passed, including required PostgreSQL behavior and all 507 compiler-rejection cases. Actual gopls passed in 944.660s and the complete generator suite in 742.938s. Formatting, vet, all three generation-freshness targets and documentation checks passed. The final source fingerprint matched the original verification inputs.

`.cache/milestone08-path-generation/verify.exit` records terminal zero after 3550.4s, peak child RSS 3550.1 MiB and 9.39 GiB free. This closes full acceptance of the generated path bindings and shared wire-parser addition; it supplements their earlier focused races, bounded fuzzing and canonical consumer/gopls checks. It does not cover the separate DTO contract/generator drafts. Typed endpoint payloads, validation and the remaining milestone 08 scope are still in progress. The documented compiler-resource limitation remains a production-hardening gate in milestone 24, and the complete framework objective still requires its final verification and re-audit.


### Milestone 08 generated JSON DTO contracts

The public `contract` package and `//foundry:dto` generation now preserve concrete DTO types through immutable normalized schema descriptions and bounded strict decoding. Descriptors retain model-ID ownership, enum values from existing descriptors, scalar formats, recursive field shapes, exact names and independent presence/nullability. Shared JSON parsing rejects duplicate names and invalid Unicode; errors return owned bounded field diagnostics without received values. Codec failures discard partial DTOs, contain panic/Goexit and retain resource ownership until callbacks return. Direct and transitively embedded persistence models require an explicit response DTO.

Focused runtime race/vet acceptance and bounded fuzzing passed for the promoted sources (`milestone08-json-contract/check.exit`: zero, 37.2s wall, 177.2MiB peak, 4,586 fuzz executions). Generator acceptance passed all five DTO tests, including a real executable `main` package and fresh-package model leakage rejection (17.195s generator tests; 23.1s total wall, 267MiB peak). The independent consumer proves omitted/null patch behavior, imported enum reuse, model-ID ownership and six additional compiler-rejection cases. Its initial generated/freshness acceptance passed, followed by a 28.8s consumer recheck of the final runtime correction. Source fingerprints were checked before publishing 28 runtime, generator, fixture and language-probe files.

Canonical consumer generation, all three freshness targets, formatting, docs, consumer races and the six new compiler-rejection cases passed. All four actual-gopls probes ran and passed (20.677s), checking the concrete descriptor, Decode signature, model-ID field and Optional enum field. Canonical acceptance ended zero after 336.4s wall, peak child RSS 1189.6MiB and 10.61GiB free; its final promoted-source fingerprints matched. Full repository acceptance of this DTO addition remains required; previous path/wire acceptance does not cover it. Root collection/nullable composition, response encoding, explicit custom codec metadata and typed endpoint integration remain milestone 08 implementation work. The full framework goal and final re-audit remain active.


### Milestone 08 wrapped JSON precision correction

A focused review regression reproduced loss of `9007199254740993` to a rounded `float64` inside Optional/Nullable fields, both through direct wrappers and the new DTO decoder. Each wrapper's own `json.Unmarshal` had discarded the enclosing decoder's exact-number option. The shared wrapper decoder now uses `json.Number` and rejects trailing values before assigning a result. Nested stored snapshots retain the same representation.

The reproducer failed specifically on precision in both value and contract packages. After the correction, all value/contract/database-codec races passed (1.031s, 1.026s and 1.011s), as did the independent DTO consumer race (1.005s) and affected vet. The fixed run ended zero after 87.8s wall, peak child RSS 1155.6MiB and 10.26GiB free. The three checked files were published after canonical and candidate fingerprint checks. Full repository verification now follows for this correction and the DTO addition together; milestone 08 and the overall framework remain incomplete.


### Milestone 08 DTO decoding full acceptance

The generated JSON DTO addition and shared Optional/Nullable precision correction passed `make TEST_PACKAGE_BATCH_SIZE=1 verify` with the existing constrained compiler profile and cache-capacity guard. All 48 framework packages and 50 independent consumer packages passed, including required PostgreSQL behavior and all 513 compiler-rejection cases. Actual gopls passed in 911.718s and the complete generator suite in 686.259s. All three generation-freshness targets, formatting, vet and documentation checks passed. The final source fingerprint matched.

The successful run ended zero after 3458.9s wall, with peak child RSS 3583.5MiB and 12.22GiB free. Cache reclamation occurred only between completed commands. The earlier 2.3s formatting-only failure remains preserved separately; it is not counted as a successful run. This evidence closes full acceptance of the DTO decoding and precision correction, supplementing their focused races, generation, consumer and language checks. It does not cover the separate response-encoding and composition candidate, which is now entering focused acceptance. Milestone 08, later 09–24 work, the default-compiler resource gate and the final framework verification/re-audit remain outstanding.


### Milestone 08 typed JSON response preparation

`contract.JSON.Encode` now prepares owned bytes and validates the same generated schema used by decoding and contract descriptions before returning a body. `contract.Slice` retains ordinary typed Go slices, while `contract.Nullable` reuses `value.Nullable`; both derive their normalized graph from the element descriptor. Shared `value.EncodeJSON` reuses native field/tag metadata and wrapper inspection, applies input-work and output bounds, rejects invalid Unicode/duplicate names, and contains codec panic/Goexit without abandoning canceled work. Custom serialization and omission methods still own their internal work and allocation.

Focused checks initially reproduced a nil-pointer regression in candidate wrapper inspection; the failed candidate and its logs were preserved. After correction, value, contract and database-codec races passed (1.037s, 1.033s and 1.015s), along with all selected DTO generator tests (16.920s), the independent consumer race (1.016s), five additional compiler-rejection cases and affected vet. Oversized native-string rejection allocated 736 B/op and 17 allocations for both 1MiB and 16MiB inputs, with 852.6ns/op and 852.8ns/op respectively. This benchmark covers early native-string rejection, not every codec or workload.

The successful candidate check ended zero after 181.6s wall, peak child RSS 1147.9MiB and 11.30GiB free. Eighteen checked runtime, generator, consumer and language-probe files were published after canonical/candidate fingerprint checks. Native streaming JSON and text-appender protocols require explicit wire contracts instead of inferred struct fields. Model-ID serialization continues to preserve the nil UUID; operation-level non-empty identity validation is separate. Canonical editor/consumer checks and full repository acceptance now follow. Typed endpoint integration, custom codec/key declarations, validation and the other remaining HTTP work are still required; milestone 08 and the complete framework remain in progress.


### Milestone 08 response preparation canonical acceptance

After promotion, formatting, all three generation-freshness targets and documentation checks passed. The direct consumer race passed in 1.006s, and the five new compiler-rejection cases passed in 0.558s. All three actual-gopls probes ran and passed in 18.814s, verifying the concrete slice Encode signature, the generic Slice constructor and Nullable DTO decoding through completion, hover and definition lookup. The promoted-source fingerprints remained unchanged.

Canonical acceptance ended zero after 358.1s wall, with peak child RSS 1156.1MiB and 9.64GiB free. This confirms the response/composition public APIs in the actual consumer workspace. Full repository regression verification now follows for the eighteen promoted files; it is separate from the completed DTO-decoding acceptance. Milestone 08 and the complete framework objective remain in progress.


### Milestone 08 response preparation full acceptance

The full `make verify` run passed on the repository-selected Go toolchain in the
existing constrained VM profile. It covered all 48 framework packages and 50
consumer packages, 518 required compiler-rejection cases, real PostgreSQL,
actual gopls (933.495s), the complete generator suite (687.280s), formatting, vet,
all three generated-output freshness targets and documentation checks. The final
verification source fingerprint matched. The run ended zero after 3450.1s
wall, with peak child RSS 3550.0MiB and 12.22GiB free.

This closes full regression acceptance of the previously published response
encoding and slice/null composition, supplementing their focused race and
canonical consumer/editor checks. The separate typed-query candidates remain
unpublished and unverified at this point. Milestone 08, required milestones
09–24, default-compiler resource acceptance and the final framework-wide
verification/re-audit remain outstanding.


### Milestone 08 typed query bindings and generation

Published `http.Query[Q]`, required/optional/repeated field bindings and
`//foundry:query` generation. Handwritten structs own names and types once;
generated descriptors share the runtime and the existing generator workflow.
Path/query discovery and emission share scalar rules, and both query runtime and
generation use the same name grammar. Omitted values, explicit zero/empty values,
ordinary/named slices, model IDs and enums retain their Go representations.
Unknown names, scalar duplicates and missing fields fail before codec execution.

The wire parser passed races (1.008s), vet and 98,769 differential fuzz
executions. Oversized 1MiB and 16MiB inputs rejected with 48 B/op and one allocation
in both directions. Runtime/consumer races passed (3.579s/1.006s framework and
1.012s consumer), along with six new compiler-rejection cases and vet; that check
ended zero in 154.5s wall with 1150.6MiB peak child RSS. Its first overlay attempt
failed because vet required a real package directory; the already-tested private
parser was published there before retrying. The failed evidence was preserved.

Generator acceptance then passed the eight query/path test groups (14.663s),
consumer generation and freshness, the generated consumer race (1.014s), the six
compiler rejections (1.103s) and affected vet. It ended zero after 332.6s wall with
1145.9MiB peak child RSS. An earlier consumer assertion incorrectly mixed a named
boolean and bool; correcting that assertion retained the intended named type.
Twenty-five further Go files were published after fingerprint checks; the two
private wire files were already published. Canonical generation, actual gopls,
full repository regression and the rest of milestone 08 remain outstanding.


### Milestone 08 typed query canonical acceptance

Canonical generation produced the one new query descriptor. Formatting, all
three freshness targets and documentation checks passed. The actual consumer
race passed in 1.009s and the six added compiler-rejection cases in 1.050s. All
three new gopls probes ran and passed in 16.122s, covering the generated query
descriptor, concrete Decode signature and preserved enum-slice fields through
completion, hover and definition lookup. Promoted-source fingerprints matched.

The check ended zero after 121.8s wall with 1180.6MiB peak child RSS and 9.22GiB
free. Full repository verification is now running with the 524-case compiler
catalogue and required PostgreSQL/gopls coverage. The rest of HTTP milestone 08,
09–24, default-compiler resource acceptance and the final framework-wide audit
remain outstanding.


### Milestone 08 typed query full acceptance

The complete `make verify` run passed on the repository-selected Go toolchain
with the existing constrained VM compiler profile. It covered 49 framework
packages, 51 consumer packages and 524 required compiler-rejection cases.
Real PostgreSQL, actual gopls (935.772s), the complete
generator suite (689.622s), formatting, vet, all three
generated-output freshness targets and documentation checks passed. The final
source fingerprint matched. The run ended zero after 3476.1s wall with peak
child RSS 3548.8MiB and 12.09GiB free.

This completes regression acceptance of the published typed query runtime and
generation, supplementing their focused race, fuzz, consumer and language checks.
Native floating-point URL bindings and path callback ownership are separate
candidates being checked; this gate does not claim those changes as delivered.
Milestone 08, required milestones 09–24, default-compiler resource acceptance and
the final whole-framework verification/re-audit remain outstanding.


### Milestone 08 native URL floats and callback recovery

Native/named float32 and float64 fields now share width-aware URL codecs through
path/query generation, including optional/repeated bindings and custom text-codec
precedence. Focused runtime races, bit-roundtrip fuzzing, generated consumer
runtime/determinism, freshness and three new compiler-rejection cases passed.
The consumer compiler catalogue now contains 527 cases.

Path encoding/decoding now owns panic and Goexit without formatting arbitrary
codec errors. Cancellation retains callback ownership and suppresses later
fields/handler invocation. HTTP error classification similarly owns custom
As/Unwrap methods before response output, retaining wrapped/joined classifications
and safely reporting callback failures. All HTTP races and HTTP kernel/query
consumer races passed on the combined candidate overlays.

Focused evidence: milestone08-url-floats: 441.1s wall, 1178.0MiB peak; milestone08-path-callbacks: 47.2s wall, 361.1MiB peak; milestone08-error-recovery: 47.2s wall, 361.3MiB peak. Eighteen Go files were published, including three
new canonical gopls probes. Canonical generation/consumer/editor acceptance
follows publication; this section does not claim that gate or full regression
for these changes has passed. Milestone 08 and the full framework goal remain
in progress.


### Milestone 08 URL binding and recovery canonical acceptance

Canonical acceptance passed after publishing the combined float, path callback
and error-classification changes. The actual consumer generated two files;
formatting, all three freshness targets and documentation checks passed.
HTTP kernel/query consumer race tests and all three new compiler-rejection cases
passed. The compiler catalogue contains 527 cases. All three actual gopls probes
ran and passed (16.280s), covering the float query descriptor, named
float field and generated path URL. The eighteen published Go source hashes
remained unchanged.

The gate ended zero after 152.6s wall, with peak child RSS 1152.9MiB and
9.19GiB free. This verifies the canonical consumer experience; broader full
regression verification is running. Milestone 08 and the full framework goal remain
in progress, including the final whole-framework verification/re-audit.


### Milestone 08 URL binding and callback recovery full acceptance

The complete `make verify` run passed on the repository-selected Go toolchain
with the existing constrained VM compiler profile. It covered 49 framework
packages, 51 consumer packages and 527 required compiler-rejection cases.
Real PostgreSQL, actual gopls (953.710s), the complete
generator suite (692.300s), formatting, vet, all three
generated-output freshness targets and documentation checks passed. The final
source fingerprint matched. The run ended zero after 3503.0s wall with peak
child RSS 3540.7MiB and 12.09GiB free.

This completes regression acceptance of the published native URL floats and
path/error callback recovery, supplementing focused race, fuzz, generation,
consumer and language checks. Typed endpoint integration and request deadlines
remain separate unpublished candidates; this gate does not cover their drafts.
Milestone 08, required milestones 09–24, default-compiler resource acceptance and
the final whole-framework verification/re-audit remain outstanding.


### Milestone 08 typed endpoints and request-context deadlines

Typed endpoint descriptors now compose the existing route, query and JSON
contracts. Handlers receive `Input[P,Q,B]` and return their declared response
type. Empty payloads have explicit types; source-qualified field issues preserve
safe public diagnostics. Query/body limits and the kernel body ceiling compose.
Response encoding completes before success headers are committed. Native short
writes abort. Metadata exposes owned JSON graphs and query cardinality; URL
scalar schemas and the complete client manifest remain required work.

The kernel now applies a positive `RequestTimeout` after admission, preserving
earlier request-parent deadlines and values. Completion cancels the derived
context; deadline expiry never releases dependencies before the handler exits.
Native I/O bounds and raw response ownership retain their existing contracts.
No implicit request transaction or database model lookup is introduced.

Endpoint HTTP races passed (3.636s), followed by generation/freshness, four
independent consumer race packages, six compiler-rejection cases (1.963s) and
affected vet. That gate ended zero after 412.5s, with 1150.7MiB peak child RSS and
10.00GiB free. An earlier unused test import failed compilation and was corrected;
its evidence remains preserved. Deadline integration subsequently passed all
HTTP races (3.644s), endpoint/kernel consumer races (1.076s/1.122s) and vet. Its
gate ended zero after 175.5s, with 1140.8MiB peak and 9.21GiB free. The real TCP
consumer proves deadline propagation and the correlated 408 response.

Twenty-eight tested Go sources and three new consumer language probes in one
additional test file were published after input/baseline fingerprint checks.
The compiler catalogue now contains 533 cases. Canonical generation, consumer
checks and actual gopls probes follow publication; full repository regression
acceptance remains required for these changes. Milestone 08, required milestones
09–24, the default-compiler resource gate and final whole-framework audit remain
outstanding.


### Milestone 08 endpoint and deadline canonical acceptance

Canonical generation produced no changed files. Formatting, all three freshness
targets, documentation checks and four HTTP consumer race packages passed. All
six new compiler-rejection cases ran and passed; the catalogue contains 533
cases. All three actual gopls probes ran and passed (16.362s), retaining
concrete endpoint handler/input/URL types through completion, hover and definition.
The 29 published source fingerprints remained unchanged.

The gate ended zero after 200.4s wall, with 1170.7MiB peak child RSS and
8.41GiB free. This establishes the canonical consumer experience for the
published endpoint/deadline changes. Full repository regression follows.
Milestone 08, required milestones 09–24, default-compiler resource acceptance and
the final whole-framework verification/re-audit remain outstanding.


### Milestone 08 typed endpoints and request deadlines full acceptance

The complete `make verify` run passed on the repository-selected Go toolchain
with the existing constrained VM compiler profile. It covered 49 framework
packages, 52 consumer packages and 533 required compiler-rejection cases.
Real PostgreSQL, actual gopls (974.372s), the complete
generator suite (728.999s), formatting, vet, all three
generated-output freshness targets and documentation checks passed. The final
source fingerprint matched. The run ended zero after 3606.4s wall with peak
child RSS 3560.3MiB and 9.57GiB free.

This completes regression acceptance of the published typed endpoints and
request deadlines, supplementing their focused race, generation, consumer and
actual-editor checks. Validation remains a separate unpublished candidate and
is not covered by this regression. Milestone 08, required milestones 09–24,
default-compiler resource acceptance and the final whole-framework
verification/re-audit remain outstanding.


### Milestone 08 typed validation focused acceptance

The [validation guide](../docs/guides/validation.md) documents the typed rule
engine, generated DTO validation fields and HTTP integration now in the working
checkout. Runtime and contract/HTTP race tests passed, along with focused
generator acceptance, real-HTTP consumer tests, five consumer race packages,
ten required compiler-rejection cases, affected vet, all three generated-output
freshness targets and documentation checks. The generator updated the three
consumer DTO files deterministically. The focused run ended zero after 259.0s
with peak child RSS 1161.7MiB and 8.38GiB free.

An earlier overlay-only attempt could not run vet because the new package
directories did not physically exist. The implementation was applied to the
working checkout and checked there; this was a check-setup failure rather than
a passing validation gate. Editor probes and broader regression remain pending.
Additional rule families, full validation parity, the rest of milestone 08 and
required milestones 09–24 remain outstanding, along with the final framework
verification/re-audit.


### Milestone 08 validation editor and compiler acceptance

All fourteen added validation compiler-rejection cases passed, bringing the
consumer catalogue to 547 cases. Three actual-gopls probes passed completion,
hover and definition checks for generated validation fields, typed rule binding
and HTTP validation registration in 15.288s. A final formatting check identified
seven unformatted scratch files; those were formatted and the remaining
formatting, three freshness targets and documentation checks passed separately.
The final handwritten-source fingerprints match all 45 affected sources.

Full `make verify` is now running with real PostgreSQL, actual gopls and the
existing constrained compiler profile. No full-regression outcome for validation
is claimed yet. Additional validation rules and the rest of the roadmap remain
required.


### Milestone 08 typed validation full acceptance

The complete `make verify` run passed on the repository-selected Go toolchain
with the existing constrained VM compiler profile. It covered 51 framework
packages, 54 consumer packages and 547 required compiler-rejection cases.
Real PostgreSQL, actual gopls (992.981s), the complete
generator suite (745.685s), formatting, vet, all three
generated-output freshness targets and documentation checks passed. The final
source fingerprint matched. The run ended zero after 3687.6s wall with peak
child RSS 3561.4MiB and 9.52GiB free.

This completes regression acceptance of the published typed validation engine,
generated DTO fields and HTTP integration, supplementing their focused races,
generation, consumer and actual-editor checks. Additional rule families are a
separate draft and are not covered by this result. Milestone 08, required
milestones 09–24, default-compiler resource acceptance and the final framework
verification/re-audit remain outstanding.


### Milestone 08 validation rule expansion focused acceptance

The rule expansion adds typed conditional control flow, membership and enum
descriptors, collection limits/distinctness, strict absence, pointer rules and
common string formats. Native scalar comparisons enforce finite values and text
bounds. Metadata marks custom scalar/container encodings and base64 bytes where
native validation cannot promise equivalent wire behavior; the existing value
encoder reuses shared codec discovery. Focused validation/jsonshape/value/contract/
HTTP and independent consumer races passed, along with canonical generation,
seven new compiler-rejection cases, two actual-gopls probes, vet, formatting,
three freshness targets and docs. The initial run ended one after
665.2s because one editor probe searched for
an expression absent from its fixture. The corrected probe and remaining gates
ended zero after 214.5s. Runtime and consumer
sources remained unchanged. Both original results are preserved. Their combined
wall time was 879.7s, peak child RSS
1162.6MiB and final free space 12.09GiB.

The adjacent addition completes prefix/suffix and character rules, shared explicit
timezone loading and same-type temporal bounds/related fields. Calendar ordering
and UTC instant ordering preserve nanoseconds and their different zero semantics.
Temporal/validation and affected query races, generated preferences/window
consumers, four new compiler-rejection cases, two actual-gopls probes, vet,
formatting, three freshness targets and docs passed. It ended zero after
472.5s wall, peak 3671.8MiB and
9.89GiB free.

The combined handwritten source fingerprints match both final focused inputs.
The catalogue now contains 558 compiler-rejection cases; full catalogue and
repository regression verification remain required for this combined addition.
The preceding 547-case full result does not cover it. Required/prohibited and
empty-state convenience, wire presence, collection helpers, advisory database
rules, file validation and the other HTTP work remain. Milestone 08, required
09–24, default-compiler resource acceptance and the final framework verification/
re-audit remain outstanding.


### Milestone 08 validation expansion full acceptance

The combined validation expansion passed complete `make verify` on Go 1.27.1
with the existing constrained VM compiler profile. All 51 framework packages,
55 consumer packages and 558 compiler-rejection cases passed, including required
PostgreSQL, actual gopls (1035.806s), the complete generator
suite (695.386s), formatting, vet, three generation
freshness targets and documentation checks. Final source fingerprints matched.
The run ended zero after 3630.6s wall, with peak child RSS 3572.7MiB and
11.80GiB free. This covers both adjacent rule additions and supplements their
focused race, consumer, compiler and editor evidence; their earlier failed
editor anchor and corrected continuation remain recorded.

Required/prohibited and empty-state convenience remain a separate draft and are
not covered by this full result. Wire presence, field labels, collection helpers,
advisory database rules, file validation and the rest of HTTP still remain.
Milestone 08, required milestones 09–24, default-compiler resource acceptance and
the final whole-framework verification/re-audit remain outstanding.


### Milestone 08 presence rules and field labels focused acceptance

Typed required/prohibited rules and empty-content checks preserve omitted, null, blank, zero and false states. Field display labels retain exact wire paths and share runtime/contract metadata. Focused runtime and consumer races, four compiler rejections, two actual-gopls probes, vet, formatting, freshness and documentation checks passed. Combined full regression remains required.

- `milestone08-validation-presence`: 498.4s wall, 1159.4MiB peak child RSS, 9.58GiB free.
- `milestone08-validation-labels`: 127.4s wall, 477.1MiB peak child RSS, 9.05GiB free.

Source fingerprints matched for both changes. The earlier validation expansion
full regression does not cover these later changes. Advisory database validation
remains an isolated draft. Milestone 08 and required milestones 09–24 remain open,
along with production resource acceptance and the final framework-wide audit.


### Milestone 08 advisory model validation focused acceptance

Advisory model validation reuses typed stored fields, query scopes and parameterized existence execution. It preserves exact decimals, natural keys and explicit soft-deletion visibility; typed update exclusions do not reserve values or replace constraints. Focused runtime/consumer races, real PostgreSQL, four compiler rejections, one actual-gopls probe, vet, formatting, freshness and documentation checks passed. Combined full regression remains required.

The focused run ended zero after 589.9s wall, with 1149.7MiB peak child
RSS and 12.77GiB free. The compiler catalogue now contains 566 distinct cases.
The merged presence, label and lookup source fingerprints matched. Middleware
composition remains an isolated draft. Milestone 08, required milestones 09–24,
production compiler capacity and the final framework-wide audit remain open.


### Milestone 08 middleware composition focused acceptance

Typed middleware declarations compose native HTTP wrappers in explicit parent/child/route order, retain matched-route metadata before decoding and preserve native writer capabilities. Duplicate IDs and constructor failures reject assembly. HTTP/consumer races, one compiler rejection, one actual-gopls probe, vet, formatting, freshness and documentation checks passed. Combined full regression remains required.

The focused run ended zero after 285.1s wall, with 1135.6MiB peak child
RSS and 11.61GiB free. The compiler catalogue now contains 567 distinct cases.
Merged source fingerprints matched across presence, labels, advisory lookups and
middleware composition. A combined full regression remains required. Built-in
transport policies and the rest of milestone 08, required milestones 09–24 and
the final framework-wide verification/re-audit remain open.


### Milestone 08 middleware editor-expression recovery

The first combined full run stopped at actual-gopls acceptance because the
`typed-validation-rule-binding` probe still expected `Email.Rules` after the
consumer added `Email.WithLabel(...).Rules`. Completed runtime packages and
source fingerprints passed, but that run was not complete repository acceptance.
The failed run and the dependent queues' guarded stops remain preserved.

The probe now follows the typed labeled field. One shared probe catalogue also
checks all consumer expressions before any real-gopls requests, so stale source
locations fail promptly. Focused actual-gopls completion/hover/definition checks
for generated fields, labeled rule binding and HTTP validation passed, followed
by vet, formatting, three generation freshness checks and documentation checks.
The recovery run ended zero after 26.1s wall, with 126.0MiB peak child RSS
and 10.90GiB free. Dependent CORS/proxy baselines were rebased only to these
checked editor-test sources. Complete repository verification must still finish;
this targeted recovery is not a substitute for that gate or the final audit.


### Milestone 08 presence, database validation and middleware full acceptance

Complete `make verify` passed with required PostgreSQL and actual gopls on the
existing Go 1.27.1 constrained VM compiler profile. All 52 framework packages,
57 consumer packages and 567 compiler-rejection cases passed. The actual-gopls
suite reported 1027.197s and the complete generator suite reported
673.840s. Formatting, vet, three generation freshness
targets, documentation checks and final source fingerprints passed. PostgreSQL
reported a Go test cache hit; cached results are recorded explicitly
and do not claim a new database execution. The run
ended zero after 3421.2s wall, with 3551.0MiB peak child RSS and 12.73GiB free.

This combined gate covers required/prohibited and empty-state rules, display
labels, advisory typed existence/uniqueness validation and explicit middleware
composition. Their earlier focused race, PostgreSQL, compiler and language
checks remain recorded above. CORS and other built-in transport policies are
not covered by this run. The rest of milestone 08, required milestones 09–24,
default-compiler resource acceptance and the final whole-framework verification
and re-audit remain outstanding.


### Milestone 08 CORS focused acceptance

Typed CORS configuration snapshots origin, method and header policies into shared middleware assembly. Bounded preflights, credential rules, cache variation, shared errors and native writer capabilities passed HTTP/consumer races, targeted header fuzzing, two compiler rejections, one actual-gopls probe, vet, formatting, freshness and documentation checks. CORS full regression remains required.

The focused run ended zero after 346.8s wall, with 1134.0MiB peak child RSS
and 11.46GiB free. The compiler catalogue now contains 569 distinct cases.
Source fingerprints matched across the preceding validation/middleware work and
the CORS addition. Its independent consumer covers an actual generated PATCH
endpoint, global preflight interception and structured missing-route responses.
The policy introduces no new dependency and no implicit authentication state.
CORS full regression, remaining milestone 08 work, required milestones 09–24,
default-compiler capacity and the final framework-wide audit remain outstanding.


### Milestone 08 CORS full acceptance

Complete `make verify` passed with required PostgreSQL and actual gopls on the
existing Go 1.27.1 constrained VM compiler profile. All 52 framework packages,
58 consumer packages and 569 compiler-rejection cases passed. The actual-gopls
suite reported 1075.526s and the generator suite reported
718.331s. Formatting, vet, three generation freshness
targets, documentation checks and final source fingerprints passed. PostgreSQL
reported 1.327s; cached results are recorded explicitly
and do not claim a new database execution. The run
ended zero after 3731.4s wall, with 3544.9MiB peak child RSS and 12.49GiB free.

This completes the CORS addition's acceptance alongside its focused concurrency,
HTTP consumer, bounded-parser fuzz and compiler/editor evidence. Trusted proxies,
remaining transport policies and the rest of milestone 08 still remain. Required
milestones 09–24, default-compiler resource acceptance and the final complete
framework verification/re-audit are outstanding.


### Milestone 08 trusted-proxy focused acceptance

Trusted proxy middleware uses explicit typed peer networks and ordered header sources. Bounded IPv4/IPv6 forwarding chains stop at untrusted or unknown hops and enrich shared client attribution without rewriting native transport state. HTTP/consumer races, targeted parser fuzzing, two compiler rejections, two actual-gopls probes, vet, formatting, freshness and documentation checks passed. Trusted-proxy full regression remains required.

The focused run ended zero after 388.0s wall, with 1151.2MiB peak child RSS
and 11.01GiB free. The compiler catalogue now contains 571 distinct cases.
Source fingerprints matched across the preceding middleware work and the proxy
addition. Independent generated-endpoint HTTP acceptance covers spoofed leftmost
values and first-present header priority before the concrete service executes.
No additional dependency or default CDN trust is introduced. Unknown forwarding
nodes stop traversal; the nearest known address may therefore remain a proxy.
Public URL/scheme/authority policy is separate from client-IP attribution.
Trusted-proxy full regression, remaining milestone 08 work, required milestones
09–24, default-compiler capacity and the final framework audit remain outstanding.


### Milestone 08 trusted-proxy full acceptance

Complete `make verify` passed with required PostgreSQL and actual gopls on the
existing Go 1.27.1 constrained VM compiler profile. All 52 framework packages,
59 consumer packages and 571 compiler-rejection cases passed. The actual-gopls
suite reported 1078.092s and the generator suite reported
709.836s. Formatting, vet, three generation freshness
targets, documentation checks and final source fingerprints passed. PostgreSQL
reported 1.326s; cached results are recorded explicitly
and do not claim a new database execution. The run
ended zero after 3730.3s wall, with 3566.6MiB peak child RSS and 11.80GiB free.

This completes client-IP trusted-proxy acceptance alongside focused concurrency,
HTTP consumer, bounded-parser fuzz and compiler/editor evidence. Public scheme/
authority policy, security headers, remaining transport policies and the rest of
milestone 08 still remain. Required
milestones 09–24, default-compiler resource acceptance and the final complete
framework verification/re-audit are outstanding.


### Milestone 08 security-header core focused acceptance

Security-header core supplies typed frame/referrer policies, explicit native-TLS HSTS, bounded custom header values and copied response defaults while preserving native writer capabilities. HTTP/TLS consumer and runtime races, two compiler rejections, two actual-gopls probes, vet, formatting, freshness and documentation checks passed. Structured CSP/nonce and combined security-header full regression remain required.

The focused run ended zero after 324.4s wall, with 1161.7MiB peak child RSS
and 10.44GiB free. The compiler catalogue now contains 573 distinct cases.
Source fingerprints matched. The independent consumer exercises both ordinary
HTTP and native TLS with an existing generated endpoint and structured fallback.
HSTS does not infer TLS from untrusted forwarding headers. This core supplies
response defaults, not a filter over committed responses; downstream native
code may deliberately override them. CSP/nonce, trusted public-origin handling,
the combined security-header gate and the rest of milestone 08 remain required.
Milestones 09–24 and the final full framework verification/re-audit remain open.


### Milestone 08 typed CSP and nonce focused acceptance

Typed source/hash/sandbox declarations, independent enforced/report-only policies,
bounded grammar validation and per-request cryptographic nonces passed focused
HTTP and consumer races. A native HTML consumer verified its nonce against both
response headers over real HTTP, with new values on separate requests. Nested
policies share one request nonce. Static policies preserve cache headers; nonce
policies set no-store. Foundry preserves native writer capabilities and does not
rewrite HTML. Three compiler rejections, two actual-gopls probes, bounded source
fuzzing, vet, formatting, three freshness targets and documentation checks passed.

The run ended zero after 294.5s wall, with 1004.1MiB peak child RSS and 9.23GiB
free. The compiler catalogue contains 576 distinct cases. Source fingerprints
matched. Combined security-header full regression remains required; this focused
result does not close milestone 08. Trusted public-origin handling, remaining
HTTP scope, required milestones 09–24, default-compiler resource acceptance and
the final complete framework verification/re-audit remain outstanding.


### Milestone 08 security headers and CSP full acceptance

Complete `make verify` passed with required PostgreSQL and actual gopls on the
existing constrained VM compiler profile. All 52 framework packages, 60 consumer
packages and 576 compiler-rejection cases passed. Actual gopls reported
1109.278s and the generator suite reported
688.124s. Formatting, vet, three generation freshness
targets, documentation checks and final source fingerprints passed. PostgreSQL
reported 1.331s; cached results are recorded explicitly
and do not claim new database execution. The run ended zero after 3847.2s wall,
with 3555.0MiB peak child RSS and 11.23GiB free.

This supplements focused core security-header and CSP runtime/consumer races,
source grammar fuzzing, compiler rejections and actual editor acceptance. Native
TLS HSTS and explicit security defaults, typed policies, hashes, report-only
configuration and request nonce handling are covered. Trusted public scheme/
authority handling and the remaining milestone 08 work still remain. Required
milestones 09–24, default-compiler resource acceptance and the final complete
framework verification/re-audit remain outstanding.


### Milestone 08 public origins and proxy HTTPS focused acceptance

Approved public origins, typed proxy origin sources and canonical link generation passed focused runtime/consumer/compiler/editor acceptance and bounded URL fuzzing. HSTS now recognizes explicitly trusted public HTTPS without confusing a configured URL base or internal TLS with the incoming public scheme. Combined transport full regression remains required.

TrustedProxy reuses its existing peer policy and the shared Forwarded parser for
explicit scheme/authority declarations. RFC Forwarded selects both values from
one trusted boundary element; legacy pairs require overwritten single fields.
Approved exact origins own request link generation; a canonical alias changes
generated URLs without changing transport security. Native request fields,
writer, attribution and cancellation are preserved. A real HTTP consumer invokes
an existing typed endpoint and builds its public link from generated path/query
arguments inside the concrete service.

HTTP/consumer races, two compiler rejections, two actual-gopls probes, bounded URL
fuzzing, vet, formatting, three freshness targets and documentation checks passed.
The run ended zero after 389.9s wall, with 1149.0MiB peak child RSS and 9.70GiB
free. All source fingerprints matched; the compiler catalogue has 578 cases.
Combined transport full regression and the rest of milestone 08 remain required.
Required milestones 09–24, default-compiler resource acceptance and the final
complete framework verification/re-audit are outstanding.


### Milestone 08 typed cookies and signing focused acceptance

Typed cookies and signed cookies passed focused runtime/consumer/compiler/editor acceptance and bounded cookie-input fuzzing. Scalar codecs preserve named values, model IDs and enums; scoped deletion, duplicate detection, expiry, key rotation and verification before domain decoding are covered. Combined transport full regression remains required.

The native TLS consumer uses a real cookie jar, model-owned IDs, a named locale
and generated enum values. Runtime checks cover absence versus empty, quoting,
scope ownership/removal, bounds, prefix/security policies, codec failures,
cancellation, concurrency, HMAC scope binding, tampering, key rotation/removal
and exact expiry. Authentication and encryption are separate capabilities.
Scalar conversion and native cookie serialization reuse existing contracts.

The run ended zero after 282.8s wall, with 999.2MiB peak child RSS and 8.52GiB
free. Source fingerprints matched; the compiler catalogue contains 582 cases.
Four compiler rejections, two actual-gopls probes, runtime and six-consumer races,
bounded input fuzzing, vet, formatting, three freshness targets and documentation
checks passed. Combined transport regression, signed URLs, compression, remaining
HTTP work, milestones 09–24 and final framework verification/re-audit remain open.


### Milestone 08 signed URLs focused acceptance

Typed signed routes and endpoints passed focused runtime/consumer/compiler/editor acceptance and bounded verification fuzzing. Temporary URLs bind exact escaped input, actual approved origin, route and method; key rotation, expiry, duplicate rejection and verification before domain decoding are covered. Their combined transport full regression remains required.

The independent consumer composes existing generated paths, model IDs, query
fields and response DTOs. Native HTTP requests exercise temporary previews,
raw route paths, replay until expiry and the shared 403 envelope. Runtime checks
cover origin/method/route/pattern binding, exact escaped bytes, duplicate and
encoded reserved parameters, bounded parsing, clock ownership, key rotation,
cookie-purpose separation, proxy admission and owned contract metadata.
Shared signing time, keys and MAC logic avoid parallel implementations.

The run ended zero after 598.7s wall, with 1163.5MiB peak child RSS and 10.83GiB
free. Source fingerprints matched; the compiler catalogue contains 586 cases.
Four compiler rejections, two actual-gopls probes, runtime and seven-consumer
races, bounded input fuzzing, vet, formatting, three freshness targets and
documentation checks passed. Combined transport regression, compression,
remaining HTTP work, milestones 09–24 and final framework verification/re-audit
remain open.


### Milestone 08 JSON and query presence acceptance

JSON/query presence acceptance passed through real HTTP endpoints using generated DTOs and typed rules. Omission, nullable input, blank text, collections, zero and false preserve domain values; invalid representations, conditional triggers and source-specific errors are covered. Multipart presence remains with file transport.

All six new acceptance test groups passed, alongside the existing validation
consumer tests under race detection, consumer vet, formatting, three generation
freshness targets and documentation checks. The run ended zero after 7.2s
wall, with 175.5MiB peak child RSS and 10.82GiB free. The source fingerprints
matched. No new public API or compiler-rejection case is claimed; the existing
catalogue remains at 586 cases.

The matrix exercises real HTTP clients and the existing generated DTO, body
descriptor, query codecs, typed rules and endpoint error boundary. Handlers
receive unchanged values only on accepted input. Unknown submitted names are
redacted; known fields retain precise body/query and collection-item paths.

Multipart presence/file transport, scalar contract metadata, automatic HTTP
pagination and the rest of milestone 08 remain required. The subsequent
combined transport gate, milestones 09–24 and final framework verification
and re-audit remain outstanding.


### Milestone 08 typed parameter scalar contracts

Typed path/query scalar metadata passed focused runtime, fresh-generation, consumer, compiler and actual-gopls acceptance. Native widths, UUID identities, shared decimal/temporal formats, enum membership and copied metadata use the same typed codecs as runtime execution. Custom codec descriptions retain their concrete Go value type. Combined transport full regression remains required.

Fresh local-enum generation and independent imported-enum consumers exercise
the same constructors. The custom TrackingCode consumer proves that metadata
preserves domain parsing and typed Optional values. Unknown custom-codec
metadata remains explicit rather than an inferred schema. This is a metadata
foundation for milestone 21, not a claim of delivered SDK/OpenAPI generation.

Contract/HTTP races, focused fresh-generation behavior, six consumer package
races, three compiler rejections, two actual-gopls probes, vet, formatting,
canonical generation, three freshness targets and documentation checks passed.
The run ended zero after 1037.1s wall, with 1155.8MiB peak child RSS and
12.37GiB free. Handwritten and generated fingerprints matched; the compiler
catalogue now contains 589 cases.

The combined transport gate, remaining custom transport contracts, automatic
pagination/model binding, file transport and complete milestone 08 acceptance
remain required. Milestones 09–24 and final framework verification/re-audit
remain outstanding.


### Milestone 08 native custom JSON value contracts

Native custom JSON value contracts passed focused runtime, fresh-generation, consumer, compiler and actual-gopls acceptance. Generated DTOs reuse typed JSONContract methods, shared scalar declarations and owned normalized schemas. Incompatible types, conflicting definitions and factory failures reject construction. Combined transport full regression remains required.

The run ended zero after 375.5s wall, with 1147.6MiB peak child RSS and 10.63GiB free. Handwritten and generated fingerprints matched. The catalogue now contains 591 compiler cases. Contract/HTTP and seven consumer package races, focused fresh generation, two compiler rejections, two actual-gopls probes, vet, formatting, canonical generation, three freshness targets and documentation checks passed.

This increment preserves existing native decoding. JSONFrom-only decoder protocols, typed map-key contracts and collision handling, main-package URL/schema identity agreement, pagination/model binding, file transport and full milestone 08 acceptance remain required. Milestones 09–24 and final framework verification/re-audit remain outstanding.


### Milestone 08 typed JSON map-key contracts

Typed JSON map-key contracts passed focused runtime, fresh-generation, consumer, compiler and actual-gopls acceptance. Generated maps retain native key types, integer widths, enum membership, model ownership and custom text contracts. Canonicality and identity checks precede DTO hydration; response output follows the same rules. Combined transport full regression remains required.

The run ended zero after 1070.7s wall, with 1162.2MiB peak child RSS and 12.14GiB free. Handwritten and generated fingerprints matched. The catalogue contains 594 compiler cases. Value/contract/HTTP and seven consumer package races, focused fresh generation, three compiler rejections, two actual-gopls probes, vet, formatting, canonical generation, three freshness targets and documentation checks passed.

The wrong-map-owner example was rejected as intended in the first run, but the assertion expected different compiler wording. The original failure is preserved in failed-map-diagnostic-01; its successful runtime/generation/consumer prefix was reused only after source and output hash checks. The corrected compiler assertions and all remaining checks ran afresh. Wall time above is cumulative across that attempt and targeted recovery.

JSONFrom-only decoder protocols, main-package URL/schema identity agreement, pagination/model binding, file transport and full milestone 08 acceptance remain required. Milestones 09–24 and final framework verification/re-audit remain outstanding.


### Milestone 08 shared transport source identities

Generated URL source identities passed focused runtime, executable, consumer, compiler and actual-gopls acceptance. A real main executable reproduced the previous URL/DTO identity mismatch before the fix. Generated URL, JSON value and map-key metadata now share source type names, including model-ID type arguments, while preserving native codecs and scalar shape. Combined transport full regression remains required.

The run ended zero after 494.9s wall, with 1144.2MiB peak child RSS and 10.04GiB free. Handwritten and generated fingerprints matched. The catalogue contains 595 compiler cases. HTTP and seven consumer package races, focused fresh generation, one compiler rejection, one actual-gopls probe, vet, formatting, canonical generation, three freshness targets and documentation checks passed.

JSONFrom-only decoder protocols, pagination/model binding, file transport and full milestone 08 acceptance remain required. Milestones 09–24 and final framework verification/re-audit remain outstanding.


### Milestone 08 native streaming JSON values

Native streaming JSON value contracts passed focused runtime, generation, consumer, compiler and actual-gopls acceptance. Typed JSONContract methods now admit native JSONTo/JSONFrom codecs through shared capability checks. Native precedence, fallback, singular consumption, exact numbers, callback ownership and partial-value rejection are covered. Combined transport full regression remains required.

The run ended zero after 413.8s wall, with 975.4MiB peak child RSS and 8.31GiB free. Handwritten and generated fingerprints matched. The catalogue contains 595 compiler cases. Contract/HTTP and seven consumer package races, focused fresh generation, two existing compiler rejections, one actual-gopls probe, vet, formatting, canonical generation, three freshness targets and documentation checks passed.

Typed custom HTTP errors, pagination/model binding, file transport and full milestone 08 acceptance remain required. Milestones 09–24 and final framework verification/re-audit remain outstanding.


### Milestone 08 typed application HTTP errors

Typed application HTTP errors passed focused runtime, consumer, compiler and actual-gopls acceptance. Immutable declarations own public code, status and message metadata; typed endpoints share those declarations with response classification and contract inspection. Private causes retain ordinary Go error identity. Invalid declarations, undeclared failures and conflicting catalog entries are covered. Combined transport full regression remains required.

The run ended zero after 778.2s wall, with 1158.2MiB peak child RSS and 11.29GiB free. Handwritten and generated fingerprints matched. The catalogue contains 597 compiler cases. HTTP and seven consumer package races, two compiler rejections, one actual-gopls probe, vet, formatting, canonical generation, three freshness targets and documentation checks passed.

Automatic HTTP pagination/model binding, file transport, remaining middleware and full milestone 08 acceptance remain required. Milestones 09–24 and final framework verification/re-audit remain outstanding.


### Milestone 08 typed query composition and defaults

Typed query composition and defaults passed focused runtime, consumer, compiler and actual-gopls acceptance. Generated filters can be embedded and combined without duplicating parsing or scalar metadata. Typed defaults preserve omission, explicit zero/false/empty values and owned per-request results; canonical default URL metadata shares runtime declarations. Combined transport full regression remains required.

The run ended zero after 297.9s wall, with 990.5MiB peak child RSS and 10.04GiB free. Handwritten and generated fingerprints matched. The catalogue contains 600 compiler cases. HTTP and seven consumer package races, three compiler rejections, one actual-gopls probe, vet, formatting, three generation-freshness targets and documentation checks passed.

Automatic HTTP pagination/model binding, file transport, remaining middleware and full milestone 08 acceptance remain required. Milestones 09–24 and final framework verification/re-audit remain outstanding.

### Milestone 08 typed cursor HTTP pagination focused acceptance

Typed cursor HTTP pagination passed focused runtime, consumer, compiler, real-gopls and freshness acceptance. It preserves source-owned cursor positions through explicit DTO mapping and reuses the shared endpoint, validation, codec and URL contracts. Invalid cursor input remains distinct from server/codec failures. Combined transport full regression remains required.

Canonical acceptance completed in 692.5s, peak child RSS 3713.1MiB and 8.96GiB free. All 26 applied Go source hashes and 102 generated Go file hashes were preserved. Full query/codec races, HTTP/pagination/validation races, four consumer families, nine focused compiler-rejection cases, four actual-gopls probes, the generated main executable, affected vet, formatting, three generation-freshness targets and documentation checks passed. The compiler-rejection catalogue contains 609 cases; this focused run executed nine. PostgreSQL opt-in tests were not enabled for this slice.

Model binding, file transport, remaining middleware and full milestone 08 acceptance remain required. Milestones 09–24 and final framework verification/re-audit remain outstanding.

### Milestone 08 typed route-model binding focused acceptance

Typed route-model binding passed focused runtime, generated-consumer, compiler and real-gopls acceptance, plus isolated PostgreSQL tests. Generated primary-key queries retain exact key/model types, query scopes, soft-delete visibility, eager loading and retrieval hooks. The adapter resolves once per request and preserves explicit transport DTOs. Combined transport full regression remains required.

The runtime/consumer/editor gate completed in 638.0s; PostgreSQL follow-up completed in 477.2s. Unchanged accepted source hashes were verified before reusing earlier focused evidence, and 14 exact Go files were promoted after both gates passed. Five new compiler cases and two real-gopls probes passed; the catalogue now contains 614 cases. Native HTTP tests distinguish malformed input, absent models and infrastructure/hook failures. PostgreSQL tests prove UUID, numeric and string keys, parent scopes, soft-delete visibility, eager relations and retrieval hooks with stored fields preserved. No generated declarations changed. Full combined transport acceptance, remaining milestone 08, milestones 09–24 and final framework verification/re-audit remain required.

### Milestone 08 combined transport full regression

The complete canonical `make verify` gate passed after the public-origin,
cookie/signing, signed-URL, scalar/JSON contract, application-error, query
composition, pagination and model-binding additions. Root and independent
consumer tests, vet, formatting, all three generation-freshness targets and
documentation checks passed. PostgreSQL was required and actual gopls was enabled;
the consumer compiler-rejection catalogue contained 614 cases. The complete
gopls package passed in 1230.450s and the complete generator package in 676.858s.

The run took 4148.0s, with 3582.9MiB peak child RSS and 11.34GiB free at completion.
All 1689 recorded non-Markdown verification inputs retained their exact hashes.
It used one package per batch and a 30-minute per-package allowance in the
existing constrained VM; individual operation deadlines and all tests remained
enabled. Existing low-memory compiler flags were retained, so default-compiler
resource hardening remains part of the outstanding framework work.

This acceptance covers the canonical transport features listed above. Multipart
and download work was outside that snapshot and remains unpublished. The rest
of milestone 08, required milestones 09–24 and the final complete framework
verification/re-audit remain open. Earlier evidence sections retain the status
at their delivery time; this section and the status table own current acceptance.

### Milestone 08 typed multipart uploads focused acceptance

[Typed multipart uploads](../docs/guides/http-uploads.md) now provide generated text/file/JSON field binding, typed file validation, bounded capture, and request-owned cleanup. Focused runtime, generation, consumer, compiler, editor, fuzz and allocation acceptance passed. Canonical publication checks also passed; combined multipart regression remains required.

Handwritten form structs generate descriptors and validation selectors without a
field map. Text fields reuse query codecs; JSON fields retain scalar, collection,
nullable and custom-codec types through the shared JSON field contract. Existing
complete DTO and custom-codec constructor restrictions remain unchanged. Files
retain owned readers and metadata, with cleanup after response writing, failures
and cancellation. Native chunked and signed requests are covered.

The 48 Go sources and guide passed focused acceptance in the independent
workspace. Seven compiler-rejection cases passed, bringing the catalogue to 621.
All three actual-gopls probes passed on an exact-source follow-up after one
bounded shutdown cancellation; the failed evidence was retained. The main gate
took 574.8s and the successful editor/remaining-gate follow-up took 104.9s.
Contract/upload/validation/HTTP races, related generator tests, consumer races,
vet, formatting, all three freshness targets and documentation checks passed.
Multipart framing fuzzing ran 1760 executions. The three-iteration streaming
benchmark measured about 35 KB allocated per capture for both 32 KiB and 8 MiB
inputs; these bounded samples support allocation behavior, not production
throughput claims. Accepted generated artifacts were copied without manual edits.

Canonical acceptance completed in 528.0s with peak child RSS 1021.0 MiB.
All 52 published artifact hashes and 1323 canonical source fingerprints matched.
Runtime/consumer races, seven compiler cases, three actual-gopls probes, affected
vet, formatting, all three freshness targets and documentation checks passed.
These checks used the documented constrained-VM compiler settings; default
compiler resource hardening remains part of final production acceptance.

Combined regression, download/static/SPA work, remaining middleware, milestones
09–24 and final framework verification/re-audit remain required. Storage persistence and attachment consistency arrive in their
own milestones; an uploaded file does not outlive its request.


### Milestone 08 multipart full regression

The complete canonical regression including [typed multipart uploads](../docs/guides/http-uploads.md) passed in 4124.1s, with peak child RSS 3589.9 MiB and 1725 matching source fingerprints. All root and independent consumer tests passed with real PostgreSQL required, the complete actual-gopls package, the 621-case compiler-rejection catalogue, vet/formatting, all three generation-freshness targets and documentation checks. The complete agent package took 1288.223s and generator package took 719.722s.

These checks used the documented constrained-VM compiler settings and one package per batch. Default-compiler resource hardening remains part of production acceptance. Downloads are a separate unpublished implementation under focused acceptance; unseekable streams, static/SPA and remaining middleware still belong to milestone 08. Milestones 09–24 and final complete-framework verification/re-audit remain required.


### Milestone 08 typed downloads focused acceptance

[Typed downloads](../docs/guides/http-downloads.md) now provide a concrete `Download` response, declared media, deferred/local sources, native HEAD/conditional/range behavior, shared 412/416 errors and owned cleanup. The exact 41 Go sources and guide passed focused acceptance in 608.8s with peak child RSS 1162.1 MiB. All root HTTP/model-binding/pagination races and independent document-consumer races passed, including real connection cancellation, unexpected EOF, failed native writers and upload-backed response lifetime. Six new compiler cases passed, bringing the catalogue to 627; all three actual-gopls probes passed.

Range fuzzing completed 9595 executions. The three-iteration full response benchmark allocated 41797 bytes for a 32 KiB representation and 133200 bytes for an 8 MiB representation, with write buffers bounded to 32 KiB. Callback isolation adds per-chunk allocation; these synthetic results do not establish production throughput. Vet, formatting, generation, all three freshness targets and documentation checks passed. No generated artifact changed. An initial unused import after cleanup extraction was corrected and its failed evidence retained.

Canonical acceptance passed in 597.1s with peak child RSS 1152.3 MiB and 1355 matching source fingerprints. Root/consumer races, six compiler cases, three actual-gopls probes, affected vet, formatting, all three freshness targets and documentation checks passed. Combined regression remains required. Unseekable streams, static/SPA, remaining middleware, milestones 09–24 and the final full-framework verification/re-audit remain open.


### Milestone 08 typed streams focused acceptance

[Typed unseekable streams](../docs/guides/http-streams.md) reuse download presentation, source ownership, guarded I/O and endpoint cleanup. `StreamResponse` retains the concrete handler result and media declaration; optional exact lengths and the shared byte ceiling are enforced without buffering the representation. HEAD does not read the body. Before sending the final declared chunk, Foundry verifies EOF so an overlong producer cannot look complete.

The 27 Go source/test artifacts and guide passed focused canonical acceptance in 267.5s with peak child RSS 1183.2 MiB. Full HTTP/model-binding/pagination races and independent stream/download consumer races passed. Six new compiler cases passed, bringing the catalogue to 633. Three new actual-gopls probes and the existing download-value probe passed. Real TCP tests verify short/overlong/failed streams and cancellation; upload-backed response lifetime, writer failure causes and exactly-once cleanup are covered.

Fuzzing completed 18675 executions. Three-iteration synthetic copy benchmarks allocated 40330 bytes for 32 KiB and 132128 bytes for 8 MiB, with writes bounded to 32 KiB. Per-chunk callback allocation remains; these samples do not establish production throughput. Vet, formatting and documentation checks passed. The first run retained a fixture expectation failure: initial data-plus-EOF and later truncation now have separate coverage. Writer error causes are preserved for errors.Is.

The combined full regression including downloads and streams, all generation-freshness targets, remaining milestone08 work, milestones09–24 and final complete-framework verification/re-audit remain required. These checks use the existing constrained-VM compiler settings; default-compiler resource hardening remains outstanding.


### Milestone 08 downloads and streams full acceptance

The complete canonical regression including downloads and unseekable streams
passed in 4232.2s, with peak child RSS 3569.4 MiB and 1775 matching source
fingerprints. All root and independent consumer tests passed with required real
PostgreSQL, the complete actual-gopls package, all 633 compiler-rejection cases,
vet/formatting, three generation-freshness targets and documentation checks.
The agent package completed in 1304.627s and generator package in 717.323s.

These checks used the documented constrained-VM compiler settings and one
package per batch; default-compiler resource hardening remains in production
acceptance. Static/SPA is a separate private implementation now under focused
verification. Automatic ETags for ordinary responses and response compression
also remain required in08. File conditionals do not establish automatic ETag
middleware parity with Rust. Milestones09–24 and final full-framework
verification/re-audit remain open;25 is deferred.

### Milestone 08 static assets and SPA focused acceptance

[Static assets and SPA routing](../docs/guides/http-assets.md) now expose framework-owned local/embedded sources, typed mounts and URLs, explicit MIME policies, native conditional/range transfers and bounded file ownership. SPA fallbacks preserve API/method behavior and isolate application prefixes.

Private acceptance passed in 714.1s with peak child RSS 1145.2 MiB and 11.08 GiB free. All 34 accepted artifacts were published without changing generated output. Root HTTP and independent-consumer races, five new compiler-rejection cases, three actual-gopls probes, affected vet, fuzzing, streaming benchmarks, formatting, all three generation-freshness targets and documentation checks passed. The compiler catalogue now contains 638 cases; this focused run executed the five new cases.

A fake language server reproduced a final-stdout pipe deadlock in the unchanged client. The cleanup fix drains output during exit while retaining the existing five-second shutdown bound. The reproducer, cancellation/bounded-shutdown protocol races and actual-gopls asset probes passed. Cold gopls initially exceeded its shutdown bound under the compiler’s 192 MiB memory setting. Server traces showed active type checking stalled in GC assistance. The private VM runner now gives gopls a separate 768 MiB allowance with GOGC=50, retaining the compiler settings and five-second cleanup bound. A fresh-cache comparison passed at 628.1 MiB peak child RSS. Earlier failed attempts and diagnostic evidence are retained privately.

Canonical full regression of these published changes remains required. Automatic ETags, compression, remaining milestone 08 acceptance, required milestones 09–24 and final framework verification and audit remain outstanding. These checks retain the constrained-VM compiler settings; default-compiler resource hardening remains part of production acceptance.

### Milestone 08 static assets and SPA full acceptance

The complete canonical regression including static assets, SPA routing and language-client cleanup passed in 4131.1s, with peak child RSS 3572.0 MiB and 1799 unchanged source fingerprints. Root and independent consumer checks passed with required real PostgreSQL, the complete actual-gopls package, all 638 compiler-rejection cases, vet/formatting, all three generation-freshness targets and documentation checks.

This run used the documented constrained-VM compiler settings and the separately bounded gopls runner. Default-compiler resource hardening remains part of production acceptance. Automatic ETags and compression remain unpublished work under milestone08; required milestones09–24 and the final complete-framework verification and audit remain open.

### Milestone 08 automatic ETag focused acceptance

[Automatic ETags](../docs/guides/http-etags.md) provide bounded response hashing through the existing typed middleware API. Independent consumer DTOs use explicit getters while stored fields remain unchanged. Native conditions, file/stream/static integration, full-duplex exchange, source failure and nested writer ownership have runtime coverage.

Focused acceptance passed in 689.0s with peak child RSS 1113.6 MiB. All 22 accepted artifacts were published without changing generated output. HTTP and consumer races, one new compiler-rejection case, two actual-gopls probes, affected vet, fuzzing, synthetic resource benchmarks, formatting, all three freshness targets and documentation checks passed. The catalogue now contains 639 compiler cases; this focused run executed the new case.

Three-iteration discard-writer benchmarks allocated 67,965 bytes for 32 KiB and 67,072 bytes for 8 MiB under a 64 KiB test capture ceiling; these measure synthetic allocation bounds, not network throughput. Canonical regression and compression integration remain required. Milestones 09–24, default-compiler resource hardening and final complete-framework verification and audit remain open.

### Milestone 08 automatic ETag full acceptance

The complete canonical regression including automatic ETags and shared response controls passed in 4489.5s, with peak child RSS 3565.7 MiB and 1817 unchanged source fingerprints. Root and independent consumer checks passed with required real PostgreSQL, the complete actual-gopls suite, all 639 compiler-rejection cases, vet/formatting, all three generation-freshness targets and documentation checks. All published ETag Go sources remained unchanged.

These checks used the documented constrained-VM compiler settings and the separately bounded gopls runner. Compression integration and complete milestone 08 acceptance remain required. Milestones 09–24, default-compiler resource hardening and the final complete-framework verification and audit are still open.

### Milestone 08 response compression focused acceptance

[Response compression](../docs/guides/http-compression.md) uses typed gzip/Brotli descriptors, bounded buffering and concurrency, exact encoding negotiation and existing response ownership. Consumer domain services and DTOs stay unchanged. Conditional files, static/SPA responses, automatic ETags for encoded bytes, native controls and interrupted streams have runtime coverage.

Focused acceptance passed in 1195.7s with peak child RSS 1148.2 MiB. HTTP and fourteen consumer package races, two new compiler-rejection cases, two actual-gopls probes, affected vet, negotiation fuzzing, allocation samples, formatting, all three freshness targets and docs passed. The compiler catalog now contains 641 cases; the new cases ran here. The exact 23 source/guide artifacts and four approved module files were published. Existing dependency versions and Go directives were preserved.

Three-iteration discard-writer samples measured gzip: 815736 B/op for 64 KiB and 817314 B/op for 16 MiB; br: 3706992 B/op for 64 KiB and 7775485 B/op for 16 MiB. These are allocation measurements rather than peak-memory or throughput claims. The first focused attempt failed because a new test omitted fmt; the corrected draft passed, and failed evidence was retained.

At this focused acceptance point, canonical full regression was still required. The subsequent [completion review](#milestone-08-completion-review) records the successful native gate. Milestones 09–24 and the final complete-framework verification and audit remain open.

### Milestone 08 completion review

The full canonical regression including response compression passed in 718.9s, with peak child RSS 5477.4 MiB and 1837 matching source fingerprints. Required real PostgreSQL, the complete actual-gopls and generator suites, all 641 compiler-rejection cases, root/independent consumer tests, vet/formatting, all three generation-freshness targets and documentation checks passed. Focused compression acceptance additionally covered affected races, negotiation fuzzing and allocation samples.

The [endpoint consumer](../tests/fixtures/consumer/httpendpoints/endpoints.go), [model binding](../tests/fixtures/consumer/httpmodels/bindings.go), [pagination](../tests/fixtures/consumer/httppagination/pagination.go), [uploads](../tests/fixtures/consumer/httpuploads/forms.go) and [compression](../tests/fixtures/consumer/httpcompression/compression.go) confirm the intended boundary: consumers declare Go DTOs, typed routes and domain services; Foundry owns transport processing and resource lifetimes. Generated field notices and explicit getter-to-DTO mapping preserve stored models. Native HTTP interoperability stays explicit. Nineteen older HTTP guides were corrected and documentation validation passed.

Milestone 08 is complete. Session/CSRF, credential authentication, provider storage, realtime, image processing, localization and client exporters remain in their scheduled milestones. This completion gate ran natively on macOS with the standard Go compiler, GC and CPU settings; the historical VM workarounds were not used. Milestones 09–24 and the final complete-framework verification and audit are still open.

### Milestone 09 typed cache and memory acceptance

The first [cache slice](09-redis-cache-and-coordination.md#first-slice-acceptance) passed canonical native macOS verification in 376.9s with local PostgreSQL, real gopls and 1859 unchanged source fingerprints. Root/consumer behavior, all 648 compiler-rejection cases, 233 consumer editor scenarios plus five field-documentation scenarios, vet/formatting, three freshness targets and docs passed. Focused cache, consumer and editor races and 64,509 key-fuzz executions passed. No new dependency was installed.

The [consumer](../tests/fixtures/consumer/caching/profiles.go) binds model-owned keys and concrete snapshot values, explicitly selecting getters without changing stored model fields. Stores borrow adapter lifecycle; the memory adapter bounds retained entries and bytes. Milestone 09 remains in progress: Redis, additional cache operations and distributed coordination are required. Milestones 10–24 and the final framework-wide verification and audit remain open.

### Milestone 09 local Remember acceptance

The [Remember slice](09-redis-cache-and-coordination.md#local-remember-slice) passed focused cache/consumer/editor races and two new compiler-rejection cases. Full canonical native macOS verification passed in 377.8s with 1864 matching source fingerprints, required local PostgreSQL, all 650 compiler-rejection cases, 234 actual-gopls consumer scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three freshness targets and docs. No new dependency was installed.

Review confirmed independent decoded snapshots, bounded active fills/followers, cooperative owner cancellation, prompt follower cancellation, recursive-context rejection, and cleanup after loader/backend/codec panic or Goexit. A cancellation edge case was fixed so a canceled backend miss cannot start its loader. The [consumer](../tests/fixtures/consumer/caching/profiles.go) retains model-owned IDs and selects read getters when creating snapshots. Concurrent Put/Forget semantics remain explicitly documented; this is local coalescing within one Store.

Milestone 09 remains in progress. Redis integration, tagged invalidation, counters, distributed leases/coalescing, rate limiting and pub/sub are still required. Milestones 10–24 and the final complete-framework verification and audit remain open.

### Milestone 09 typed counter acceptance

The [typed counter slice](09-redis-cache-and-coordination.md#typed-atomic-counters) passed focused cache/consumer/editor races, three new compiler-rejection cases, two actual-gopls probes and 619,028 arithmetic fuzz executions against arbitrary-precision integers. Full canonical native macOS verification passed in 376.9s with 1877 matching source fingerprints, required local PostgreSQL, all 653 compiler-rejection cases, 236 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three freshness targets and documentation checks. No dependency was installed.

The [consumer](../tests/fixtures/consumer/caching/counters.go) retains model-owned member IDs through exact int64 activity counts. Review confirmed one declaration registry and one shared memory entry/expiry/eviction implementation. Increments preserve initial expiry; corrupt data, overflow and capacity rejection leave live data unchanged. Contention tests cover distinct returned increments, and arbitrary-precision fuzzing verifies both arithmetic limits and canonical round trips. The memory adapter's atomic capability is explicit; unsupported adapters fail binding without a read/write fallback.

Milestone 09 remains in progress. Tagged invalidation, Redis integration, leases, distributed coalescing, rate limiting and pub/sub remain required. Milestones 10–24 and the final complete-framework verification and audit are still open.

### Milestone 09 typed tag acceptance

The [tag slice](09-redis-cache-and-coordination.md#typed-tag-invalidation) passed focused cache/consumer/editor races, three new compiler-rejection cases and two actual-gopls probes. Full native macOS verification passed in 541.2s with 1891 matching source inputs, local PostgreSQL, all 656 compiler cases, 238 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three freshness targets and docs. The consumer demonstrates explicit typed domain invalidation and getter-based snapshots.

The approved go-redis v9.22.0 dependency is installed. Redis adapter integration, leases, distributed coalescing, rate limiting and pub/sub remain required before milestone 09 is complete. Milestones 10–24 and the final framework verification and audit remain open.

### Milestone 09 Redis connection and cache acceptance

Full native verification passed in 493.4s with 1905 matching source inputs, required local PostgreSQL and Redis, all 656 compiler cases, 239 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Focused Redis/cache/consumer/editor races and shared memory/Redis contracts also passed.

The [Redis consumer](../tests/fixtures/consumer/caching/redis.go) preserves the existing model-owned profile key and getter-based payload. Foundry owns pure construction, startup, operation/connection limits and shutdown; the consumer uses the same typed cache API. Lost-response fault tests verify that mutations are not retried. Approved dependency versions are aligned in both modules, with earlier versions unchanged.

Redis tag snapshots, leases, distributed coalescing, rate limiting and pub/sub remain required before milestone 09 is complete. Milestones 10–24 and the final complete-framework verification and audit remain open.

### Milestone 09 Redis tag acceptance

Full native verification passed in 327.6s with 1915 matching source inputs, required local PostgreSQL and Redis, all 656 compiler cases, 239 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Shared memory/Redis tag races, Redis failure/cross-client tests, consumer races and three actual-gopls probes passed. No dependency was added.

The [Redis tag capability](09-redis-cache-and-coordination.md#redis-tag-snapshots-and-conditional-operations) preserves the existing model-owned tag/profile consumer API. Metadata batches are atomic; stable addresses and fresh versions prevent generation accumulation and resurrection after metadata loss. Cross-client tests reject a stale Remember owner after a newer value is published. Shared integer handling includes Redis hash-specific corruption errors, found and fixed during acceptance.

Leases, distributed coalescing, rate limiting and pub/sub remain required before milestone 09 is complete. Milestones 10–24 and the final complete-framework verification and audit remain open.

## Milestone 09 typed lease acceptance

Full native verification passed on 1945 matching source inputs with local PostgreSQL and Redis required, all 662 compiler cases, 242 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. The initial full run took 416.7s. After a regression test exposed release hiding an earlier manager cancellation, the corrected final gate took 12.2s: affected lease/Redis/consumer packages reran and 108 unchanged packages reused Go's valid test cache. Fresh focused lease/cache/Redis races also passed on the correction; consumer races, six new compiler rejections and three actual gopls probes passed. No dependency was added.

The [lease guide](../docs/guides/leases.md) and independent consumer establish
model-owned resource keys, finite acquisition, bounded contention waiting,
heartbeat/lost-ownership cancellation and owned cleanup. The shared keyspace
extraction preserves existing cache APIs and physical addresses. Memory never
evicts live owners; Redis performs bounded atomic owner checks and never retries
uncertain mutations. Provider dependencies keep the backend alive until lease
scopes and callbacks finish. Redis authority limitations and resource fencing are
explicit; this is not durable recovery or an exactly-once execution contract.

Milestone 09 remains in progress. Distributed Remember, rate limiting and pub/sub
are still required. Milestones 10–24 and the final framework-wide audit remain open.

## Milestone 09 distributed Remember acceptance

Full native verification passed in 414.1s with 1960 matching source inputs, required local PostgreSQL and Redis, all 665 compiler cases, 243 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Fresh focused lease/cache/Redis races, two-client failure tests, consumer races, three new compiler rejections and one actual gopls constructor probe also passed. No dependency was added.

The [consumer](../tests/fixtures/consumer/caching/distributed.go) preserves its
existing model-owned keys, getter-specific snapshots and typed Remember API.
Construction borrows cache and lease capabilities from the same authority;
shared local fill handling supplies bounds, independent decoding and callback
ownership. Atomic Redis publication validates exact lease binding and current tag
snapshots, rejecting superseded writers even before the client detects lease loss.
Unknown write/release acknowledgements stay errors without retries or claimed
rollback. Unsupported coordination never silently falls back to local loading.

Milestone 09 remains in progress. Rate limiting and pub/sub are still required.
Milestones 10–24 and the final framework-wide verification/audit remain open.

## Milestone 09 typed rate-limit acceptance

Full native verification passed in 465.6s with 1984 matching source inputs, required local PostgreSQL and Redis, all 670 compiler cases, 245 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Fresh focused rate-limit/Redis/HTTP races, consumer races, five new compiler rejections, two actual gopls probes and 2,861,333 timestamp fuzz executions also passed. No dependency was added.

The [guide](../docs/guides/rate-limiting.md) and
[consumer](../tests/fixtures/consumer/limiting/members.go) establish model-owned
quotas for domain work and HTTP. Immutable declarations, explicit borrowed stores,
bounded callback ownership and focused adapters preserve the consumer boundary.
Shared contracts verify weighted admission, exact maximum counts, policy conflicts,
concurrency and cancellation. Redis server time and a single bounded metadata write
with absolute expiry prevent client-clock drift and split counter/TTL mutations.
Denials consume nothing; lost acknowledgements remain errors without retries.
HTTP reuses trusted attribution, native handlers and the existing 429/503 envelopes.

Milestone 09 remains in progress with pub/sub required. Milestones 10–24 and the
complete-framework verification and audit remain open.

## Milestone 09 typed pub/sub acceptance

Full native verification passed in 430.4s with 2012 matching source inputs, required
local PostgreSQL and Redis, 675 compiler cases, 247 consumer editor scenarios plus
five field-documentation scenarios, root/consumer tests, vet/formatting, three
generation-freshness targets and documentation checks. Focused native races and
1,519,877 bounded-queue fuzz executions also passed. No dependency was added.

The [consumer](../tests/fixtures/consumer/messaging/members.go) preserves model-owned
keys and getter-selected event DTOs. Redis owns acknowledged setup, bounded dedicated
subscriptions, heartbeat, explicit loss and draining shutdown. The framework does
not claim durable delivery or silently retry unknown publications. The parity
review identified additional cache invalidation and typed Redis data/command work;
see the [closure inventory](09-redis-cache-and-coordination.md#parity-review-remaining-implemented-rust-capabilities).
Milestone 09 remains open.

## Milestone 09 namespace invalidation acceptance

Full native verification passed in 350.4s with 2018 matching source inputs, required local PostgreSQL and Redis, all 248 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. The existing 675-case compiler suite reused cached results; all 20 affected cache and cross-feature compiler rejection cases were additionally rerun uncached and passed. Fresh complete milestone 09 races and independent consumer races passed. No dependency was added.

The [consumer](../tests/fixtures/consumer/caching/invalidate.go) calls
`store.Invalidate(ctx)` while retaining its typed handles and getter-selected DTOs.
Native memory/Redis stores automatically include one reserved namespace snapshot
in values, tags, counters and cache fills. Rotation reuses the existing atomic
version machinery, rejects old publications and gives fresh calls separate fill
ownership. Stable addresses avoid Forever generation-key accumulation; namespace
metadata loss does not resurrect data. Shared tests also cover full application tag
capacity, failure ownership, exact-key cleanup and lost rotation acknowledgements.

Milestone 09 remains open for existence/expiry/batch operations, typed hash/set
facilities and the scoped command/pipeline/script boundary. Milestones 10–24 and
final whole-framework verification/audit remain required.

## Milestone 09 typed cache entry acceptance

Full native verification passed in 436.6s with 2039 matching source inputs, required
local PostgreSQL and Redis, 680 compiler rejection cases, 251 consumer editor
scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting,
generation freshness and documentation checks. Focused complete milestone 09 races
and independent consumer races passed. No dependency was added.

The [consumer](../tests/fixtures/consumer/caching/entries.go) retains model-owned keys
through `Exists`, `Expire` and bounded atomic `ForgetMany`. Shared namespace/tag
snapshots protect complete batches; validation precedes deletion, expiry preserves
payloads and uncertain acknowledgements return errors without retries. The same
contracts cover memory and Redis, including concurrent batches, maximum dimensions,
corruption, callback ownership and getter-selected DTOs.

Milestone 09 remains open for general Redis data-key/hash/set operations and the
scoped command/pipeline/script boundary. Milestones 10–24 and final framework-wide
verification and audit remain required.

## Milestone 09 typed Redis data acceptance

Full native verification passed in 444.1s with 2067 matching source inputs, required
local PostgreSQL and Redis, 690 compiler rejection cases, 256 consumer editor
scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting,
generation freshness and documentation checks. The full milestone 09 race suite
and focused maximum-bound tests passed. No dependency was added.

The [consumer](../tests/fixtures/consumer/redisdata/members.go) uses typed hash
fields and getter-selected DTO values, plus sets of concrete model IDs. Native
Redis adapters provide missing/null distinctions, owned canonical JSON results,
expiry retention, bounded collections and atomic batch deletion. Tests cover
4096-member and 256-key limits, two clients racing for capacity, corruption,
unknown acknowledgements, pure assembly and callback lifetime. Existing cache
behavior remains green after sharing the expiry command helper.

Milestone 09 remains open for scoped commands, pipelines and scripts, including
general raw-key operations. Milestones 10–24 and final framework-wide verification
and audit remain required.

## Milestone 09 completion and scoped command acceptance

Full native verification passed in 463.5s with 2095 matching source inputs, required
local PostgreSQL and Redis, 702 compiler rejection cases, 262 consumer editor
scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting,
generation freshness and documentation checks. The complete eleven-package milestone
09 race suite passed, along with the new consumer/compiler/editor checks. No dependency
was added beyond the previously approved go-redis adapter.

The [consumer](../tests/fixtures/consumer/rediscommands/commands.go) demonstrates
model-owned keys, concrete command/script results, heterogeneous typed pipeline
receipts and explicit interoperability with typed data keys. Native tests reproduce
Rust's leaderboard, transaction and stream examples and verify error/unknown-outcome
behavior without retries. Immutable requests/replies, shared bounds and callback
ownership passed their edge and maximum-limit tests.

The source/parity review covered every milestone 09 subsystem and the remaining
Redis helper/command inventory. The [owning blueprint](09-redis-cache-and-coordination.md)
records the Go representation differences and completion evidence. Milestone 09 is
complete. Milestones 10–24 and final complete-framework verification/audit remain
required; authentication is next.

### Milestone 10 first authentication slice acceptance

Delivered `auth` with model/key-preserving providers and strategies, immutable
credential proofs, typed guards, bounded callback ownership, request-owned
coalescing and concrete subject/resource policies. HTTP required/optional adapters
reuse the transport pipeline and expose concrete models. Unbound guarded routes
fail registration. Stored identity is checked against the resolved model; custom
getters do not replace credential keys. See the [guide](../docs/guides/authentication.md).

Verified natively on macOS with the pinned Go toolchain and required local
PostgreSQL/Redis: focused race checks pass across auth, HTTP/model binding/pagination
and model packages; the independent consumer and ten new compiler cases pass;
four new real-gopls completion/hover/definition probes pass. Full `make verify`
passed in 651.5 seconds with 2,119 unchanged source inputs, 712 compiler-rejection
cases, 266 editor checks plus field notices, and current generated output.

The review covered isolation of registered guards/providers, concurrent lookup
coalescing, cancellation/closure, partial-model suppression, MFA-pending rejection,
credential bounds, redaction and explicit handler/DTO contracts. Required/optional
HTTP assembly shares one implementation. No dependency was added for this slice.

Milestone 10 remains in progress: sessions, token persistence/rotation, password
and verification flows, lockout/MFA factors, attribution/testing integration and
remaining raw/signed/model-binding composition require their own delivery/review.
The final framework-wide verification and audit remain after milestones 10–24.


### Milestone 10 session persistence acceptance

Delivered `auth/session` with concrete model/key bindings, immutable metadata,
opaque credentials persisted only as hashes, atomic rotation, server-side idle and
absolute expiry, fixed pending-MFA lifetime, remember policy, typed listing and
revocation by secret or model-owned session ID. PostgreSQL is the first authority,
using framework migrations and generated internal models. Applications reuse their
existing model provider; no Actor or application session schema is required.
See the [guide](../docs/guides/sessions.md) and
[independent consumer](../tests/fixtures/consumer/authenticating/sessions.go).

Stable subject locks serialize issuance, touch, rotation and revocation. Reads
recheck their credential locator after locking; expiry is sampled after lock
acquisition. Revocation by ID also requires the matching subject/address. Pruning
is bounded and rechecks candidates under locks. Small subject lock rows remain
for concurrent-operation safety. Lost acknowledgements never cause automatic
mutation retries or return a partial credential. Cookie delivery is a separate
boundary and is not claimed to be atomic with these transactions.

Verified on 2026-09-16 with native macOS Go and local PostgreSQL/Redis:

- Focused auth/session race tests passed, including parallel rotation and issuance,
  revocation isolation, disabled models, expiry while awaiting locks, stored-subject
  corruption, missing migrations, temporary-table shadowing and uncertain writes.
- Independent consumer runtime, five new compiler-rejection cases and three new
  actual-gopls probes passed. Secret parser fuzzing passed 258,696 inputs in a
  three-second bounded run. Callback ownership covers cancellation, panic, Goexit
  and late results without exposing credentials.
- Canonical `make verify` passed in 490.3 seconds with all 717 compiler-rejection
  cases, 269 editor probes plus field notices, four generation-current targets,
  root/consumer behavior and vet, formatting and documentation checks. All 2,144
  source fingerprints matched at completion. No VM, SSH or constrained compiler
  settings were used.
- Consumer and implementation review confirmed stored references remain separate
  from getters, complete records are validated, all session CRUD uses typed queries,
  schema selection preserves the configured tables over temporary names, and
  absolute lifetime constraints measure elapsed time. No new dependency was needed.

The persistence portion of slice 2 is accepted. HTTP cookie publication and CSRF
remain next, followed by token, password, MFA and attribution flows. Milestone 10,
11–24 and the final complete-framework verification/audit remain open.


### Milestone 10 token implementation under the revised cadence

The user changed execution to finish milestone implementation, tests and docs
before its verification/fix cycle. This cadence is recorded in AGENTS.md and the
blueprint index; no acceptance gate is removed. The per-slice full run 19 was
canceled deliberately and is not a success or a runtime-failure claim.

Typed access scopes, token runtime/PostgreSQL refresh/reuse handling and typed
HTTP delivery are written. `http.TokenResponse[M,K]` accepts a model-owned Issued
result, while generated internal DTOs own the exposed JSON schema. Secure POST,
no-store delivery, redacted refresh inputs and the ordinary HTTP error/decoder
pipeline are shared with existing transport behavior. The independent consumer
now includes login, scoped access, refresh replay, current-model eligibility and
revocation. Additional tests cover uncertain writes, callback cleanup, address
isolation, expiry after lock waits and invalid HTTP input/delivery.

The new token tests, compiler fixtures and editor probes have **not been run**.
They await completion of milestone 10, alongside the shared credential/session
refactor regression. The [token guide](../docs/guides/tokens.md) documents this
implementation status. Six generation targets now include the internal auth wire
DTOs. Password hashing/reset/verification, lockout, MFA/recovery, attribution and
remaining composition/review still precede the milestone's acceptance phase.

### Milestone 10 typed password models and login implementation

The password dependency approval has been applied. The user subsequently authorized
needed dependencies without separate approval, provided an existing dependency's
function is not duplicated by another library/repository; AGENTS.md records this.

Typed password fields now generate ordinary model/projection APIs, use one sensitive
codec and retain raw stored values. Sensitive metadata propagates through nullable
and validated codecs, redacts automatic audit fields regardless of column name,
and rejects hash identity/cursor keys. Generated field notices make this behavior
visible in handwritten models and language-tooling output without configuration.
The consumer model declarations generated successfully after correcting an unused
import in hash-only projection emission. This is source generation, not acceptance.

`auth.PasswordLogin` performs model-first verification using the existing provider,
with one login lookup, explicit eligibility and MFA policy, dummy work for missing
or unusable hashes, and conditional rehash. Rehash losers and uncertain writes
return no authority and are not retried. The returned model/key types remain in
`PasswordResult`/`Proof`; credential issuance is a separate transaction boundary.
Tests cover concrete models, typed hash CRUD/projections, audit/cursor protection,
rehash persistence, pending assurance, disabled models, failures and cancellation.
New compiler and field/editor probes are written. **These tests have not run**;
verification remains deferred until milestone 10 implementation is complete.

Reset/email verification, lockout, MFA factor persistence/completion, credential
revocation integration, remaining permissions/attribution and HTTP composition
remain open before that gate. Milestones 11–24 and the final audit remain open.

### Milestone 10 login lockout implementation

Typed `auth/lockout` declarations and `Throttle[K]` retain the exact submitted
credential key. The runtime reuses existing keyspace addressing and bounded
credential callbacks. Redis support reuses Foundry's installed go-redis client,
operation lifecycle and time bounds; no additional dependency was installed.
One atomic metadata update owns failures and lock expiry. A revision protects newer
failures from a late success, and a fresh generation separates expired/reset
windows. Both admission and completion check current lock state. Denials never
renew locks, uncertain mutations are never retried, and there is no Redis-to-memory
fallback. The local adapter is an explicit bounded single-process authority.

`PasswordLogin.WithLockout` shares the login input type and withholds results until
completion succeeds. Existing request-rate middleware independently bounds IP
attempts. The HTTP pipeline reuses its 429/503/401 contracts and adds bounded,
rounded Retry-After for confirmed locks. An optional typed observer receives only
confirmed lock transitions; this is in-process delivery, not a durable outbox.

The [consumer fixture](../tests/fixtures/consumer/passwords/lockout.go) and guide
show composition. Deferred coverage includes shared memory/Redis behavior,
concurrent failures, stale success, explicit reset, expired generations, policy
conflicts, corrupt metadata, server clocks, lost replies, cancellation, observer
failures, no second model lookup, HTTP headers and request-limit independence.
Four compiler-rejection fixtures and three editor probes are written. No tests,
compilation, editor or full freshness checks ran during this continuation; the
milestone-level acceptance remains required.

Reset/email verification, MFA factor persistence/completion, password-change
revocation, remaining permissions/attribution and HTTP composition are still open.
Milestones 11–24 and the final complete-framework verification/audit remain open.


### Milestone 10 recovery implementation

Typed reset and verification flows are written on the shared `auth/challenge`
store. Model and purpose remain compiler-visible, including explicit-conversion
rejection. Stored current-state bindings, provider eligibility and generated
ForUpdate lookup precede mutations. A successful consumption and the model update
share one transaction. A failed action or late expiry rolls back both. Stable
subject locks serialize replacement, single use, revocation and bounded pruning.
PostgreSQL tables/models/migrations and the independent recovering consumer are
written. Shared credential fingerprinting preserves existing address encoding;
Provider.CheckModel adds no lookup or credential proof.

The [guide](../docs/guides/account-recovery.md) records ownership, lock order,
current-value binding limits, hash redaction and explicit migration behavior.
Password reset requires a same-transaction invalidation callback; framework
session/token invalidation integration remains required, and test spies do not
claim that integration. Recovery request DTO/HTTP composition, MFA and durable
email delivery remain in their owning milestones.

Native generation completed (three internal model/projection files; consumer
model output and field documentation). Seven compiler rejection fixtures and
two editor probes are written alongside runtime/provider/PostgreSQL tests for
rollback, cancellation, panic/Goexit, concurrency, stale state, purpose separation,
revocation and expiry. **No new behavior tests, compiler suite, gopls suite or full
verification ran**; all new behavior remains unverified until milestone 10's gate.
Makefile now has seven generation targets, including internal/challengestore.
The full-framework goal and final audit remain active; 01–09 retain historical
acceptance, 10 remains in progress and 11–24 remain open.

Source review also corrected shared credential cancellation handling: it now
preserves an operational/database outcome cause alongside late cancellation,
without disclosing that cause in error text. Written recovery acceptance includes
an after-commit failure plus cancellation: the returned error must retain the
Committed outcome even though no model is published. This shared change remains
part of milestone 10's pending regression.


### Milestone 10 credential-change integration

Session/token `RevokeAllIn` now joins a caller's transaction. Typed revocation
contributions share a provider, reject duplicate names and run in deterministic
order inside one savepoint. Reset uses the group's Invalidate callback. The
PostgreSQL adapters reuse their existing bounded deletion logic, verify exact
pool ownership and restore the prior search path. Database transactions,
connection-scoped transactions and savepoints retain ownership via Tx.BelongsTo.

PasswordModel gains a required typed Lock callback for issuance. Password proofs
carry an immutable current-hash/identity/eligibility/MFA check. Checked creation
locks the model before credential subjects and writes in that transaction;
reset either revokes an earlier issuance or prevents a later stale issuance.
Scope narrowing preserves the check. Unsupported adapters, omitted/repeated
checks and suppressed check errors never publish a credential.

The [consumer](../tests/fixtures/consumer/recovering/credentials.go) now combines
real session/token invalidation with recovery. Written tests cover PostgreSQL
reset/revocation rollback, concurrent issue/refresh, current guard rejection,
fresh-login recovery, schema restoration, foreign-pool rejection, required backend
capabilities, proof checks, scope preservation and transaction ownership. Five
compiler fixtures and three editor probes accompany these changes. Documentation
is in the [credential-change guide](../docs/guides/credential-changes.md).

**These tests are unrun.** No build/test/generation/editor/full verification ran
this continuation; formatting and source review only. Foundation/session/token
changes require the milestone gate. Historical accepted states remain evidence
for their old source, not acceptance of these additions. MFA, recovery transport,
remaining authorization/attribution/helpers/parity work and then full10 verification
remain required. Milestones11–24 and the final framework-wide audit remain open.


### Milestone 10 MFA prerequisites — written, not accepted

Typed TOTP/recovery inputs and encrypted-secret primitives now have independent
consumer declarations and generated DTO contracts. Shared `encryption` owns
bounded AES-256-GCM envelopes, authenticated purpose/record context, retained-key
rotation and redaction. No dependency was added. `Provider.RecheckPassword`
returns a current locked model using the same validation as password issuance,
preserving the original provider declaration and avoiding duplicate lookup.

Native generation emitted the two new consumer DTO descriptors. Unit/security,
consumer/compiler/editor tests are written but have not run; full verification
remains deferred until the remaining milestone 10 implementation and documentation
are finished. Transactional factor storage, enrollment/confirmation, management,
MFA lockout and pending/full credential completion remain open, along with the
other recorded auth integration work. Milestone 10 and the framework goal remain
in progress. The [MFA guide](../docs/guides/mfa.md) details these boundaries.


### Milestone 10 MFA storage and management — written, not accepted

`auth/mfa` now implements model-first enrollment, confirmation, disable,
recovery-code regeneration, key rotation and bounded pending-factor pruning.
Its PostgreSQL adapter uses generated framework models and explicit migrations.
Password/model locking precedes factor and credential locks; a separate typed MFA
throttle completes before protected writes. Factor state, domain mutation and
registered session/token invalidation commit together. No management operation
returns an authenticated proof.

Runtime protocol and real PostgreSQL consumer tests are written for state changes,
rollback, concurrent recovery consumption, late lockout, required policy, pruning,
rotation and after-commit failure. Five compiler cases and two editor probes were
added. Necessary native model generation completed, but no tests or acceptance
suite ran: verification remains deferred until all milestone 10 implementation is
written. Pending/full credential completion and remaining auth/transport/event
integration are open. [The guide](../docs/guides/mfa.md) documents the actual API.

### Milestone 10 MFA completion and typed transport implementation

Single-use pending-session/token completion is written. A typed second-factor
action joins the encrypted factor mutation to credential creation, locks and
checks the current model once, and consumes the pending credential under its
original guard. Factor state and all credential writes share rollback. The
creation adapter rechecks pending expiry before commit; different database pools
are rejected. No reusable full authentication proof escapes.

Browser completion reuses CSRF/cookie staging. Enrollment and recovery-code
responses require secure POST and share the token response's bounded, redacted
preparation boundary. Generated request contracts retain TOTP/recovery/refresh
input distinctions and model-owned enrollment IDs. Consumer routes exercise the
actual public API. New transaction, security, HTTP, compiler and editor tests are
written but have not run. Required generation completed natively; a compiler
import-name collision found during generation was corrected. No new dependency
was installed and no database or service state changed in this continuation.

Milestone 10 remains in implementation. Recovery transport, auth events and
attribution, operational cleanup, remaining permission/test-helper/parity work,
and then the complete acceptance/fix cycle remain required. Milestones 11–24 and
the framework-wide audit are still open.

### Milestone 10 typed recovery transport and request orchestration

Recovery tokens now retain model and purpose through native JSON input and
generated contracts. Named generic identities agree with Go runtime names;
executable model arguments retain source identity using a schema alias. Explicit
credential request protection shares the secret-response secure POST policy.
Consumer reset/verification completion accepts typed bodies, preserves ordinary
CSRF/error contracts, and creates no login credential.

The framework's typed requester coordinates recipient quota, domain lookup,
current-model issuance and post-commit delivery to the stored subject snapshot.
Admitted requests expose no account/delivery outcome; safe diagnostics retain
operational visibility, while callbacks remain bounded and owned until exit.
Public fixtures add IP admission and email validation. Delivery is an explicit
in-process boundary; reliable email/outbox adapters remain separate work.

Required consumer generation succeeded. New native JSON, generic/main contract,
requester, HTTP, PostgreSQL, compiler and editor cases are written and unrun.
No dependency was added, database/service state changed or full gate started.
Milestone 10 remains in implementation; historical email-change invalidation,
security events/attribution, cleanup and remaining parity/permissions/test-helper
work precede the acceptance cycle. Milestones 11–24 and final audit remain open.

## Milestone 10: recovery history, permissions and attribution — written, unverified

Recovery mappings now require `EmailRevision(M) challenge.Revision[M]` alongside
stored Email. Revisions reuse the existing typed UUID implementation and codec;
`BindRevision` rejects zero. The consumer initializes/rotates its revision through
ordinary model hooks, resets verification on address changes, and checks the final
stored changes. Restoring a previous address cannot restore its old link binding.
No challenge lock is acquired from that model write; original lock order remains.
Set-based/raw writes explicitly own equivalent revision maintenance. Password
reset and verification callbacks must preserve the email revision themselves.

`Guard.Origin`/`WithAttribution` capture the verified identity from the existing
scope cache. HTTP required/optional adapters attach it before decoding, preserving
request metadata and leaving anonymous requests anonymous. Audit and event outbox
use the existing attribution context. It carries no reusable execution authority.

`Permission[M]` reuses `Policy[M,struct{}]`; registration, bounded evaluation,
errors and model resolution are shared. HTTP `WithPermissions` validates exact
registration, snapshots requirements, applies all checks after guard/scopes and
before decoding, and exposes owned typed metadata. Domain role relations can be
queried in the callback; no stale role snapshot is added to credentials.

Required native consumer generation succeeded (one refreshed model file).
Behavioral/PostgreSQL/compiler/editor cases are written and **not executed**.
This is implementation progress. Security-event/outbox composition, operational
cleanup policy, signed/model-bound/raw transport composition and the final Rust parity inventory remain before gate 20.


Typed auth test helpers are now written in `testkit/auth`: test-owned production
scopes and concrete-model assertions. They preserve verifier/provider/eligibility,
MFA and policy checks and register scope cleanup. The independent consumer uses
them; unit/consumer/compiler test sources are unverified until the milestone gate.

## Authenticated transport composition — written, unverified

A private `authenticationBinding[M]` now shares validation, immutable requirements,
metadata and middleware between typed endpoints and native routes. Existing public
guard/scope/permission APIs delegate to it. Optional absence discards inherited
model/system/guard provenance while retaining trusted request metadata.

Required/optional endpoints expose `Signed`, retaining concrete or Optional
subjects. `AuthenticatedTransport[P,Q,B,S,R]` is the typed handler capability used
by `modelbinding.BindAuthenticated`; the bound resource remains a separate M.
The ordinary and authenticated model-binding adapters share resolver/validation
logic. Signatures reuse the existing SignedEndpoint and signedRegistration paths.

`RequireRouteAuthentication`/`OptionalRouteAuthentication` supply typed subjects
to native HandleRaw callbacks. Scope and permission requirements use the same
private binding. Signed variants preserve native writer capabilities and original
URL/RequestURI/body/headers; raw payload metadata remains explicit. Guarded routes
without a typed auth binding still fail registration. All declaration constructors
perform no I/O.

Written runtime/consumer tests cover guard/permission/signature failures before
path decoding and resource lookup, optional subjects, resource policy denial,
partial resolver errors, expiry, scope cleanup, native capabilities and metadata
ownership. Nine compiler rejection cases and three editor probes are written.
**None have run.** No generation or dependencies were needed for this slice.
Security-event/outbox composition, operational cleanup policy and final Rust auth
parity inventory still precede the complete milestone 10 verification/fix cycle.

## Milestone 10 acceptance

Native macOS full gate 22 passed in 12.9 seconds with cached results after gate 21
executed the complete test suite in 896.1 seconds. Gate 21 passed every test but
found three stale source-line comments in generated challenge models. Canonical
generation refreshed only those comments; all eight generation targets were
current in the successful final gate. All 2,494 verification inputs stayed unchanged.
The complete source and consumer tests, 805 compiler-rejection cases, 305 catalogued
editor probes plus three basic gopls operations, six field-documentation checks,
vet and documentation checks passed with required local PostgreSQL and Redis.

Auth, challenge, password, lockout, session/token persistence, MFA, encryption,
HTTP and auth-testkit race tests passed. Independent authenticating, passwords,
recovering and multifactor consumer races passed with real PostgreSQL; Redis
races passed against the existing local server. Four bounded parser fuzz targets
(session secrets, PHC, challenge JSON, ciphertext) passed ten-second runs each.
Initial verification-run mistakes (a test variable count, a consumer directory
name and an omitted Redis test address) were corrected. No remaining test failure
or pending authentication implementation item is being hidden by acceptance.

Consumer review confirms concrete models/keys through providers, guards,
permissions, signed/native handlers and bound resources; normal typed ORM
mutations for model behavior; transactional revocation, MFA changes and security
outbox; and explicit initiator attribution. Rust parity and redesigns are recorded
in [blueprint 10](10-model-first-authentication-and-authorization.md#source-parity-closure).
Only framework code and consumer fixtures were built. Storage (11), milestones
12–24 and the requested final framework-wide audit remain open; 25 stays deferred.
Private native evidence is under `.cache/milestone10-tokens/`.


## Milestone 11 acceptance

The [storage blueprint](11-storage-reliability.md#current-implementation-and-verification)
owns implementation, Rust parity, verification evidence and remaining acceptance.
Local managed storage, typed disk ownership, official SDK S3/R2 transfers/signing,
HTTP composition and an independent consumer passed native checks. Dependencies
reuse one official AWS SDK family under the user's nonduplication authorization.
The full regression passed 810 compiler-rejection cases, 310 real-gopls probes,
six automatic getter/setter notices, local PostgreSQL/Redis, current generation
and documentation checks. Storage/consumer races, parser fuzzing and regression
coverage for the reviewed fixes passed as well.

Live R2 verification subsequently passed with a temporary user-provided key,
including signed/public reads, multipart interruption cleanup and Unicode alias
protection. Its upload-generation header and NFC key behavior are now covered by
provider-specific regressions. A shutdown ordering race found during regression
was fixed and passed repeated reader/disk cancellation checks, storage/consumer
races and a fresh live R2 run. Separate live AWS S3 checks passed after fixing
form-encoded listing keys: spaces, literal plus signs and percent sequences retain
identity in object/upload pagination and historical-version cleanup. The four
versions left by the initial failure were removed; final read-only inventory found
no objects, versions, delete markers or pending uploads. AWS versioned reads,
conditional deletion and signed payload integrity passed. The optional public-URL
case skipped because the dedicated AWS bucket remains private.

Milestone 11 is complete. Consumer review confirms typed disk descriptors and
keys, explicit ownership/cleanup, bounded streaming and small upload/download
composition without application-side infrastructure. Jobs and the worker kernel
are next in milestone 12. The [storage guide](../docs/guides/storage.md) documents
the available API, provider differences and live-test configuration. Milestones
12–24 and the requested final framework-wide audit remain open; 25 stays deferred.


## Milestone 12 acceptance

The [jobs blueprint](12-jobs-and-worker-kernel.md#implementation-checkpoint) owns
delivered contracts, source parity and native evidence. Full `make verify` passed
with required local PostgreSQL/Redis, 818 compiler-rejection cases, 313 real-gopls
probes, current generation and documentation checks. Jobs, workflows, events,
outbox publication, infrastructure and independent consumer race checks passed.
The final source gate reused unchanged Go results and passed in 16.4 seconds;
recorded source hashes were unchanged when checked afterward. Evidence is private
under `.cache/milestone12-jobs/`.

Review fixes cover whole-envelope/workflow bounds, zero-delay retries, cancellation
of completion jobs while members still run, Redis retirement capacity/corruption,
and sub-millisecond eligibility. The consumer retains concrete DTOs, handlers and
service registration while Foundry owns execution and publication infrastructure.
Milestone 12 is complete. Scheduler work follows in 13; milestones 13–24 and the
requested final framework-wide verification/re-audit remain open, with 25 deferred.

### Milestone 13 verification and consumer review

Delivered the [scheduler](../docs/guides/scheduler.md): immutable parsed timing,
explicit zones/DST behavior, anchored intervals, bounded catch-up/concurrency/history,
system-attributed occurrences, hooks, environment filters and typed job targets.
Leadership and overlap protection reuse the lease manager/Redis authority. The
[owning blueprint](13-scheduler-kernel.md) records source parity and deliberate
best-effort delivery semantics.

On 2026-09-16, native macOS Go 1.27.1 `make verify` passed with required existing
PostgreSQL/Redis, 821 compiler-rejection cases, 316 real-gopls probes and six behavior
notice probes. Root/consumer vet/tests, deterministic generation and documentation
checks passed. Scheduler/Redis/lease races and independent Scheduler kernel races
passed, including real Redis expired-owner takeover, injected coordination failure,
leadership loss and overlap renewal until actual callback exit.

Review found and fixed custom error inspection escaping callback isolation; domain
errors now stay out of coordination classification. Panic/Goexit regressions cover
handler and failure-hook errors with/without overlap and unrelated healthy schedules.
The first full gate took 759.7 seconds; final-source `make verify` passed in 11.4
seconds using unchanged accepted test caches. All recorded source hashes matched.
Private evidence is under `.cache/milestone13-scheduler/`.

The [consumer](../tests/fixtures/consumer/scheduling/reports.go) declares a typed
report schedule, explicit Asia/Kuala_Lumpur time and injected domain service.
Foundry owns scheduling/coordination/drain; no consumer infrastructure loop is
required. Milestone 13 is complete. Milestones 14–24 and the final verification,
whole-framework audit and fixes remain required before the goal is complete.

### Milestone 14 verification and consumer review

Delivered [typed WebSocket channels and protocol](../docs/guides/websocket.md):
nominal room/event/payload ownership, version-one frames, local routing, fresh
operation scopes, owned user rooms, safe presence DTOs, hooks, bounded queues and
native HTTP/WebSocket kernel ownership. The [blueprint](14-websocket-channels-and-protocol.md)
records Rust feature review and the distributed boundary assigned to milestone 15.

On 2026-09-16, native macOS Go 1.27.1 `make verify` passed with required existing
PostgreSQL/Redis, 827 compiler-rejection cases, 319 real-gopls probes and six behavior
notice probes. Root/consumer vet/tests, deterministic generation and documentation
passed. WebSocket/HTTP and independent consumer races passed; protocol fuzzing
completed 42,636 executions with two workers. The first full gate took 787.4 seconds
(real gopls 443.438s, generator tests 161.220s).

Review added concurrent unsubscribe/disconnect exact-cleanup and blocked publication
codec ownership regressions. They passed under the race detector. The final-source
full gate passed in 12.1 seconds with accepted unchanged caches; all 2,673 recorded
source/build/fixture hashes matched. No runtime fixes were needed after initial
compilation. Private evidence is under `.cache/milestone14-websocket/`.

The [consumer](../tests/fixtures/consumer/realtime/orders.go) reuses generated DTOs,
model-owned rooms and a domain service. Foundry owns protocol, credentials, socket
loops, membership and shutdown. Its real kernel test verifies middleware, typed
messages/publication and independent socket lifetime after the HTTP deadline.
Milestone 14 is complete. Milestones 15–24 and final verification, a whole-framework
audit and its fixes remain required before the goal can be marked achieved.


### Milestone 15 verification and consumer review

Delivered [distributed WebSocket behavior](../docs/guides/websocket-distributed.md):
Redis fan-out and trusted HTTP/job publication, heartbeat/fresh authorization,
local/IP/subject and cluster quotas, safe TTL presence across tabs, typed subject and
connection disconnect, bounded replay/live deduplication, typed client relay,
acceptance/completion acknowledgements, shutdown drain and protected diagnostics.
The [blueprint](15-websocket-distributed-behavior.md) records Rust parity review and
explicit fail-closed recovery. No dependency was added.

Native macOS Go 1.27.1 `make verify` passed with required existing PostgreSQL/Redis,
832 compiler-rejection cases, 319 catalogued real-gopls scenarios plus three base
checks and six behavior-documentation checks, root/consumer tests and vet,
formatting, eight generation freshness targets and documentation checks. The
complete pre-cleanup-review gate took 606.2 seconds (gopls 468.535s);
the final reviewed-source full gate passed in 29.5 seconds.
All 2,703 recorded source/build/fixture hashes matched.

Two actual WebSocket/HTTP servers with separate instance identities used real Redis
for room isolation, live fan-out/duplicate suppression, typed relay, reconnect
history, pending replay/live overlap, multi-tab presence, heartbeat lease renewal,
global/subject quotas and forced disconnect. Authority tests covered abrupt-process
lease expiry, foreign-owner protection, last-tab removal, safe JSON integer precision,
replay count/byte/TTL/idempotency and corrupt policy/clock/revision rejection without
resetting state. Stream-gap tests verified degraded terminal state and actual drain.
Root/consumer races passed. Two-worker parser fuzzing passed 116,217 distributed
publication and 58,257 protocol-request executions; these are bounded fuzz samples.

Review fixed callback self-wait protection in backend operations and stream cleanup,
with real Redis regression cases. It replaced a timing assumption in replay overlap
with an explicit consumption barrier, covered typed connection disconnect and IP
capacity release, and checked 32-connection churn ownership/queue bounds with
allocation measurements (2,035,224 allocated bytes and 18,183 allocations in the
recorded churn sample; live goroutines returned from baseline 2 to 3). The first
gate exposed a stale gopls source anchor matching
PublisherSource instead of Publish; one compiler assertion also expected different
wording for the correctly rejected raw ID. Both were corrected in the batch, and the
new public APIs passed completion, hover, definition and compiler checks. Private
logs/manifests are under `.cache/milestone15-distributed/`.

The [independent consumer](../tests/fixtures/consumer/realtime/orders.go) shares one
registry across local/distributed hubs and trusted publishers, preserves generated
DTO/model-room types, and delegates protocol/state/ownership to Foundry. Its real
kernel test checks middleware, relay acceptance/completion and generated DTO replay.
Milestone 15 is complete. Milestones 16–24, final framework verification, one complete
implementation audit and its corrections remain required before the goal is achieved.


### Milestone 16 verification and consumer review

Delivered [email](../docs/guides/email.md): validated mailbox values with shared
JSON contract metadata, immutable messages, concrete typed templates with safe
HTML escaping, bounded MIME/storage attachments, SMTP and Mailgun/Postmark/Resend/
SES adapters, provider receipts, classified failures, safe observations/diagnostics,
borrowed-resource ownership, memory/log drivers and fake assertions. Rust feature
review preserved the transport coverage while using Postmark header arrays and
SES raw MIME for attachments. No dependency was added: standard-library facilities
and the existing AWS SDK signer/credential interface cover the adapters.

Typed `JobHandler[P]` reuses jobs schema/capture/dispatch and the transactional
outbox. Stable execution IDs become provider idempotency keys; queued references
must pin object version or ETag. `jobs.Permanent` records terminal failure, and
`jobs.PreventRetry` protects a known accepted side effect against a later timeout
or failing middleware without pretending the whole job succeeded. Owned callback
classification covers custom error Is/As/Unwrap, panic and Goexit.

The native macOS Go 1.27.1 full `make verify` passed against the final reviewed
source in 492.4 seconds, with required existing PostgreSQL/Redis,
837 compiler-rejection fixtures, 322 catalogued real-gopls scenarios plus
three base checks and six behavior-documentation checks, root/consumer vet/tests,
eight generation freshness targets, formatting and documentation checks. All
2,740 recorded source/build/fixture fingerprints matched. Focused root and
consumer races passed. Two-worker address fuzzing passed 104,692 initial and 74,540 final-source executions; this
is a bounded sample, not an exhaustive parser proof.

Local fixtures verified SMTP plaintext-loopback/implicit TLS/STARTTLS/authentication,
all-recipient admission before DATA, BCC envelope separation, temporary/permanent
rejection, accepted-but-lost responses and cancellation. HTTP fixtures checked all
four API request formats, attachment bytes/inline identities, existing AWS SigV4
credentials/session token, response bounds, provider-specific error codes,
idempotency, redirect refusal and no replayable submission body. Storage tests
verified pinned content failure, byte limits and reader closure before transport
and after read failure. Real PostgreSQL proved rollback/uncommitted suppression and
committed typed job publication. Runtime tests covered safe HTML, MIME nesting,
header injection, immutable snapshots, callback ownership, capacity, self-shutdown,
redaction and acceptance-preserving observers.

Review fixed a bounded-writer bypass before the first compilation, then closed
accepted-email retry after timeout/middleware failure, preserved transient builder
cancellation, completed address DTO metadata/raw JSON bounds, strengthened safe
structured formatting and corrected a vet-detected mutex-copy formatter. The
[independent consumer](../tests/fixtures/consumer/mailing/welcome.go) delegates
infrastructure to Foundry and retains only domain data, templates and declarations;
its generated recipient DTO uses the address's single wire contract. Private logs
and fingerprints are under `.cache/milestone16-email/`.

Real-account email sends were not run: no approved provider credentials, verified
sender and test recipient were supplied for this gate. Local protocol fixtures do
not establish external deliverability. Provider accounts remain a separate
operational decision; acceptance does not claim inbox delivery or exactly-once
sending after worker/process loss. Milestone 16 is complete with that recorded
verification gap. Milestones 17–24, final framework verification, one complete
implementation audit and its corrections remain required before goal completion.


### Milestone 17 verification and consumer review

Delivered [notifications](../docs/guides/notifications.md): typed input/version
and recipient/model/key declarations, explicit preferences, database/email/
realtime/custom channel outputs, three generated private persistence models,
ordinary PostgreSQL migrations, authenticated recipient-scoped inbox pages,
unread/read operations and per-channel status. Recipient resolution reuses the
existing provider identity, lookup and eligibility policy; a guard must share
that exact provider declaration. Equal stored keys from different models/guards
remain in separate recipient scopes.

Persistent channel preparation freezes output before external claiming. Database
inbox creation and delivery commit atomically; completed channels are skipped by
retries. External definite rejection alone permits retry, with stable delivery
IDs and frozen rendered output. Ambiguous responses or abandoned Running claims
are inspectable and never automatically resent. This deliberately does not claim
exactly-once external delivery or end-user receipt of realtime/email content.

Email Snapshot uses existing Message validation and pinned storage references;
implicit Message serialization remains denied. Private realtime helpers reuse
owned guard/model/key rooms, server-only events, existing local/distributed
publishers and a typed outer notification envelope. Custom channels implement
one focused typed transport. Existing jobs/outbox supplies queue capture,
serialization, worker attempts and transaction-aware publication; no second queue
was introduced. Shared savepoint/schema scoping now lives in internal/sqlscope,
with existing credential code forwarding to the same implementation.

The native macOS Go 1.27.1 full `make verify` passed against the reviewed source
in 819.1 seconds, using the required existing PostgreSQL/Redis
services, 844 compiler-rejection fixtures, 325 catalogued real-gopls scenarios
plus three base checks and six behavior-documentation checks, nine generation
freshness targets, root/consumer vet/tests, formatting and documentation checks.
All 2,783 recorded source/build/fixture fingerprints matched. Focused root
and independent consumer race checks passed. Bounded two-worker SnapshotDecode
fuzzing passed 149,970 executions; this sample is not an exhaustive parser proof.

Acceptance exercised partial delivery/retry without duplicate inbox/realtime,
stable email routes and keys, preferences and current recipient revocation/deletion,
same-key cross-model ownership, read/unread scoping, queue wire restoration,
transaction rollback and suppressed enqueue errors, schema restoration, abnormal
callbacks, concurrent claims and actual callback shutdown ownership. A recorded
completion-write failure leaves an inspectable Running claim without resending.
The real WebSocket fixture rejects whole-channel/foreign-room subscriptions and
client attempts to publish the notification event.

Source review closed revocation during rendering and a failed concurrent renderer
overwriting another caller's prepared snapshot. Consumer review fixed provenance
changing when the same notification was recaptured/enqueued from a later request:
first stored attribution is restored for callbacks and job publication. Real
PostgreSQL duplicate-outbox-publication regression passed for that case. The
[independent consumer](../tests/fixtures/consumer/notifying/orders.go) retains domain
DTOs/renderers/declarations while Foundry owns infrastructure. No dependency was
added, no external email was sent, and the milestone 16 real-provider smoke gap
remains explicit. Private verification logs/fingerprints are under
`.cache/milestone17-notifications/`.

Milestone 17 is complete. Milestones 18–24, final framework verification and one
whole-implementation audit with its corrections remain required before the goal
can be marked achieved.


### Milestone 18 verification and consumer review

Delivered [imaging](../docs/guides/imaging.md),
[attachments](../docs/guides/attachments.md), shared typed extension owners,
[metadata/translations](../docs/guides/model-extensions.md),
[settings](../docs/guides/settings.md) and [countries](../docs/guides/countries.md).
Ordinary generated models and explicit versioned migrations own infrastructure
storage. Owner identity, codecs, query scopes, lifecycle transactions and contract
schemas reuse their existing sources; no second ORM or queue was introduced.

Immutable image plans support JPEG, PNG, lossless WebP, GIF, BMP, TIFF, ICO and
AVIF output. AVIF remains output-only, matching the Rust default build. Inspection
checks input/container bounds, dimensions, frames/pages, EXIF orientation and
resampling intermediates before pixel work; re-encoding strips metadata. The
admitted workspace estimate is not a process heap quota. Actual codec/reader
lifetime retains operation capacity through cancellation and shutdown.

Attachment intent commits before storage, acknowledged bytes are pinned and
verified, and ready ownership commits before superseded deletion. Durable
cleanup is retryable with existing jobs/outbox/Redis workers. Unknown storage or
commit outcomes are inspectable and never justify blind deletion. Explicit
settlement requires the writer and provider to have stopped; retained files
remain protected from automatic cleanup. Owner pruning rechecks current models,
including soft-deleted ones; storage orphan listing is read-only.

Typed metadata/settings use versioned JSON contracts, including exact primitive
types and an explicit dynamic escape hatch. Translation fields and localized
attachments share typed locale snapshots and deterministic fallback. Batches use
bounded data and owner/value snapshots without per-owner N+1 queries. Country
seeding uses pinned Rust reference data and timezone mapping, preserves existing
administrative choices and never installs itself during boot.

Native macOS Go 1.27.1 `make verify` passed in 688.8 seconds:
root/consumer formatting, vet and tests; required existing PostgreSQL/Redis;
all 859 compiler-rejection fixtures; 332 catalogued actual-gopls scenarios
plus three base checks and six field-documentation checks; twelve generation
freshness targets; and documentation checks. All 2,899 recorded
source/build/data fingerprints matched the frozen implementation. Focused root,
independent consumer and actual Redis attachment integration race checks passed.
Bounded two-worker image inspection fuzzing passed 197,708 executions; this is a
sample rather than an exhaustive parser proof.

Acceptance covers concurrent replacements, failed puts/hooks/commits/deletes,
both real outcomes of lost commit acknowledgements, conditional foreign-object
protection, cancellation with active readers, jobs/outbox retry, owner deletion
rollback, soft-delete restoration, retained objects, orphan pagination, typed
values, batch query counts, and idempotent reference seeding. The
[profiles consumer](../tests/fixtures/consumer/profiles/profile.go) uses only public
typed policy, generated DTOs and lifecycle composition.

Source review hardened embedded ICO PNG metadata and fill intermediates, moved
the attachment reader progress guard into shared operation infrastructure for
image processing, and fixed translation deletion beyond the generic 1,000-row
batch limit with bounded key locking and ordinary typed deletes. The 1,088-row
regression and UUID orphan cursor checks passed. The first full gate passed all
tests but detected a stale generated source-location comment after a model field
notice shifted a following DTO. The generator now updates locations for DTO,
path, query and multipart declarations as well as models/enums/projections.
Fresh, repeated and check-only generation passed for all seven declaration kinds
in one source file; all twelve current-generation targets and the final full gate
passed after the correction. Four reviewed non-duplicating
image libraries were installed and aligned across modules. No database reset,
duplicate service, Rust edit, commit/push/merge or external email send occurred.
Milestone 16's real-account email smoke gap remains recorded. Private logs and
fingerprints are under `.cache/milestone18-imaging/`.

Milestone 18 is complete. Milestones 19–24, final framework verification and one
whole-implementation audit with its corrections remain required before the goal
can be marked achieved.


### Milestone 19 verification and consumer review

Delivered [datatables and reporting](../docs/guides/datatable.md): typed table IDs,
columns preserving SQL scope/row/value/nullability, mandatory authority and source
scope, stable pages/counts, declared WHERE/HAVING filters, relation filters and
bounded CSV/XLSX artifacts. `projection dto=true` shares the existing DTO emitter,
JSON graph and typed validation selectors with the generated SQL projection.
Ordinary projections remain independent from transport DTOs. The opt-in applies
ordinary DTO restrictions, including no persistence `foundry` tags. Literal
case-insensitive text comparisons bind escaped SQL parameters through the same
query compiler.

Structural request bounds and the complete allowlist validate before scalar
callbacks or SQL. Mixed WHERE/HAVING OR/NOT groups are rejected. Server source
policies and authorization run for interactive queries, counts and exports;
exports use a separate action. Root acceptance checks two SQL statements for a
page with a count, one for a count, and one streaming statement for an export.
Read-only repeatable-read transactions preserve each operation's snapshot.

The [public reporting consumer](../tests/fixtures/consumer/reporting/members.go)
uses generated nullable/enum/exact-decimal/computed fields, aliases and joins,
grouped HAVING, relation filters and tenant/soft-delete scopes. Interactive
operations reuse the registered guard policy. Ordinary queued jobs persist actor
references, filters and explicit locale/timezone, reload current eligibility and
permissions per attempt, and deliver complete artifacts to an explicit destination.
Real worker acceptance covers permission revocation after dispatch, retries,
permission restoration, interrupted acknowledgement after storage, stable job
identity and conditional idempotent storage. Current source changes do not replace
an already completed object for the same execution. No additional queue or
implicit external notification was introduced.

Artifacts retain bounded export admission until Close, use private temporary
files, and expose only completed bytes. Failed file removal remains manager-owned,
blocks new export admission and is retried. Cancellation stops reads while actual
callback/file ownership remains visible; module shutdown retains borrowed services
until both query and export lifetimes exit. Existing HTTP downloads provide byte
ranges, authentication errors and closing. CSV quotes cells and applies its
documented apostrophe policy for formula-like text. XLSX has five fixed ZIP parts,
inline text cells and no shared-string table, formulas, macros or external links.
All values remain text, preserving large integers and decimal strings. Cell,
row, compressed-output and XML limits are explicit; one worksheet is supported.

Native macOS Go 1.27.1 `make verify` passed in 25.5 seconds:
root and independent consumer formatting/vet/tests, required existing PostgreSQL
and Redis, all 875 compiler-rejection fixtures, 338 catalogued actual-gopls
scenarios plus three base and six field-documentation checks, thirteen generation
freshness targets, and documentation checks. All 2,999 frozen source/build/data
fingerprints matched. The milestone adds sixteen compiler-rejection cases, six
editor scenarios and seven public consumer integration tests. Focused root and
consumer races passed, including shared lifetime callers in attachments/imaging.
Fresh, repeated and check-only combined projection/DTO generation passed.

Bounded two-worker fuzzing passed 43,006 request executions and, after correction,
28,362 XLSX literal executions. Fuzzing discovered that expanding a carriage
return could complete an earlier literal `_x` prefix and corrupt text. The writer
now protects incomplete prefixes as well as complete escape sequences; the
failing corpus is retained. A shutdown test found that placing a timeout outside
the shared cancellation link delayed immediate owner-cancellation observation;
the link now remains outermost and repeated regression checks cover the boundary.
A final delivery-context review added bounded regressions demonstrating a self-shutdown
deadlock and lost export deadline in queued delivery. The adapter now passes the
artifact lifetime context, retains job identity, rejects self-shutdown without
partially closing the manager, and propagates cancellation even after a callback
returns nil. Root and public worker/storage race checks, the affected editor
scenario and a second frozen full gate passed after this correction.
Consumer metadata comparison now compares the serialized contract, since omitted
empty metadata slices and nil slices have the same representation.

Streaming 50,000 distinct cells (>50 MiB of input) while keeping the worksheet
open retained at most 16,368 additional live-heap bytes at post-collection
checkpoints in the focused run, under the 8 MiB regression threshold. This is a
live Go heap measurement, not a process RSS quota or a claim that cumulative
allocation is constant. Independent ZIP/XML readers check structure, literal
round trips and exact values. Operational limits and safe CSV interpretation are
recorded in the guide. No dependency installation, database reset, duplicate
service, Rust edit, commit/push/merge or external send occurred. Milestone 16's
optional real-account email smoke gap remains recorded. Private logs and frozen
fingerprints are under `.cache/milestone19-datatables/`.

Milestone 19 is complete. Milestones 20–24, final framework verification and one
whole-implementation audit with its corrections remain required before the goal
can be marked achieved.


### Milestone 20 verification and consumer review

Delivered [localization](../docs/guides/localization.md),
[outbound HTTP clients](../docs/guides/http-client.md), and
[supporting APIs](../docs/guides/supporting-apis.md). The existing immutable
`i18n.LocaleSet` remains the sole supported/default locale configuration for UI
catalogs, model translations and localized attachments. UI messages select the
requested translation then the configured fallback; model-content fallback is
unchanged. Declared missing translations return an explicit missing result and
key, while undeclared dynamic keys and signature mismatches fail. Catalogs copy
configuration and templates, bound directory/file/input/output resources, reject
invalid or duplicate JSON/flattened keys, normalize locale directories and isolate
filesystem callbacks through actual return. Filesystem root authority remains the
caller's choice (`os.Root.FS` for root confinement); entry checks are not a substitute
for a filesystem's concurrent replacement guarantees.

`//foundry:message` uses the existing DTO discovery, generated codec and validation
selectors, retaining the concrete `Message[Args]` type. The acyclic `i18n/message`
adapter derives required scalar parameters from the shared wire graph, including
quoted booleans/integers, enums and exact decimals. Invalid locale, context or
catalog signature fails before a custom encoder. One-pass interpolation preserves
argument braces. CLDR cardinal/ordinal selection uses the installed x/text tables
and canonical decimal scale. Large operands preserve modulo digits and cannot
collapse into small literal matches. The guide identifies the library's explicit
CLDR-version contract; this is not a claim of the latest CLDR tables.

Opt-in enum labels derive from actual generated cases; validation issues keep
wire paths and static missing-label fallbacks; permission labels preserve the
registered policy's nominal identity. Request locale middleware uses context and
quality-sorted language headers without global mutable locale state. The shared
manifest emitter remains milestone 21; these APIs supply owned metadata through
existing typed descriptors.

Named `httpclient` instances own native pools or borrow explicit transports, with
bounded concurrency, whole-operation/per-attempt deadlines, immutable client-bound
requests, copied headers/bodies and redacted diagnostics. Safe-read retries require
replayable content; mutation retries require explicit idempotent-operation policy.
Native `GetBody` remains unset and even empty bodies retain an owned wrapper, so
an idempotency-key POST cannot silently replay behind that policy. Redirects are
returned without following. Small rejected bodies drain within a bound before
reuse; reads, body factories, transport callbacks and actual close operations
remain owned until return. The request body closes before the response to interrupt
unfinished uploads. Escaped stream readers close after the callback. Panic,
Goexit, swallowed read failure, invalid lengths and close failure cannot publish a
successful buffered response or release admission early. Module shutdown drains
actual callbacks before borrowed providers. Test transports have bounded copied
outcomes and captures, explicit exhaustion and no network fallback. Email adapters
share only the native transport constructor and retain their one-attempt policy.

The token utility shares entropy with stored authentication credentials while
preserving the prior raw-byte digest and URL-safe token format. Existing encryption,
rotation, password and signing implementations remain the source of truth.
`sanitize` uses bluemonday v1.0.27, with its existing transitive dependencies and
aligned x/net v0.59.0, under the repository's dependency authorization. The private
immutable policy permits selected passive tags only and enforces input/output
bounds; output is HTML body content, with no promise to repair malformed nesting.
Focused slice transformations and explicit-clock/timezone helpers complement Go's
standard APIs instead of creating a mutable collection or global-time service.

Native macOS Go 1.27.1 `make verify` passed in 765.3 seconds:
formatting/vet, all root and independent consumer tests, required existing
PostgreSQL and Redis, all 887 compiler-rejection fixtures, 345 catalogued
actual-gopls scenarios plus three base and six field-documentation checks, thirteen
generation freshness targets, and documentation checks. All 3,073 frozen
source/build/data fingerprints matched. The milestone adds twelve compiler cases,
seven editor scenarios and six independent localization/outgoing consumer tests.
Fresh, repeated and check-only generated-message tests passed. Root and consumer
races passed, including shared contract/validation/auth, stream read-close ownership,
provider shutdown, HTTP connection reuse/decompression and email adapter callers.
Existing encryption/password tests and credential-format compatibility passed.

Bounded two-worker fuzzing passed 24,372 localization inputs and 18,710 sanitizer
inputs. The first native round found x/text `MatchDigits` saturating large integer
operands, so the adapter now uses the documented operand API with regression
coverage for large, negative, fractional, cardinal and ordinal values. Fresh
standalone generation found a missing generated-message import in export discovery;
that dependency now joins the existing explicit generated-import list. An editor
probe was corrected to inspect the actual consumer expression. Review also added
pre-encoder locale validation, direct fake metadata bounds before copying, and
regressions for acquired readers returned alongside errors and cleanup failure.
The batched fixes passed targeted checks before the frozen full acceptance run.
A subsequent review corrected direct fake-transport rejection of a nil URL to
close the already transferred request body. Its regression and affected root/public
consumer races passed, followed by a second final-source full gate. Enum cases sharing a label key now
contribute one catalog declaration. The first full run also exposed an older cursor
fixture that assumed same-millisecond UUIDs followed insertion order; its oracle
now derives group winners from the ordered baseline and exercises both buyer
orders. Repeated real-PostgreSQL regressions and the final full gate passed.
No database reset, duplicate service, Rust change, commit/push/merge or external
send occurred. Milestone 16's optional real-account email smoke gap remains
recorded. Private evidence is under `.cache/milestone20-localization/`.

Milestone 20 is complete. Milestones 21–24, final framework verification and one
whole-implementation audit with its corrections remain required before the goal
can be marked achieved.


### Milestone 21 verification and consumer review

Milestone 21 is complete. `contract/manifest` owns a validated version 1 graph
from registered HTTP/realtime, rendered notification, datatable, locale, enum and
permission metadata. `openapi` emits OpenAPI 3.1.1; `typescript` emits a pure ES2022
module with injected transports and bounded runtime codecs. The separate client
value representation preserves full Go wide-integer/json.Number numeric tokens
without JavaScript rounding. Optional/null, model identities, decimal/time values,
multipart JSON/file fields, pagination, declared errors and file ownership remain
explicit. Private persistence and notification inputs are not inferred as public
DTOs. Portable validation honestly reports skipped server-only/unsupported rules.

The shared publisher now supports client ownership manifests and a narrowly scoped
recovery journal. Three deterministic artifacts publish together with read-only
checking, edited/unowned file refusal, rename/deletion, canonical directory handling,
hash/mode preservation and interrupted-publication recovery. Existing Go publication
and older journal authority remain covered by the complete generator tests.

Native macOS full gate01 passed in 1097.2 seconds; after
the callback review fixes, final-source full gate02 passed in 31.3
seconds, reusing eligible cached results. Verification required
PostgreSQL/Redis, all 893 compiler-rejection fixtures, 350 catalogued
real-gopls scenarios plus existing base/field documentation checks, all generation
freshness targets, and documentation checks. All 3,299 frozen source,
build and data fingerprints matched. Strict TypeScript 5.9.3 with
`exactOptionalPropertyTypes`/`noUncheckedIndexedAccess` passed, including all
12 negative assertions. Native Node 24.21.0 exercised real HTTP and
WebSocket servers: exact codecs, validation/error paths, status agreement, auth,
uploads, range downloads, paging, private rooms, presence, replay, accepted/final
acknowledgements, malformed frames, deduplication and cancellation. An older v1
generated client retained its existing operation against the newer server.

Root/consumer races passed with batched fixes and affected reruns. The complete
generator race suite took 253.790 seconds. Final client consumer races passed in
2.441 seconds test time. Two-worker bounded manifest fuzzing passed 813 executions;
this is a smoke check rather than an exhaustive parser proof. Compiler/editor
coverage adds six Go negative cases and five catalogue scenarios. The TypeScript
compiler is a pinned development tool; generated SDKs import no runtime package.

Verification corrected a remaining pagination metadata helper call, closed struct
inspection of embedded unlisted types, canonical macOS temporary paths, strict TS
regex capture typing and empty transport lists serialized as null. Source review
also preserved primary errors through JSON/file cleanup and froze realtime callback
payloads. After the first full gate, callback review contained async listener/error
observer rejections and bounded dispatch over listener snapshots, including changes
and client closure during delivery. Affected races and the final full gate passed. The first race batch's only failure was a regression assertion assuming
compact instead of formatted JSON; the semantic assertion and affected race rerun
passed. Failed logs remain retained. No milestone was accepted on a failed gate.

Consumer review confirmed one registration-owned manifest, typed operations/events,
explicit transport/resource ownership, framework-neutral React/Vue integration and
documented application compatibility boundaries. Milestones 22–24, final framework
verification and one complete audit/fix round remain open. The milestone 16 optional
real-account email smoke gap remains recorded.


## Milestone 22 acceptance

The [plugin ecosystem](../docs/guides/plugins.md) is implemented and accepted.
Typed manifests validate full semantic versions, framework compatibility, missing
dependencies and cycles before registration. Plugins reuse foundation's service,
feature and lifecycle contracts, with explicit typed application overrides and
value-free inspection. Configuration uses isolated namespaces and the existing
layered loader. Migrations retain their historical introducing release. Assets
and typed scaffolds share the guarded ownership publisher and version 6 recovery,
including an immutable proof for repeated interrupted recovery.

All implementation, test sources, fixtures and documentation were written before
project compilation/testing. Native Go 1.27.1 `make verify` passed in 1012.9 seconds
with required existing PostgreSQL/Redis services, actual gopls and TypeScript.
The gate covered both independent plugin modules and the application consumer,
all 903 compiler-rejection cases, all 357 editor scenarios, generation, vet,
formatting and documentation. Plugin additions account for ten compiler cases
and seven editor scenarios. Separate ordinary/race tests exercised configured
routes, events, worker execution, cleanup order and retained PostgreSQL migration
history. The shared publisher race suite passed; two bounded 15-second fuzz runs
passed 434,543 path inputs and 1,412,117 manifest inputs. No implementation fixes
were required by these verification rounds; final documentation review clarified
constructor errors, registration-helper ownership and canonical output roots.

Consumer experience was reviewed against the independent modules and public test
harness. Milestones 23–24, whole-framework verification and the requested complete
audit/fix round remain required. Milestone 25 remains deferred.


## Milestone 23 source preparation

Implementation and test sources cover the [tooling contract](23-developer-tooling-and-testing.md):
CLI/bootstrap, create-only scaffolds, offline doctor and registry inspection,
test-owned clients and fakes, generated-model factories and typed query plans.
The [consumer guide](../docs/guides/developer-tooling-and-testing.md) links executable
recipes. Twelve compiler cases and eight real-editor scenarios extend the
catalogues. Verification batching retains individual case ownership, fresh editor
sessions per scenario and external-child cache invalidation. No milestone 23
compilation or acceptance result is claimed here. Complete the source review,
formatting and verification/fix cycle before accepting it or starting 24.


Milestone 23 first verification round passed: core 58.2s, generator 197.9s,
independent consumer 31.3s, eight new editor scenarios 5.7s and static checks 17.2s.
The compiler batch failed one expected-diagnostic wording assertion; its real
native JSON correctly identified the intended source/type error. The expectation
was fixed as a batch after all results were collected. Full acceptance remains
pending; failed logs are retained and no completion is claimed.


## Milestone 23 acceptance

The [developer-tooling contract](23-developer-tooling-and-testing.md) is accepted.
Independent consumers exercise typed CLI argument/handler ownership, real shared
bootstrap and cleanup, pure registry inspection, generated HTTP/realtime DTOs,
model factories and explicit local job/event/mail/storage capabilities. Query
plans reuse compiled statements/bindings and preserve transaction requirements;
PostgreSQL proves planning versus execution, side effects/rollback, locks and
cancellation. Scaffolds preserve existing files and legacy recovery authority.

All milestone implementation, tests and docs were written before compilation.
The final native Go 1.27.1 `make verify` passed in 430.4s with required existing
PostgreSQL/Redis, actual gopls/Node/TypeScript, all three independent modules,
generation freshness, vet/formatting and documentation checks. It includes 915
compiler cases and 365 editor scenarios plus base and field-documentation probes.
The complete generator package passed in 180.177s. Relevant root and
consumer races passed; a 15-second plan fuzz campaign passed 108,861 inputs.

Batching retained all assertions: per-case Go JSON build/source attribution and
one fresh gopls per scenario with three independently bounded operations. Final
editor and consumer compiler package times were 215.733s and 9.366s;
previous milestone values were 582.512s and 130.414s with fewer scenarios/cases.
These are native acceptance observations; controlled resource budgets remain in24.
Make computes one source/tool fingerprint for external-child gates, including
shared generator fixtures and native prerequisites, with actual stale/reuse proof.

Verification corrected one expected diagnostic wording and a race-test process
exit allowance. Review corrected `agent --help` and completed external-child cache
coverage. All affected ordinary/race checks and the final full gate passed after
those fixes. Consumer experience was reviewed, including lifecycle/auth/transaction
semantics and explicit ownership. No dependency was installed. No Git publication
or database reset occurred. Milestone24 and the final complete-framework
verification/re-audit/fix round remain required;25 remains deferred.

## Milestone 24 source batch prepared

The implementation, behavior/fault/load/fuzz test sources, independent consumers,
compiler/editor cases and documentation are prepared before any milestone 24
Go generation, formatting, compilation or test execution. The source covers
owned binary fields, explicit read/primary pools, readiness/maintenance, shared
traces/metrics/error export across all kernels, protected native diagnostics,
HTTP admission limits and workers-first traced-envelope rollout.

Private release preparation uses an isolated x/mod development module and builds
canonical framework/plugin artifacts without workspace replacements. Native
measurements separate ordinary and broad consumers, cold/unchanged/edited
generation/build caches, generated size, compiler/process memory and gopls
startup/request latency. The approved Rust Foundry MIT license was reused with
explicit user confirmation. Compatibility, operations and dependency-review
documents describe pending release evidence. Root runtime dependencies did not
change and no dependency installation occurred during source preparation.

Consolidated verification/fix rounds begin next. No native milestone 24 result,
security scan, controlled measurement or whole-framework audit is claimed here.
The goal remains open until final verification and a complete implementation
re-audit with fixes are finished; milestone 25 remains deferred.


## Milestone 24 acceptance

The complete source/test/documentation batch preceded verification. The initial
consolidated gate passed in 506.5s; the requested complete-framework audit then
reviewed every parity family and fixed shared error ownership, worker backend
isolation, lifecycle locking, route factory provenance and repeated generation
recovery. Its grouped race regressions passed, followed by the complete native
post-audit `make verify` gate in 739.5s against 3536 matching frozen source inputs.
The gate covers required PostgreSQL/Redis, root and independent consumer/plugin
packages, 934 compiler rejection cases, 380 catalogued editor scenarios plus
three basic/six field-documentation probes, TypeScript interoperability,
generation freshness, vet/formatting and documentation. Bounded trace fuzzing
passed 3,993,247 inputs; selected five-group races passed in 145.5s.

Private canonical module ZIPs passed independent consumption with an isolated
60-module dependency graph, no local replacements, module integrity and matching
generated output. Native ordinary/full cold builds measured 3.30/60.24s; actual
schema-edit builds measured 1.19/1.44s. Compiler/process memory, generated size,
all editor rounds and explicit resource budgets are published in the
[measurement guide](../docs/guides/developer-resource-measurements.md).

The user-approved MIT license and govulncheck v1.8.0 were used. Packaged framework
and consumer scans reported no affected functions; the sole runtime module-only
OpenPGP advisory has no affected imports. The release tool's patched x/mod scan
was clean; selected tool-only findings and license/notice/patent obligations are
recorded in [dependency review](../docs/dependency-review.md). Artifact/verification
log checks found no configured private database credentials. Unchanged S3/R2
adapter source retains the actual milestone 11 live certification; this is not a
new live-account check. Optional real-account email smoke remains unverified.

[Production acceptance](../docs/production-acceptance.md) records the audit scope,
fixes, packaging, compatibility and evidence limits. Milestones 01–24 and the
requested final audit/fix round are complete. Milestone 25 remains deferred.
No Git commit, push, merge, publication or production deployment was performed.
