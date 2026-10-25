# Go-way data layer

The `goway-data-layer` branch replaces generated model APIs with handwritten,
typed declarations backed by the existing SQL core. The old API remains available
until Stage 8. This is a branch program in progress, not a released replacement.

The [master status](../00-master-architecture-and-parity.md#go-way-data-layer)
owns delivery status. The planning record,
`proposals/foundry-go-goway-data-layer/PLAN.md` (outside this repository), contains the reviewed design and historical experiments. This page condenses that
record and records source audit findings; the [ORM source](../../database/orm/README.md)
and [independent consumer](../../tests/fixtures/goway) define what exists now.

## Goals and rules

Keep concrete model, primary-key, field, result and query-scope types, with useful
completion and ordinary Go compilation costs. Preserve SQL, bindings, hydration,
lifecycle order, omitted/null/value semantics, transaction outcomes and public
wire contracts. Reuse framework infrastructure; applications supply their models
and business code. Removing model generation does not remove DTO or enum generation.

| Rule | Constraint |
| --- | --- |
| R1 | Keep public generic method sets at most 25 exported methods per type. Private signatures remain subject to reachability checks; the sealed ordered marker is private. |
| R2 | Keep generic method bodies thin; delegate substantial work to the non-generic core. |
| R3 | Do not embed heavy generic state in public generic structs; use non-generic state or typed function fields. |
| R4 | Keep every method signature, including private signatures, restricted to thin types. Enter long-tail SQL families through package functions. |
| R5 | Extend `internal/querycore` and existing compiler/executor/transaction owners. Hydrate through validated offsets and column scanners. |
| R6 | Immutable declarations may be package values. Mutable runtime services remain instance-owned and injected; no global registry. |
| R7 | Exported symbols need documentation and a one-line example; package docs provide recipes. |
| R8 | Test common usage footprints and generic reachability so an API addition cannot silently make every importer expensive. |
| R9 | Do not build public generic types from embedding chains. Each type declares its own methods as thin shells over shared helpers; every embedding level recompiles each promoted method, and its pointer wrapper, per instantiation. |

## Stages and evidence

| Stage | Scope | Status |
| --- | --- | --- |
| 0 | Review design and decisions | Done; approved planning record |
| 1 | Scratch prototype and go/no-go gate | Done; historical evidence below |
| 2 | Branch, permanent consumer, catalogue, emitter/release audit, synthetic harness | In progress. Core and permanent fixture ported at `686c6f2`; guardrails are this increment. |
| 3.0 | Column footprint: flatten column types (R9), then measure receiver form and codec sharing; method-set trims only with approval | Done. 70-model gate: functions and bytes pass; compiler RSS 5.14 GB median against a 5 GB target, accepted as a recorded miss (2026-10-23) |
| 3.1a | Declarations/codecs: JSON, interval, natural keys, binary ownership and IContains | Implemented; local acceptance passes (segmented fixes); PostgreSQL sources compile/vet, execution belongs to the lead |
| 3.1b | Writes, encrypted fields and password hashes | Implemented; local acceptance passes in segments; live PostgreSQL belongs to the lead |
| 3.1c | Cursor/key-chunk reads and locked keyed lookup composition | Implemented; Go/fixture gates pass; unchanged contracts; existing TypeScript timestamp assertion fails on HEAD too; PostgreSQL execution belongs to lead |
| 3.1d | Typed raw model/row hydration | Implemented; acceptance evidence below; live PostgreSQL belongs to the lead |
| 3.1 | Complete declaration, reads, writes, typed raw hydration and model cursors | Done. Stage 3 gate passed on 2026-10-23: `make verify`, `make test-postgres` (race) and `make typescript-check` |
| 4a | N+1 detector foundations | Done; committed at `e731753` |
| 4b | Direct eager trees, has-one, LoadMissing, relation limits and existence filters | Done; committed at `470d585`. |
| 4c | Typed pivots, through, one-of-many and polymorphic relations | Done; committed at `a89f509`. Live PostgreSQL execution remains for the lead. |
| 4d | Typed relation aggregate slots | Done; committed in integration batch 2 at `80546f4`. |
| 4 | Full read relations, pivots, through, one-of-many, filters and aggregate slots | Complete; 4a–4d delivered. 4d committed in integration batch 2 at `80546f4`. Relation writes are delivered in Stage 5b; per-parent limits are out of scope. |
| B5a | Typed mutators, accessor validation, input-column kinds and InputKey | Done; committed at `884beb7`. |
| B5b | Write hooks, observers, stored changes, conventions and set-write/force-delete parity | Done; committed at `af1976a`. |
| B5c | Retrieval hooks/observers, global scopes and tenancy | Done; committed at `80546f4`, including sticky scope-source panic/Goexit failures. |
| B5d | Typed audit policies, transactional capture and historical readers | Done; committed in integration batch 3 at `638dc34`. |
| 5b | Typed pivot relation writes through the B5b lifecycle | Done; committed in integration batch 4 at `5cbe31d`. |
| 5 | Lifecycle, mutators, change sets, scopes, observers, audit and relation writes | COMPLETE; B5a–B5d and 5b delivered. Extension slots remain Stage 7. |
| 6a1 | Expression phases, values, arithmetic, casts, comparisons, Count/Exists and MapSelected | Done; committed at `a5f30a6`. |
| 6a2 | Row and selected CASE, COALESCE and NULLIF | Done; committed at `a5f30a6`. |
| 6a3 | Summary aggregates, FILTER, GROUP BY/HAVING, selected ordering and keys, DISTINCT | Done; committed at `2cc60e8`. Ordered input-kind compatibility completed with the 6b1 integration below. |
| 6a4 | Typed raw fragments and opt-in no-raw lint | Done; committed at `638dc34`. |
| 6a | Expressions, aggregates, grouping, having and raw fragments | Complete; 6a1–6a4 delivered. Current batch evidence below. |
| 6b1 | Join kinds, nullable scopes and typed column lifting | Done; committed with the integration gate at `690a147`. |
| 6b2 | ON comparisons and independent scalar/IN/EXISTS subqueries | Done; committed at `80546f4`; hardened join-source validation included. |
| 6b3 | Explicit correlation and cross/inner/left LATERAL | Done; committed at `80546f4`; model and projected results retain scope/nullability. |
| 6b4 | Transaction-only joins, subqueries, correlation and LATERAL | Done; committed at `f3cfffd`; phase-conversion guard retained. |
| 6b | Joins, aliases, correlation and lateral queries | COMPLETE; 6b1–6b4 delivered, all 100 catalogue rows ported. |
| 6c1 | Complete record/projection adapters and transaction-only composition | Done; committed at `638dc34`. |
| 6c2 | CTEs, recursive CTEs and record/scalar set operations | Done; committed at `5cbe31d`. |
| 6c3 | Completed-result cursors, read/page adapters, result locks and sensitivity | Done; committed in integration batch 5 at `5873773`. |
| 6c | Rows/projections, CTEs, recursive queries, sets and result cursors | COMPLETE for the approved slices. 6c1–6c3 delivered; `transaction_nullable_record` is delivered in 6b4 (73/73 catalogue rows). Projection streaming through rows.Each is delivered in datatable slice 1. |
| 6d1 | Windows, ranking/value functions and typed frames | Done; committed in integration batch 5 at `5873773`. |
| 6d2 | Text, temporal, interval and geographic functions | Done; committed in integration batch 5 at `5873773`. |
| 6d3 | Typed JSON paths and document functions | Done; committed at `05b830c`. |
| 6d | Windows, JSON, temporal, interval, geographic and text expressions | COMPLETE; 6d1–6d3 delivered, all 60 catalogue rows ported. |
| 6e1 | Column/constraint conflict policies and bounded upserts | Done; committed at `5cbe31d`. |
| 6e2 | Computed/partial conflict targets and stored/proposed calculations | Done; committed in integration batch 5 at `5873773`. |
| 6e3 | Insert-select and source update/delete | Done; source writes committed at `05b830c`; all six declaration-role negatives now ported. |
| 6e4 | Creation/lookup writes, guarded adjustments, prune and explain | Done pending the lead’s commit; provenance and AfterCommit hardening included. |
| 6e | Conflict/upsert, source writes, insert-select, adjustments, prune and explain | COMPLETE; all 57 catalogue rows ported. |
| 6 | Expressions, composition and advanced writes | All approved slices done; Stage 6 whole-app measurement gate remains pending. |
| 7s1 | Outbox store and publisher migration | Done; committed in integration batch 5 at `5873773`. |
| Typed roles | Managed timestamps, input/source-write roles and soft-delete authority | Done; committed at `f3cfffd`; six deferred 6e3 rows ported. |
| 7s2 | Seven more stores and scoped keyed writes | Done; committed at `05b830c`; 8/13 store packages migrated. |
| 7 capabilities | UUID seeks and bounded lifecycle writes | Done; committed at `05b830c`; prerequisites for later store migrations. |
| 7s3a | Extension owner/slots, pagination, model binding and factories | Done; committed at `f3cfffd`; redacted lookups and lifecycle-aware InsertEach. |
| 7 datatables | Projection adapters, datatable cutover, reporting DTOs and contract/editor gates | Done pending the lead’s commit; slices 1–3 delivered. |
| 7s3b | Last five stores and bounded projection consumers | Done pending the lead’s commit; all 13 framework stores use ORM declarations. |
| 7 validation | Typed advisory database Unique/Exists rules | Done pending the lead’s commit; five catalogue rows ported and B34 editor anchors retained. |
| 7 remainder | Application registration, seeder, pagination consumers and contained/redacted boundaries | Done pending the lead’s commit; application.WithTables and model-owned Tables lists are explicit. |
| 7 | Framework integrations and wire-contract parity | COMPLETE for the approved slices: all 13 stores, validation/database, extension slots, datatables, application registration, seeding and pagination adapters. Whole-program acceptance remains separate. |
| 8a lane B | Shared limits, concrete observer owner and ORM prune registry | Done pending the lead’s commit; generated-query compatibility retained. |
| 8a lane C | Shared page and cursor contracts | Done pending the lead’s commit; querycore owns contracts, public aliases and HTTP schema identities retained. |
| 8a lane D | Extension loading and identity backing contracts | Done pending the lead’s commit; shared internal loader, legacy compatibility entry points retained. |
| 8 D8 | Handwritten ORM model/extension scaffolds | Done pending the lead’s commit; existing flags, collision-safe names and DTO generation retained. |
| 8 | Retire model generation and old query API; extend scaffold | In progress; Stage 8a and D8 delivered. Model/query emitter retirement remains Stage 8c. |
| 9 wave 1 | B1 foundations, lane C reporting/interval/transaction consumers and D1 foundations | Done pending the lead’s commit; generated callers and parity oracles coexist. |
| 9 B2/B3 | Lifecycle consumers, observer counts, typed unset and soft-model batches | Done pending the lead’s commit; bounded lifecycle writes and stored/input identities retained. |
| 9 C A1 | Shared models/catalog/query fixtures and eleven dependent query packages | Done pending the lead’s commit; original result identities and generated parity oracles retained. |
| 9 C clauses | Scalar windows and correlated HAVING/DISTINCT/DISTINCT ON; lateral consumers | Done pending the lead’s commit; remaining terminal/stream adapters stay with their owners. |
| 9 A read/filter | Ordinary read inspection/iteration, callback identity and relation expression filters | Done pending the lead’s commit; one shared iteration owner and retained construction errors. |
| 9 B upsert | Shared-model conflict/upsert consumer cutover | Done pending the lead’s commit; 24 consumer negatives migrated with existing catalogue dispositions. |
| 9 C results | Scalar GROUP BY/HAVING/DISTINCT/DISTINCT ON and record/set DISTINCT | Done pending the lead’s commit; eight package functions, runtime consumers and seven new Go-way negatives. |
| 9 A2 | Application/profile, pivot lifecycle, relationship-filter and correlation consumers | Done pending the lead’s commit; ordinary ORM tables and thin application registration. Office relation-value callers are integrated in batch 7e. |
| 9 D3/client | Recovery/authentication, nested binding, service and client-contract consumers | Done pending the lead’s commit; six Stage 7 catalogue rows ported. Pagination clause callers are completed in batch 7f. |
| 9 C keys | Correlated grouping/selected ordering and value-set selected DISTINCT ON | Done pending the lead’s commit; four package functions, ten negatives and one bounded probe. |
| 9 A3/load | Office annotations, tooling/HTTP models and existing-parent relation limits | Done pending the lead’s commit; RelatedValue/WithValue and LoadFrom/LoadMissingFrom retain shared hydration and batching. |
| 9 D matching/D4 | Extension matching, attachment batches, diagnostic redaction and root/editor harness | Done pending the lead’s commit; lane B’s nestedbindings remains authoritative. Root relation callers are migrated in batch 7f; generated cleanup remains open. |
| 9 B reads | Retrieval streaming, scalar/DTO/complete-record pagination and safe draft-helper cleanup | Done pending the lead’s commit; EachRead retains callback identity and bounded iteration. |
| 9 C terminals | Derived aggregates, JSONKind, complete-record DISTINCT ON, relation ordering and cursor inspection | Done pending the lead’s commit; scalarqueries/keyqueries and dependent query consumers migrated. |
| 9 D loaders | Root direct, joined and aggregate relation tests; editor probes | Done pending the lead’s commit; LoadFrom/LoadMissingFrom preserve query-owned budgets and all existing assertions. |
| 9 | Migrate consumer fixtures and compiler catalogue | Catalogue complete: 587 ported, 445 unaffected, zero pending. Batch 7g removes independent generated consumers; the barrier remains in progress pending the oracle freeze and remaining legacy owners. |
| 10 | Guides, examples and editor coverage | Planned |
| 11 | Full acceptance and whole-app performance evidence | Planned |
| 12 | Consumer application migration, separately authorized | Later |

Stage 1d used Go 1.27.1 on darwin/arm64. The matched four-model package emitted
7,808 functions versus 22,973 (33.99%); its approved ceiling was revised to 40%.
The one-query probe emitted 901 versus 4,186 (21.52%, within 30%). All 56 SQL,
binding and hydrated-value comparisons matched, and 20 compiler-negative cases
passed. Runtime allocation counts were 6 → 6 for construction, 56 → 56 for SQL
compilation and 5,069 → 5,065 for hydration. Median SQL compilation time changed
−0.92% and hydration +1.11%; these are historical observations, not a latency
speedup claim. Some archive/symbol byte counts grew despite stable function counts.

The [synthetic harness](../../tools/goway_synthetic.py) measured 70 models,
40 handlers and 5 reports (seed 1; Go 1.27.1, darwin/arm64, fresh caches, old then
new). The system load average was 9 to 11 from a concurrent build, so the build
time is one noisy observation; the sizes, function counts and memory are deterministic:

| Measure | Old API | Go-way | Change |
| --- | ---: | ---: | ---: |
| Whole-app archives / functions | 8.80 GB / 2,178,194 | 2.23 GB / 569,985 | −75% / −74% |
| Build cache | 8.95 GB | 2.40 GB | −73% |
| Cold build | 204 s | 73 s | −64% |
| `models` archive / functions | 1.68 GB / 424,238 | 558 MB / 138,200 | −67% / −67% |
| `models` compiler peak RSS | 22.3 GB | 7.3 GB | −67% |
| Handler median archive / functions / RSS | 167 MB / 41,432 / 2.1 GB | 38 MB / 9,820 / 551 MB | −77% / −76% / −74% |

Column types are about 62% of the Go-way `models` functions (85k of 138k); value
methods are compiled again at each embedding level and as pointer wrappers. Stage 3.0
must reach `models` ≤ 25% of the old functions and bytes (≤ 106k, ≤ 420 MB) and
compiler RSS ≤ 5 GB at 70 models. Handlers must be no worse, runtime within ±5% with
no extra allocations, and SQL, compiler cases and editor probes unchanged. It then
adds per-kind and per-model slope budgets.

## Stage 3.0 column footprint

Flattened column kinds and replaced the private relation bridge’s public `Column`
result with method-free state. Every exported method set/signature remains pinned
by the [surface test](../../database/orm/method_surface_test.go). Rows/expr already
declared their methods directly. No method-set trim or Stage 3.1 work is included.

Go 1.27.1, darwin/arm64, seed 1, sequential new-module builds with `-p=2` and
`GOMAXPROCS=4`. RSS is native `/usr/bin/time` maximum RSS for the models compiler.
The task cache was reused; these are not cold-build time claims. MB/GB are decimal.

| Candidate | N=10 functions / MB / RSS GB | N=20 functions / MB / RSS GB | Functions per added model |
| --- | ---: | ---: | ---: |
| Baseline | 20,990 / 84.6 / 1.15 | 40,407 / 162.9 / 2.20 | 1,941.7 |
| 1: direct methods | 16,767 / 69.6 / 0.95 | 32,137 / 133.6 / 1.74 | 1,537.0 |
| 2: method-free bridge | 14,893 / 61.6 / 0.84 | 28,426 / 117.7 / 1.66 | 1,353.3 |
| 3: value-owned codec state (retained) | 14,192 / 58.6 / 0.82 | 26,953 / 111.5 / 1.46 | 1,276.1 |

The linear 70-model estimate was 90,758 functions, 376.0 MB and 4.68 GB RSS.
N=20 handler medians also fell: 6,249 → 4,404 functions and 25.6 → 18.2 MB.

The 70-model gate used the [synthetic harness](../../tools/goway_synthetic.py) with
the same seed, sources, flags and machine as the Stage 2b run. It was a cold
sequential build of the old app, then the new one. The old app reproduced the same
archives and function counts, which makes it a valid control. Its compiler RSS
varied by 4% between runs. Compiler RSS for `models` is the median of the gate run
and three recompiles of that package alone:

| 70-model measure | Before 3.0 | Stage 3.0 | Gate | Result |
| --- | ---: | ---: | ---: | --- |
| `models` functions | 138,200 | 90,922 (21.4% of old) | ≤ 106,000 | Pass |
| `models` archive bytes | 557,803,758 | 376,898,108 | ≤ 420 MB | Pass |
| `models` compiler RSS | 7.34 GB | 5.14 GB median (5.28, 5.13, 5.12, 5.14) | ≤ 5 GB | Miss by 2.7%, accepted |
| Handler median functions / bytes / RSS | 9,820 / 38.2 MB / 551 MB | 8,077 / 31.2 MB / 450 MB | no worse | Pass |
| Whole-app archives / functions | 2.23 GB / 569,985 | 1.76 GB / 449,094 | report | −21% / −21% |
| Build cache / cold build | 2.40 GB / 72.9 s | 1.93 GB / 46.6 s | report | −20% / −36% (one run) |

The remaining options for reaching 5 GB change the API. The method-set trim below
would move methods to package functions and degrade completion, so the miss was
accepted rather than trimmed. Compiler RSS stays a tracked metric at Stages 6 and
11, and the footprint budgets stop growth. Interleaved six-round benchmarks against
the pre-3.0 commit on the lead's machine: ChainBuild −0.5%, ChainCompile +0.2%,
Hydrate1000 −0.1%, with identical allocations. The method-surface test also passes
against the pre-3.0 code.

| N=20 generic origin | Before functions | Retained functions | Retained pointer wrappers |
| --- | ---: | ---: | ---: |
| `orm.Column` | 6,912 | 1,746 | 873 |
| `orm.ordered` | 3,920 | 0 | 0 |
| `orm.nullableOrdered` | 1,836 | 0 | 0 |
| `orm.Number` | 2,560 | 2,560 | 1,280 |
| `orm.NullableText` | 1,920 | 1,920 | 960 |
| `orm.Query` | 2,000 | 2,000 | 1,000 |
| `orm.LockedQuery` | 1,120 | 1,120 | 560 |
| `codec.Codec` | 2,769 | 2,769 | 1,326 |
| `value.Optional` | 1,613 | 1,613 | 863 |
| `orm.bindTyped` | 624 | 184 | 0 |
| `orm.columnWithCodec` | 954 | 276 | 0 |
| `orm.bindNullable` | 81 | 6 | 0 |

Baseline value-method promotion wrappers disappear from the column family.
The compiler still emits pointer wrappers: an isolated real `orm.Number` probe
has 32 whether reached by only a typed parameter, a direct call, an interface,
a non-generic function adapter, or reflection (concrete plus shape instances).
Changing interface boxing alone therefore does not help. Pointer receivers would
break calls on returned values; positive probes retain `table.Key()`, derived keys
and query chains. Declaration reflection remains for all existing startup checks.

Candidate 3 confirms `Codec[V]` and `Optional[V]` already share by value
type. Their symbol inventories are unchanged; model-owned ID/result values are
distinct value types. Their construction helpers did carry a redundant model
parameter: `bindTyped`, `bindNullable` and `columnWithCodec` now use only `V`.
Model ownership stays on public handle markers and the private `ormColumn(*M)`
argument. This removes duplicate codec-construction closures without erasing types.

| Fixture probe | Before functions | Retained functions | New ceiling (15% headroom) |
| --- | ---: | ---: | ---: |
| model_type | 299 | 299 | 344 |
| table_ref | 901 | 803 | 924 |
| onequery | 901 | 803 | 924 |
| q_with | 1,593 | 1,053 | 1,211 |
| q_select | 934 | 836 | 962 |
| q_join | 2,238 | 1,598 | 1,838 |
| report_pipeline | 2,879 | 1,905 | 2,191 |
| models | 7,808 | 6,147 | 7,070 |
| column_number | 1,261 | 1,172 | 1,348 |
| column_nullable_text | 1,699 | 1,456 | 1,675 |
| column_time | — | 1,194 | 1,374 |
| column_enum | — | 1,232 | 1,417 |
| relation_with | — | 1,847 | 2,125 |
| models_1 | 1,917 | 1,619 | 1,862 |
| models_3 | 3,013 | 2,435 | 2,801 |
| Two-model growth (`models_3 − models_1`) | 1,096 | 816 | 939 |

The three review probes have no pre-flattening baseline. `relation_with` declares
one `BelongsTo` and executes a query with `With`. The surface snapshot now pins
exported fields (including promotion) and sealed interface methods. Stage 3.0
deliberately removes the promoted `.Column` field while retaining exported methods;
no branch consumer migration is needed.

| Benchmark | Before median ns/op | Retained median ns/op | Allocations/op (both) |
| --- | ---: | ---: | ---: |
| ChainBuild | 136.30 | 135.70 | 6 |
| ChainCompile | 1,695.50 | 1,695.00 | 56 |
| Hydrate1000 | 182,689.00 | 180,233.00 | 5,065 |
| InsertCompile | 1,957.00 | 1,950.50 | 59 |

Six runs per side on the same machine, grouped before/after; ranges overlap.
All medians are within ±2% and allocations are unchanged; no speedup is claimed.
The fixture’s insert benchmark remains an explicit skip (no public insert-only
compiler). The added internal benchmark measures planning and SQL compilation,
checks nonempty SQL and three bindings, and includes no driver execution.

If a later gate needs an API trim and it is approved, candidates are `Ne`, `NotIn`, `Between` and
specialized text prefix/suffix/insensitive matches moved to package functions.
That would change completion; it is not implemented or assigned an RSS saving.

All requested repository checks pass on the final source, including the golden
comparison and compiler negatives. Temporary synthetic apps and their archive
cache entries were removed after the builds exited.

Full attribution, samples, trigger counts, commands and cleanup are retained in
[the Stage 3.0 evidence](../../docs/evidence/goway-stage3-column-footprint.json).

The permanent [footprint test](../../tests/fixtures/goway/footprint_test.go) now uses
the Stage 3.0 ceilings above. Its original Stage 1d measurements and ceilings are
retained here as historical evidence:

| Probe | Stage 1d functions | Budget |
| --- | ---: | ---: |
| model_type | 299 | 344 |
| table_ref | 901 | 1,037 |
| onequery | 901 | 1,037 |
| q_with | 1,593 | 1,832 |
| q_select | 934 | 1,075 |
| q_join | 2,238 | 2,574 |
| report_pipeline | 2,879 | 3,311 |
| models | 7,808 | 8,980 |

The [56-statement comparison](../../tests/fixtures/goway/golden_test.go) and
[compiler cases](../../tests/fixtures/goway/compiler_cases_test.go) are permanent
branch guards. Historical scratch acceptance does not verify a later revision.

## Stage 3.1a declarations and codecs

[Source and acceptance evidence](../../docs/evidence/goway-stage31a-declarations-codecs.json)
records the 30 compiler-case ports, surface additions and unchanged budgets.

`JSONOf` and `NullableJSONOf` retain typed documents, equality/membership,
JSONB containment, assignment, `Asc`/`Desc` and `IsJSONNull`; they expose no range
comparisons or text patterns. `IntervalOf` and `NullableIntervalOf` retain ordered interval comparisons,
sorting and assignments. All four handles declare direct methods over shared
state (R9), because existing kinds do not express these capability boundaries.
JSON Kind/Length expressions, paths and interval functions remain Stage 6d.
Interval `Sum`/`Avg` (the old private `summaryData` capability) remain Stage 6a.
A parity test compares the core method signatures against the old JSON/interval fields.
Existing staged column-comparison, grouping, subquery and conflict APIs remain
with their owning slices; interval adds no numeric adjustments or text patterns.
`IContains` delegates to the existing case-insensitive literal-pattern compiler.
Natural keys use explicit codecs where needed; binary snapshots and typed ID aliases
retain their ownership. No Query/Table/LockedQuery method was added.

The committed Stage 3.0 measurements are the “before” values below. Existing
ceilings stay unchanged. The two-model delta is 816 → 820 (ceiling 939).

| Probe | Before functions | 3.1a functions | Ceiling |
| --- | ---: | ---: | ---: |
| column_time | 1194 | 1194 | 1374 |
| column_enum | 1232 | 1232 | 1417 |
| relation_with | 1847 | 1847 | 2125 |
| model_type | 299 | 299 | 344 |
| table_ref | 803 | 803 | 924 |
| onequery | 803 | 803 | 924 |
| q_with | 1053 | 1059 | 1211 |
| q_select | 836 | 836 | 962 |
| q_join | 1598 | 1604 | 1838 |
| report_pipeline | 1905 | 1914 | 2191 |
| models | 6147 | 6163 | 7070 |
| column_number | 1172 | 1172 | 1348 |
| column_nullable_text | 1456 | 1460 | 1675 |
| models_1 | 1619 | 1623 | 1862 |
| models_3 | 2435 | 2443 | 2801 |
| column_json | — | 1280 | 1472 |
| column_nullable_json | — | 1522 | 1751 |
| column_interval | — | 1178 | 1355 |
| column_nullable_interval | — | 1406 | 1617 |

PostgreSQL sources cover every primitive numeric width, bool/text, temporal values,
IDs, enums, nullable NULL/value, typed JSON/JSON null, interval components, owned
binary buffers and empty bytes, natural keys, and malformed stored JSON/enum values.
They use retained `postgres.Isolate` schemas. PostgreSQL execution is lead-owned.

## Decisions

| Decision | Adopted direction |
| --- | --- |
| D1 | `database/orm`, `orm/expr` and `orm/rows`; retire `database/query` at Stage 8. |
| D2 | A typed table struct returned by `orm.Define`, with a separate relation namespace to avoid initialization cycles. Measure the cost of the entire table declaration. |
| D3 | Explicit capability constructors: number, text, enum, generic column, nullable and temporal families. Custom representations require an explicit codec. |
| D4 | Validate field offsets at declaration/startup; use those offsets on the hot path. |
| D5 | Assignment lists (`column.Set(value)`) preserve omitted/null/value distinctions. |
| D6 | Work in this branch/worktree. The lead owns commits, pushes and synchronization with main. |
| D7 | application.WithTables snapshots an explicit CheckedTable list and validates it through orm.Check before constructing providers/handlers. Model packages expose fresh Tables() lists; no registry or startup I/O. |
| D8 | Extend the existing `foundry make` scaffold to write an initial model declaration once. |
| D9 | Coexist with the old API until Stage 8; migrate subsystems before removal. |
| D10 | Stage 1 is the gate. Its approved model ceiling is 40%, superseding the original 25%; retain measured usage budgets. |
| D11 | Keep executor raw SQL, add typed raw hydration and opt-in fragments; provide an optional project no-raw guard. |
| D12 | Increment/decrement accepts a strictly positive finite magnitude. Reject zero, negative zero, negatives, NaN and infinity; use atomic `AtLeast`/`AtMost` bounds. `validation.Positive` and `DecimalPositive` exist on the branch. |

**Primary-key refinement:** bind the key once with `TableOf(b, &m.ID)` to obtain
`Table[M,K]`. Use `table.Key()` in the declaration. `Find`/`Require` retain the
concrete `K`; the earlier `KeyOf` sketch is superseded. **Capability refinement:**
`NumberKey` and `TextKey` derive capabilities from that key and reuse its codec.
Binding the primary field twice is a startup error, not another column declaration.

## Old to new API

Names in planned rows remain subject to their owning stage's consumer acceptance.

| Old call/declaration | New call/declaration | Status |
| --- | --- | --- |
| `QueryOrders().Where(OrderFields().TotalCents.Gt(100)).All(ctx, db)` | `Orders.Query().Where(Orders.TotalCents.Gt(100)).All(ctx, db)` | Core ported |
| `QueryOrders().Find(ctx, db, id)` | `Orders.Find(ctx, db, id)` | Ported |
| `q.CursorPaginate(ctx, ex, request)` | `orm.CursorPaginate(ctx, ex, q, request)` | Stage 3.1c; unchanged HTTP request/result adapters |
| `q.ChunkByID(ctx, ex, size, visit)` | `orm.ChunkByKey(ctx, ex, q, Orders.ID, size, visit)` | Stage 3.1c |
| `q.EachByID(ctx, ex, size, visit)` | `orm.EachByKey(ctx, ex, q, Orders.ID, size, visit)` | Stage 3.1c |
| `q.ForUpdate().RequireFind(ctx, tx, id)` | `q.Where(Orders.ID.Eq(id)).ForUpdate().Require(ctx, tx)` | Stage 3.1c; transaction-only |
| `QueryOrders().Create(ctx, tx, OrderDraft{}.SetTotalCents(v))` | `Orders.Insert(ctx, tx, Orders.TotalCents.Set(v))` | Core ported |
| `QueryOrders().With(OrderRelations().Buyer)` | `Orders.Query().With(OrderRelations.Buyer)` | Direct eager trees ported, Stage 4b |
| `defineOrderRelations() OrderRelationSet` | Separate typed relation namespace using direct, through and morph `orm.*Of` constructors | Direct kinds ported in 4b, joined families in 4c; typed aggregate slots delivered in 4d |
| `q.WhereHas(UserRelations().Orders)` | `q.Where(orm.Has(UserRelations.Orders))` | Direct kinds in 4b, joined families in 4c; target predicates and nested `Has` compose |
| `q.WhereDoesntHave(UserRelations().Orders)` | `q.Where(orm.DoesntHave(UserRelations.Orders))` | Direct kinds in 4b, joined families in 4c; correlated `NOT EXISTS` |
| `r.Orders.Where(p).OrderBy(o).With(child)` | `r.Orders.Where(p).OrderBy(o).With(child)` | Stage 4b; immutable target-owned constraints and nested branches |
| `q.WithRelationLimits(limits)` | `orm.WithRelationLimits(q, limits)` | Stage 4b; package function, shared limits type and defaults |
| `q.With(r).Load(ctx, ex, parents)` | `orm.Load(ctx, ex, parents, r)` | Direct and joined families ported; no parent statement |
| `q.With(r).LoadMissing(ctx, ex, parents)` | `orm.LoadMissing(ctx, ex, parents, r)` | Stage 4b; loaded-empty slots stay loaded |
| `ManyToMany(...)` with a `relation.Through[N,P]` slot | `orm.ManyToManyOf(slot, local, pivotLocal, pivotForeign, foreign)` with `orm.ThroughOf` binding | Stage 4c; thin `ThroughRelation[M,N,P]` descriptor and `relation.Link[N,P]` results |
| `r.WherePivot(p).OrderByPivot(o).WithPivot(child)` | `r.WherePivot(p).OrderByPivot(o).WithPivot(child)` | Stage 4c; typed pivot constraints and nested pivot branches |
| `HasOneThrough(...)` / `HasManyThrough(...)` | `orm.HasOneThroughOf(...)` / `orm.HasManyThroughOf(...)` | Stage 4c; both key pairs checked, singular and many slots retained |
| `r.OfMany(o)` / `r.LatestOfMany()` / `r.OldestOfMany()` | Same methods on singular descriptors | Stage 4c; immutable `DISTINCT ON` option with primary-key tie-breaker |
| `MorphOne` / `MorphMany` / `MorphTo` / `MorphToMany` / `MorphedByMany` | Corresponding `orm.*Of` constructor and one concrete slot per target | Stage 4c; validated discriminator and eligible-key batches |
| Generated aggregate slots loaded with `With` | `orm.ValueOf`, slot `.Using(expr.Related(...))`, optional `orm.Pivot(...)` | 4d; `With`/`Load`/`LoadMissing` unchanged |
| Generated retrieval hooks/observers | `orm.WithRetrievalHooks`, `NewRetrievalObserver`, `RegisterRetrievalObserver` | B5c; closed complete-model batches |
| `defineXGlobalScopes` and scope bypasses | `orm.WithGlobalScopes`, `Scope`, `WithoutGlobalScope(s)` and relation/pivot package bypasses | B5c; executing context retained |
| Transaction selectors and LATERAL | `rows.Transaction*` composition, transaction scopes and `AsTransactionLateral` | 6b4; only transaction terminals execute |
| Generated source-write field eligibility | `PrimaryOf`, role-aware `MapInsert`/`MapUpdate`, `SoftDeletes` token | Typed roles; Input carriers cannot enter source mappings |
| Generated extension declarations and factories | `SlotOf`/`IdentityOf`, `extensions.DefineORMOwner`, `slots.DeclareORM`, `factory.Define` | 7s3a; old adapters coexist |
| Generated scalar/IN/EXISTS selectors | `rows.SelectScalar`, `ScalarQuery`, `InQuery`, `ExistsQuery` and nullable variants | 6b2; selection sources cannot enter joins without aliasing |
| Correlated and lateral selectors | `rows.Correlate`, correlated scalar/IN/EXISTS and cross/inner/left LATERAL functions | 6b3; explicit enclosing scope |
| Generated audit policy/capture/readers | `orm.AuditPolicyOf`, `Audit`, `Auditing`, `AuditRead` and nullable/encrypted variants | B5d; shared record policy and instance-owned recorder |
| Complete model/projected record sources | `rows.ModelRecord`, `RecordOf`, `AsRecord`, `SelectRecord` | 6c1; preserved scopes, read-only results |
| Transaction record/projection sources | `rows.TransactionOf`, `LockedRecord`, `AsTransaction`, `SelectTransaction`, `SelectTransactionRecord` | 6c1; `All` requires `*database.Tx` |
| Raw selected expressions and predicates | `expr.Raw`, `RawWithCodec`, `orm.WhereRaw` | 6a4; explicit argument/result boundary and optional no-raw lint |
| Generated `Distinct`/set query surface | `rows.Distinct(...)`; `rows.Union`/`Intersect`/`Except`, ALL and scalar variants | DISTINCT delivered 6a3; sets delivered 6c2 |
| Pivot attach/detach/sync/toggle/update | `orm.Attach`, `Detach`, `Sync`, `Toggle`, `UpdatePivot` and batch/force variants | 5b; package functions, concrete endpoints and pivot assignments |
| Ordinary/transaction CTEs and recursion | `rows.CTE`, `TransactionCTE`, `RecursiveCTE`, `RecursiveAllCTE` | 6c2; immutable definitions and non-executable recursive self |
| Generated conflict/upsert wrappers | `orm.OnConflict`, `OnConflictConstraint`, `ConflictSet`/`Incoming`/`Null`, `Upsert`/`UpsertMany` | 6e1; explicit immutable policy and typed optional/batch results |
| Completed-result and scalar cursors | `rows.CursorFor` / `ValueCursorFor`, explicit `UniqueBy`, `Paginate` | 6c3; output identity and sensitivity checked before SQL |
| Result read/page and lock terminals | `rows.Read*`, `SelectTransactionValue`, `ForUpdate`/other lock functions and `LockRows` | 6c3; retained inner windows and transaction-only locks |
| Window/ranking and scalar function families | `expr.WindowFor`, `Over`, typed frame/range functions and scalar package functions | 6d1–6d2; row and selected phases remain distinct |
| Computed/partial conflict targets and calculations | `rows.OnConflictKeys`, `ConflictRowsOf`, `SetConflictValue`, target/row predicates and scalar adapters | 6e2; stored/proposed scopes are not SELECT sources |
| Generated creation and lookup writes | `orm.CreateEach`, `FirstOrCreate`, `UpdateOrCreate`, `CreateOrFirst` | 6e4; bounded lifecycle and no automatic retry |
| Generated prune and explain | `orm.PruneBatch`/`ForcePruneBatch`, `orm.Explain*`, `rows.Explain*` | 6e4; exact soft token and transaction-only locked variants |
| `prune.Model` | `prune.ORMModel` / `ORMSoftModel` | 8a lane B; legacy registration remains in its compatibility file |
| Datatable generated selections | `rows.Query`, `expr.Selected`, `expr.ProjectionOrder`, row/relation predicates | Single runtime cutover; DTO generation retained |
| Insert-select and source update/delete | `rows.InsertFrom`, `UpdateFrom`, `DeleteUsing`/`ForceDeleteUsing`, `MapInsert`/`MapUpdate`, `MatchSource` | 6e3; exact scoped mappings, Exec or bounded Returning |
| Generated JSON path accessors | `expr.JSONFieldOf`, `JSONRoot`, `JSONProperty`, typed key/index/scalar adapters | 6d3; startup validation, no generated path catalogue |
| Scoped keyed Patch/Remove | `orm.Update` / `orm.Delete` with scoped query, own key and typed identity | 7s2; ordinary lifecycle and scoped cardinality |
| Ordered UUID predicates | `orm.UUIDLess`/`UUIDGreater` and inclusive variants; scoped/nullable `expr` variants | Stage 7 capabilities; PostgreSQL byte ordering |
| Per-model UpdateEach/DeleteEach | `orm.UpdateEach` / `orm.DeleteEach` package functions | Stage 7 capabilities; complete candidate locking and one atomic owner |
| `query.SelectValue(QueryOrders(), ...)` | `rows.SelectValue(Orders.Query(), ...)` | Basic scalar selection ported |
| Model annotation plus `foundry generate` | Plain struct plus `orm.Define` | Core ported; DTO/enum generation retained |

## Feature parity contract

“Preserve” states the program's target, not completion of the feature. The current
consumer retains Stage 1 golden coverage and adds core, direct and joined-relation cases.

| Capability | Old owner/shape | New shape and owning stage |
| --- | --- | --- |
| IDs, nullables, numeric/temporal values, enums and bytes | Plain structs and generated codecs | Constructors/codecs retained; natural keys and binary ownership covered in 3.1a |
| JSON documents and intervals | Generated JSON/interval fields | `JSONOf`/`NullableJSONOf`: equality, membership, containment, assignment, `Asc`/`Desc` and `IsJSONNull`; no range comparisons/text patterns. `IntervalOf`/`NullableIntervalOf`: ordered comparisons, sorting and assignment. Interval functions delivered in 6d2; JSON Kind/Length and typed paths delivered in 6d3 |
| Password and encrypted fields | Typed values and lifecycle codecs | Stage 3.1b: password scalar capabilities; encrypted typed assignments/null predicates, shared sealing/opening, transaction-keyring enforcement |
| Column/default tags | Generator reads tags | Startup metadata reads the same tags, Stage 3 |
| Table name/options | Model annotation | `orm.Define` and typed options, Stage 3 |
| Accessors | Handwritten `Access<Field>` methods | B5a: exact startup signature and persisted-field validation; explicit calls remain ordinary Go methods and never replace stored hydration |
| Mutators, including distinct input/stored types | `Mutate<Field>` plus generated input fields | B5a: typed named methods bound once at declaration or explicit `orm.Mutate`; required/nullable input-column kinds, once-only transactional transforms and redacted errors preserving cause identity |
| Lifecycle and per-write factory state | Generated hooks and drafts | B5b delivered: `WithHooks(func() Hooks[M])`, typed Write input getters, all 13 stages, local-before-observer order per stage, locked originals and closed RETURNING rows |
| Write observers | Generated adapters and registration | B5b delivered: `NewObserver`/`NewDeletionObserver` and `RegisterObserver`, instance-owned sets including observer-only models; trusted concrete-owner discovery and actual-transaction dispatch for wrappers |
| Retrieval hooks | Generated adapters | B5c: separate RetrievalHooks/RetrievalObserver factories, complete closed batches and actual row-owner dispatch; Table.Raw included, write/DTO/scalar hydration excluded |
| Timestamps and soft deletes | Field-name conventions and opt-out | Core ported; B5b applies conventions after before hooks, validates the effective draft before mutators/encryption and captures actual stored changes |
| Integer, UUID and natural keys | `primary=` annotation | `TableOf` binds concrete `K`; B5a `InputKeyOf` accepts distinct assignment inputs while retaining stored lookup/predicate K. Distinct inputs disable automatic UUID synthesis |
| Global scopes | `defineXGlobalScopes` | B5c: lazy WithGlobalScopes with completed typed descriptors, additive context predicates and explicit query/target/pivot bypasses; sticky source failures prevent SQL |
| Direct belongs-to, has-one, has-many, self and nullable-FK relations | Slots and generated relation sets | `BelongsToOf`/`HasOneOf`/`HasManyOf`; delivered in 4b, including singular cardinality checks |
| Nested and constrained eager loads; LoadMissing | Descriptor `Where`/`OrderBy`/`With`, query `Load`/`LoadMissing` | Stage 4b: immutable descriptors, complete target batches at each level, package `Load`/`LoadMissing`, loaded-empty preservation and primary-key order tie-breakers |
| Relation limits and depth | Query `WithRelationLimits` and shared loading budgets | Stage 4b: `orm.WithRelationLimits(q, limits)`; shared row/attachment budgets and depth bound, defaults from the existing owner. No relation `Limit`/`Offset` or per-parent limits |
| Relation existence filters | `WhereHas`/`WhereDoesntHave`, descriptor `Exists` | Stage 4b: `Where(orm.Has/DoesntHave(...))`, nested correlated subqueries with related scopes and zero extra statements |
| Pivots and WithPivot | Generated relation sets, `Through` slots and joined loaders | Stage 4c delivered: `ThroughOf` and `ManyToManyOf`, typed links, target/pivot constraints and nested branches, independent pivot ownership and soft-delete scopes |
| Has-one/has-many-through | Joined hop loaders | Stage 4c delivered: `HasOneThroughOf`/`HasManyThroughOf`, both key pairs checked and shared batched plans |
| One-of-many | Latest, oldest and custom singular choices | Stage 4c delivered: immutable `OfMany`/`LatestOfMany`/`OldestOfMany`, `DISTINCT ON` per parent and primary-key tie-breaker |
| Polymorphic relations | MorphOne/Many/To/ToMany/MorphedByMany | Stage 4c delivered: corresponding `orm.*Of` constructors, validated `MorphName`, concrete target slots and discriminator-group batching |
| Relation aggregate slots | Generated computed slots | 4d: ValueOf binds relation.Value slots; expr.Related and Pivot reuse exact 6a3 result families/FILTER, existing loaders and per-slot grouped batches |
| Relation writes and pivot lifecycle | Attach/detach/sync/toggle/update-pivot | 5b: typed package functions; scoped locks, shared B5b callbacks, set writes without callbacks, bounded RETURNING and typed reconciliation candidates |
| Stored change sets | Generated `CompareX` and field structs | B5b delivered: `Changes`, `Compare`, typed `ChangeOf`/nullable/encrypted variants, persisted-only owned snapshots, Assigned versus Changed and existing FieldChange codec policy |
| Set writes | Explicit bulk lifecycle limits | B5b: `WithoutModelHooks(q)` acknowledges skipped hooks/observers/change capture; required with declared hooks, active observers or unknown executor wrappers; rejected for per-model writes |
| Force-delete visibility | Active default and scoped physical deletion | B5b: table ForceDelete keeps active visibility; package `orm.ForceDelete` retains supplied query visibility and predicates, including WithTrashed/OnlyTrashed |
| Draft, batch, lookup, conflict and source writes | Generated drafts/wrappers | Core assignments/batches retained; 6e1–6e3 deliver advanced conflicts and source writes, Stage 7 capabilities deliver bounded per-model writes. 6e4 completes lookup/prune/explain/adjustment work; six typed-role exclusions are enforced |
| Reference and identity | `FoundryReference`/`FoundryIdentity` | Table/column-based adapters retaining model/key and persisted identity, Stage 7 |
| Attachments, translations and metadata | Slot fields plus generated policies | Preserve fields, stored owner names, policies and cleanup; declarations in Stage 7 |
| Typed raw hydration | Executor-level raw only | Stage 3.1d: model/row decoders, exact named column set, no scopes or automatic routing |
| Chunks, pagination and locking | Generated wrappers and query runtime | Stage 3.1c: cursor/key-chunk package functions; keyed/locked lookup composition; shared tokens and unchanged HTTP shape |
| Audit/history and pruning | Generated wrappers and query runtime | B5d audit/history and 6e4 pruning delivered; 8a ORM registry uses shared limits and lifecycle/mass modes; redaction, transaction and outcome contracts retained |
| Arithmetic, casts and comparisons | Generated fields and query expressions | 6a1 delivered: typed row/selected package functions, explicit nullable variants and capability-specific comparisons |
| Count and existence aggregates | Selected query aggregates | 6a3 delivered: Count/Exists, CountField/CountDistinctField and immutable same-scope FILTER; counts skip NULL fields and return zero on empty input |
| Summary aggregates | Generated field Sum/Avg/Min/Max | 6a3: exact Sum/Avg return nullable decimal; float and interval variants preserve their families; sealed ordered Min/Max preserve the present type and return one nullable layer; ordered input handles have identical stored-value aggregate capabilities |
| GROUP BY, HAVING and selected ordering | Group/selected query clauses | 6a3: rows package functions, row grouping keys and separate selected keys/orders; shared compiler rejects invalid grouping and aggregate phases |
| DISTINCT and DISTINCT ON | Complete model/projection selection | 6a3: read-only rows boundary, including FromModel; row and selected keys retain ownership; no write methods |
| CASE, COALESCE and NULLIF | Conditional query expressions | 6a2 delivered: distinct row/selected phases, mandatory CASE fallback and one nullable layer; shared querycore validation |
| Join kinds and nullable scopes | Aliased inner/left/right/full/cross joins | 6b1: same shared compiler, distinct kind/side scopes, required/nullable column lifting and chained nullable scopes; broadened LeftScope/RightScope signatures approved |
| ON comparisons and subqueries | Join predicates and scalar/IN/EXISTS query families | 6b2: typed row-phase comparisons/composition, independent SelectionSource subqueries and sealed join sources; shared compiler validation |
| Correlation and LATERAL | Enclosing scopes and joined model/projected sources | 6b3: explicit correlation, nullable scalar/IN/EXISTS, cross/inner/left LATERAL with owned scopes; transaction record adapters delivered in 6c1; CTE rejection delivered in 6c2; transaction joins, subqueries and LATERAL delivered in 6b4 |
| Complete records and projection adapters | Generated complete selectors and derived records | 6c1: RecordOf/ModelRecord/AsRecord/SelectRecord, owned decoders, retained windows and retrieval callbacks; nullable record selection rejected |
| Transaction record/projection composition | Transaction-only generated selectors | 6c1/6c3: separate record/selection sources, scalar and read terminals with *database.Tx, retained inner locks; 6b4 adds transaction joins, subqueries and LATERAL |
| CTEs and recursive queries | Complete record definitions, materialization and recursive self | 6c2: ordinary/transaction CTEs, recursive UNION/UNION ALL; shared compiler enforces identity, self scope and executor restrictions |
| Record and scalar sets | UNION/INTERSECT/EXCEPT and ALL variants | 6c2: separate output scopes, exact records/value codecs/nullability, retained operand windows and read-only composition |
| Result cursors and read/page adapters | Generated query families | 6c3: explicit completed-output identities, nullable navigation, typed read/page/lock adapters; sensitive keys rejected across composed sources |
| Windows and frames | Generated window expressions | 6d1: typed Window/RangeWindow, ranking/value functions, aggregate Over, ROWS/GROUPS/RANGE and exclusions |
| Text, temporal, interval and geographic expressions | Generated scalar expression families | 6d2: typed row/selected and nullable variants, shared compiler and validation owners; JSON delivered in 6d3 |
| Conflict policies and upsert | Generated mutation wrappers | 6e1–6e2: column/constraint/computed/partial targets, typed literal/incoming/null and stored/proposed calculations, scoped scalar subqueries; scopes and proposed codecs/conventions retained, lifecycle callbacks explicitly skipped |
| Insert-select and source update/delete | Shared set-write compiler and generated mappings | 6e3: phase-distinct mappings/plans, scoped sources, defaults/conventions, bounded RETURNING and atomic cardinality checks; typed roles enforce all six declaration exclusions |
| JSON paths | Generated payload accessors | 6d3: handwritten JSONField/JSONFixedArray declarations, exact payload/key/index types and shared tag/codec/compiler validation |
| Scoped keyed and bounded per-model writes | Generated scoped writes and UpdateEach/DeleteEach | 7s2/capabilities: package functions preserve scope plus key, ordinary lifecycle, candidate locking, one transaction/savepoint and outcome distinctions |
| Audit capture and history | Generated policies and record adapters | B5d: immutable model-owned Automatic/Exclude/Redact, transactional observer capture, typed stored-value readers and shared sensitive redaction |
| Typed raw fragments | Explicit raw expression/predicate boundary | 6a4: Raw/RawWithCodec selected values and WhereRaw predicates, shared lexer and binding, scopes retained; no-raw CLI policy |
| Framework-owned stores | Generated model APIs and handwritten store logic | 7s1–7s3b: all 13 stores use ORM, including webhook, notification, attachment, extension and audit; schemas, identity codecs, scopes, lifecycle and outcomes retained |
| Model binding, factories and pagination | Generated metadata and typed query interfaces | 7s3a/8a lane C: Keyed/Unique/RelatedUnique, binding adapters, factory.Define/InsertEach; querycore page/cursor contracts retain public aliases and HTTP bytes |
| Extension owner/slot declarations | Generated extension descriptors | 7s3a/8a lane D: SlotOf/IdentityOf, DefineORMOwner/BindSlot/DeclareORM and thin typed handles; internal/extensionslot owns batching, budgets and deferred attachment; legacy bridges retained |
| Datatables | Generated SQL projections and DTOs | Slices 1–3: rows.Query/expr cutover, streaming exports, ordinary DTOs with handwritten schemas; frozen byte gates retain contracts |
| Database validation | Generated metadata and typed query interfaces | Delivered: ORM ValueLookup plus typed stored columns; scoped advisory Unique/Exists, batches of 64, redaction and unchanged rule metadata |
| Application tables and seeding | Generated declaration/check and seed adapters | Stage 7 remainder: explicit WithTables/CheckedTable lists, per-declaration callback containment and transaction-bound ORM seed writes |

Generated field hover notices move to handwritten comments and declaration docs.
Unsupported fields, undeclared persisted fields and invalid mutator signatures
move to aggregated startup errors before SQL. Owner, value and capability rules
remain compile-time constraints.

## Raw SQL levels

| Level | Boundary | Status |
| --- | --- | --- |
| 1 | Existing executor `Exec`, `Query`, `database.ScanOne` and read-routed `DB.QueryRead`; transactions unchanged | Retained |
| 2 | Typed model/declared-row hydration from raw SQL; validate selected columns rather than return partial models | Implemented Stage 3.1d: `Table.Raw` / `rows.Raw`, exact names in any order; no automatic scopes or read routing |
| 3 | Opt-in typed fragments (`expr.Raw[S,V]`, `RawWithCodec`, `orm.WhereRaw[M]`), composed with existing scopes and transaction | Delivered 6a4; selected expression phase, shared bindings and fail-closed fragment lexer |

All levels bind arguments instead of interpolating values. Raw fragments require
the caller to declare result types. A project may forbid raw call sites through
`foundry lint --no-raw ./...`, a best-effort source-policy guard. Typed advanced SQL remains a first-class parity
requirement; raw SQL does not substitute for porting its compiler rules.

The 6a1–6a3 typed expression API distinguishes row values, selected values and
HAVING conditions with different private phase markers. Explicit Go conversions
cannot move aggregates or HAVING into row WHERE. `rows.Map` keeps row-only input;
`rows.MapSelected` accepts selected values. `Grouped` explicitly lifts a predicate
for selected CASE, while the shared compiler still enforces SQL grouping rules.
Typed raw hydration remains level 2; these phase types do not add a raw fragment API.

## Compile-fail catalogue

The [mapping sheet](compilefail-mapping.tsv) covers all 1,026 existing cases. Its
rule column retains the actual invalid expression and expected diagnostic from
[the consumer catalogue](../../tests/fixtures/consumer/types_test.go). The case
column names its `testdata/compilefail/<case>/invalid.go` source. `ported` names
an existing Go-way negative case; multiple old cases may exercise one shared rule.
`port` names the feature stage that must supply its equivalent, before catalogue
migration in Stage 9. Retained identity/codec cases are explicitly called out even
though their APIs do not change. No rule is discarded merely because a generated
helper disappears; no case currently requires `obsolete` or `merge`.

[Catalogue validation](../../tools/goway_catalogue.py) checks exact coverage,
source/diagnostic drift, ported/merge references and a single table of valid
area/stage pairs. An `unaffected` case cannot reference generated model/store
symbols, including aliased imports. Its test also checks these summary counts,
so additions require an explicit mapping review.

Classification follows the rejected operation and expected diagnostic, rather than
the case prefix or incidental query setup. Basic column kinds, `Set`, `Find` and
`Eq` belong to Stage 3; specialized paths/functions belong to 6d. Relation aggregate
bindings belong to Stage 4. Per-row write and lookup-write families remain 6e,
with lifecycle integration in Stage 5; read `Each`/`Chunk` belongs to Stage 3.
All previously unaffected sources were rescanned against generated declarations;
twelve reference/store cases move to Stage 7. The original 20 Go-way cases were compared:
seven have no direct old catalogue equivalent (`enum_ordered`, `int_text_key`,
`number_key`, `joined_right`, `nullable_eq_null`, `required_null_test`,
`text_adjustment`); the others cover old rows. Stage 3.1a adds all 30 declaration/codec catalogue cases
as separate negative sources; identity and codec APIs are retained unchanged.
The binary comparable-scalar rejection uses `Key[M, []byte]`: general `Column`
intentionally accepts byte slices, while identity does not.

<!-- catalogue-summary -->

| Disposition | Cases |
| --- | ---: |
| ported | 587 |
| port | 0 |
| merge | 0 |
| obsolete | 0 |
| unaffected | 445 |
| **Total** | **1032** |

| Area | Cases |
| --- | ---: |
| Authentication and security | 94 |
| Bulk writes and conflict policies | 61 |
| Cache, Redis and coordination | 61 |
| Core reads, writes and codecs | 74 |
| Events, jobs and messaging | 54 |
| Expressions and aggregates | 70 |
| Framework integrations | 81 |
| HTTP, DTOs and transport contracts | 112 |
| Joins and correlation | 101 |
| Lifecycle, mutators and observers | 39 |
| Other framework services and contracts | 83 |
| Relations and pivots | 25 |
| Rows, CTEs and sets | 77 |
| Validation, values and localization | 40 |
| Windows and specialized expressions | 60 |

| Stage | Cases |
| --- | ---: |
| 2a | 21 |
| 3 | 60 |
| 4 | 23 |
| 5 | 39 |
| 6a | 70 |
| 6b | 100 |
| 6c | 73 |
| 6d | 60 |
| 6e | 61 |
| 7 | 80 |
| — | 445 |

587 old catalogue rows are covered by 579 distinct Go-way cases.

## Emitter audit

Audited against `internal/generate` on the branch. “Retire” means Stage 8 removes
the emitter after the listed replacement has parity. No emitter is removed here.
The dispatcher in [emit.go](../../internal/generate/emit.go) still owns model,
projection and contract generation. `model_*` does not mean every emitter whose
name contains `query`, `path` or `union`.

| Emitter source | Current output | Disposition / replacement | Contract preservation |
| --- | --- | --- | --- |
| [scaffold_developer.go](../../internal/generate/scaffold_developer.go) | `ModelScaffold` / `renderSlotModel` emit `//foundry:model` structs and slot instructions | Replace with one-time `orm.Define` declarations in existing `foundry make`, Stage 8; migrate scaffold consumers Stage 9 | Preserve DTO/enum scaffolding; model scaffolds do not directly emit TS/OpenAPI/manifest output |
| [field_documentation.go](../../internal/generate/field_documentation.go) | Generated field behavior comments for ownership, encryption, timestamps, soft deletion, accessors and mutator input types | Replace model notices with handwritten model/field and accessor/mutator comments, typed declaration symbol docs and `database/orm` package recipes (R7); retire notices Stage 8, editor/docs coverage Stage 10 | Preserve shared non-model documentation paths and behavior facts; comments are not a separate wire schema |
| [emit.go](../../internal/generate/emit.go), model body | Optional-valued draft, getters/setters/unset/clear, formatting; dispatches fields, query, hooks, changes and audit | Retire model branch; assignments Stage 3 and write state Stage 5 | Redacted diagnostics and omitted/null/value behavior remain |
| [model_query](../../internal/generate/emit_model_query.go) | Codecs, field metadata, hydration, query/ref wrappers, typed Find/RequireFind and fluent forwarding | `Define`, `Table[M,K]`, thin query and non-generic metadata, Stage 3 | `FoundryModel` is an integration adapter; replace its consumers in Stage 7 |
| [model_fields](../../internal/generate/emit_model_fields.go) | Ordinary/aliased/nullable field sets, capability operators, JSON/input field wrappers and hover notes | Column constructors Stage 3, scope binding Stage 6b, paths Stage 6d | Calls shared JSON-path emitter; do not delete that shared owner |
| [model_ownership](../../internal/generate/emit_model_ownership.go) | Binary input/stored codec selection and cloning at draft/snapshot boundaries | Bytes/nullable-bytes column codecs; distinct mutator-input ownership Stage 5 | Preserve snapshot ownership, including named byte slices |
| [mutation_input](../../internal/generate/emit_mutation_input.go) | Input-typed field wrappers embedding stored-value operators; input `Set` produces conflict assignments | Typed mutator input Stage 5 and conflict assignment Stage 6e | Broader than draft setters: stored predicates and write input must stay distinct |
| [model_mutation](../../internal/generate/emit_model_mutation.go) | CRUD, batch/upsert, draft preparation and create-mutation adapters | Assignments/core writes Stage 3; full batch/conflict Stage 6e | Keep UUID/default, mutator, transaction and integration semantics |
| [model_locking](../../internal/generate/emit_model_locking.go) | Locked fluent wrappers and typed-key locked lookup | Thin locked query, Stage 3 | Transaction capability must not be erased |
| [model_relations](../../internal/generate/emit_model_relations.go) | Relation sets, memoized key declarations and slot get/set bindings | Explicit typed relation namespace, Stage 4 | Preserve slots and batched loading |
| [model_aggregates](../../internal/generate/emit_model_aggregates.go) | Model-owned computed relation slots and aggregate bindings | Typed relation/aggregate declarations, Stage 4 with expression support Stage 6a | Preserve result types and loaded states |
| [model_hooks](../../internal/generate/emit_model_hooks.go) | Lifecycle callback structs, observer names, factories and database registration | Typed framework hooks/write state and observers, Stage 5 | Preserve lifecycle order, per-write state and after-commit outcomes |
| [model_retrieval_hooks](../../internal/generate/emit_model_retrieval_hooks.go) | Read callback structs, retrieval observer factories and registration | Retrieval options/observers, Stage 5 | Complete-model reads only; retain executor capability |
| [model_changes](../../internal/generate/emit_model_changes.go) | Persisted snapshots, field changes, comparison, operation state and redacted formatting | Column-keyed comparison/change API, Stage 5 | Preserve stored values, not accessor output |
| [model_each](../../internal/generate/emit_model_each.go) | Bounded atomic per-row create/update/delete/restore wrappers | Lifecycle-aware package functions, Stage 6e | Do not replace with hook-skipping set writes |
| [model_lookup](../../internal/generate/emit_model_lookup.go) | FirstOrCreate/CreateOrFirst/UpdateOrCreate and typed callbacks | Lookup-write functions, Stage 6e | Savepoint/unique-conflict semantics and branch-specific preparation remain |
| [model_set_writes](../../internal/generate/emit_model_set_writes.go) | UpdateAll/DeleteAll, numeric deltas, hook opt-out and update-mutation adapter | Core assignments plus full set-write/adjustment functions, Stage 6e | Preserve explicit hook opt-out; new adjustment rules follow D12 |
| [model_insert_select](../../internal/generate/emit_model_insert_select.go) | Source selection builder, literal draft values, Exec/bounded Returning | Typed insert-from-source functions, Stage 6e | Preserve mutator/timestamp exclusions, defaults and return bounds |
| [model_source_write](../../internal/generate/emit_model_source_write.go) | Update/delete using a source, key matching, stored selections and literal values | Typed source writes, Stage 6e | Preserve match ambiguity checks, soft deletion and transaction outcomes |
| [model_reference](../../internal/generate/emit_model_reference.go) | Concrete-key `FoundryReference`, serialized `FoundryIdentity` | Table reference/identity adapters, Stage 7 | **Persisted identity contract:** storage model name, key codec and serialized identity must not drift |
| [model_audit](../../internal/generate/emit_model_audit.go) | Disclosure policies, capture from stored changes, typed historical fields and automatic audit observer | Column-keyed audit adapters, Stages 5/7 | **Persisted audit contract:** field names/codecs, omitted/redacted/null values and identity remain; no direct TS/OpenAPI emission |
| [model_extensions](../../internal/generate/emit_model_extensions.go) | Slot policy/set types, persisted owner, runtime binding, eager descriptors, registration, cleanup and package `FoundryExtensions` | Explicit slot policies/declarations and integration adapters, Stage 7 | **Contract contribution:** `metadataContract` supplies scalar/DTO/dynamic JSON descriptors to metadata slots. Preserve those schemas, declaration inspection and stored owner/field names |
| [projection](../../internal/generate/emit_projection.go) | Row field sets, decoder definitions, selection structs and ordinary/correlated/transaction builders | `rows.Define`, mappings and scoped rows, Stage 6c | **Direct contract contribution:** `projection dto=true` also calls `emitDTO`. Convert to plain DTO declarations; preserve JSON, validation and downstream TS/OpenAPI/manifest output |
| [query](../../internal/generate/emit_query.go) | HTTP query-string and URL-encoded form descriptors, codecs and validation | Keep | Transport descriptors feed public contracts; this is not SQL query generation |
| [path](../../internal/generate/emit_path.go) | HTTP route pattern, path field codecs and descriptor | Keep | Preserve endpoint/path schema and URL construction |
| [union](../../internal/generate/emit_union.go) | Tagged JSON union, variant constructors/visitors, marshal/unmarshal and JSON contract | Keep | Preserve tagged DTO schemas; unrelated to SQL UNION |
| [multipart](../../internal/generate/emit_multipart.go) | Multipart field/file descriptors and validation | Keep | Preserve request part contracts |
| DTO/generic DTO/validation, enum, config and [json_paths](../../internal/generate/emit_json_paths.go) | Reusable wire contracts, validation, descriptors and typed JSON paths | Keep shared owners; adapt model callers | TS/OpenAPI/manifest generators remain; Stage 7 requires byte-identical contract exports |

The important correction to PLAN §4.9 is scope: `mutation_input` participates in
conflict assignment and stored-value predicates, and `model_extensions` supplies
metadata JSON descriptors. Retiring model emitters must retain those behaviors.
Projection retirement must split out its DTO work before deleting its SQL helpers.

## Release packaging

The [source policy](../../tools/release/source.go) includes Go source by file type,
so `database/orm`, `internal/ormcore`, `internal/modelmeta` and every
`internal/querycore` Go file need no directory allowlist change. Nested modules
remain excluded from the framework ZIP. The release preparer now includes the
entire Go-way fixture as `consumers/goway`, with candidate requirements and no
workspace replacement, alongside the existing consumer profiles (manifest format
3). Formats 1 and 2 remain readable by the measurement tool.

The mapping TSV has one exact-path allowance; arbitrary TSV files remain rejected.
A dry packaging run also exposed previously excluded full-consumer fixtures:
`mailing/testdata/mailers.toml` and five embedded SPA assets. Those six paths are
now explicitly allowed; arbitrary TOML, JavaScript, SVG and text files stay excluded.
[Release tests](../../tools/release/release_test.go) inspect the real runtime
inventory and separate fixture inventory, check ownership manifests, and exercise
candidate rewriting and ZIP exclusion. The release measurement tool validates the
new consumer's file hashes and downloads its dependencies; its established performance profiles stay
ordinary/configured/full. Go-way performance uses the separate harness below.

## Synthetic whole-app harness

[goway_synthetic.py](../../tools/goway_synthetic.py) writes equivalent `old/` and
`new/` modules outside this repository, using this checkout through `replace`.
The default seed is 1, with 70 models, 40 handlers and 5 joined reports. Each
handler uses 2–5 models for filtered/ordered/limited reads, typed lookup, count,
update and eager loading. Models include typed IDs, text, numbers, enum, nullable
fields, timestamp/soft-delete subsets and bidirectional FK chains. Both styles
retain enum generation; only old models/projections use model generation.
A composition package imports every handler and report.

```sh
GOCACHE="$PWD/.cache/codex-gocache" python3 tools/goway_synthetic.py generate /private/tmp/goway-app --models 70 --seed 1
python3 tools/goway_synthetic.py measure /private/tmp/goway-app --cache "$PWD/.cache/goway-cold-run1"
```

Both target and measurement cache must be fresh. Generation runs the repository's
`go run ./cmd/foundry generate --recursive --dir …` path, retaining ownership
manifests. The root module owns the Go requirement; the existing Go-way fixture
supplies dependency pins. `--models 5` selects the small compile-check workload;
`--handlers` and `--reports` allow smaller tool tests.

Measurement is native macOS, sequential old then new, with separate cold Go build
caches, `-p=2` and `GOMAXPROCS=4`. Dependency downloads are shared and generation
is outside the timer. A `-toolexec` wrapper records each compiler's `/usr/bin/time`
peak RSS. `go list -export` plus `go tool nm` records archive bytes and defined
T/t symbols per application package. The report separates models, handlers,
reports and total application archives; it also records cold wall time, cache
bytes, toolchain, machine/load information and source fingerprints.

Outputs are `measurement/measurement.json`, `measurement/summary.md` and retained
command/resource logs. Child output and elapsed time are bounded. Failed or
partial runs cannot claim `complete`; existing caches are never deleted to force
a cold run. Compiler RSS is per process, not concurrent total RSS. Archive symbols
include compiler wrappers; archive totals are not linked binary size. One run per
style is evidence for review, not a statistical performance claim. The lead runs
the full 70-model measurement.

## Stage 2b check record

[Recorded evidence](../../docs/evidence/goway-stage2b.json) covers the scoped docs,
release-tool and Python checks, real candidate inventory, and vet of both N=5
modules (40 handlers and 5 reports each). The 70-model measurement and full
framework/database gates remain with the lead. This increment does not mark
Stage 2 or later parity stages complete.

## Stage 3.1b writes and sensitive fields

The lead verified the preceding acceptance set and both PostgreSQL suites.
Security-review fixes below await the lead’s selective verification. See [the evidence](../../docs/evidence/goway-stage31b-writes-sensitive-fields.json). `internal/querycore` owns sealing, key discovery, transaction-keyring
lookup and open scope for both APIs. `encrypted.Binding` remains the sole owner of
purpose/table/column/row-key AAD. Existing generated API behavior is unchanged.

Password constructors retain scalar hash comparisons, sorting and assignment,
including nullable operations, with no text patterns or plaintext setters.
Encrypted handles support typed assignment; nullable forms additionally support
null assignment and null predicates. All set-based encrypted assignments are
rejected before SQL, even with a primary-key predicate. Single-model and batch
inserts seal each row after its application key is known, using the transaction's
keyring; reads publish no partial models on decryption failure.

Sensitive fixtures live under `models/sensitive` and `oldmodels/sensitive` so the
original fixed-shape probes remain comparable. Dedicated probes cover both
handle kinds with text/JSON values and required/nullable password constructors.
The sensitive golden checks compare SQL, binding shapes and decrypted values in
both API directions. PostgreSQL sources also cover nullable values, wrong-row and
wrong-column ciphertext, keyed updates and password round trips. Tests cover
RETURNING failure, unknown commit, committed callback failure, savepoint rollback,
batch defaults and returned order. Twelve catalogue rows are ported.

All 19 existing probes retain their Stage 3.1a function counts and unchanged
ceilings. New Go 1.27.1 darwin/arm64 archive T/t counts and ceilings:

| Probe | Functions | Ceiling |
| --- | ---: | ---: |
| column_encrypted_text | 1127 | 1297 |
| column_nullable_encrypted_text | 1359 | 1563 |
| column_encrypted_json | 1217 | 1400 |
| column_nullable_encrypted_json | 1449 | 1667 |
| column_password | 1190 | 1369 |
| column_nullable_password | 1417 | 1630 |
| sensitive_models | 2606 | 2997 |

Security-review follow-up closes explicit-codec declaration bypasses for encrypted
Go types, including nullable values and row projections. The shared declaration
check inspects the `encrypted.Value` method contract without calling user code.
Hydration caches the primary column at declaration seal and binds the key lazily
once per row. Footprint ceilings remain unchanged; the table above records the
preceding measurement, not a new measurement of this revision.

Added regression sources cover unsealed plaintext and key-material redaction,
read/write publication failures, and old/new streaming parity. `Each` keeps rows
already yielded before a later open failure; buffered reads and failing chunks
publish nothing. PostgreSQL coverage now inspects stored envelopes, persists
new-API `SetNull`, and checks `fault.Invalid` for wrong row, column and table.

## Stage 3.1c reads

`orm.CursorPaginate(ctx, ex, q, query.CursorRequest[M])` returns the existing
`query.CursorPage[M]`, directly usable with `pagination.MapCursorPage` and the
existing HTTP `CursorRequest.Page`. Wire types, manifests, OpenAPI and TypeScript
remain owned by their existing descriptors. No Query/LockedQuery/Table methods
are added. `ChunkByKey` and `EachByKey` take a same-model key column and preserve
the old primary-key ordering, bounded-window and closed-row callback contracts.

Token parsing, encoding, scope hashing and boundary decoding now share
`internal/querycore`; old query entry points delegate. The old v1 tokens are
unsigned positions (not credentials). A client may move its own position; query
predicates (tenant, soft-delete and scopes) always still apply. Scope includes
concrete Go model identity, SQL and bindings. Interoperability uses both APIs over the same Go model, with exact token
byte comparisons. Distinct Go models remain distinct token scopes.

Eighteen read compiler cases are ported. Golden tests cover nullable/mixed cursor
orders, forward/backward navigation, key-chunk SQL and locked keyed reads.
PostgreSQL sources traverse ties and NULLs both ways, cover malformed-token and
foreign-scope rejection, preserve tenant and soft-delete predicates after client
position edits, delete rows during key chunks, and perform a transaction-only
keyed lock.
Footprint ceilings are retained; dedicated cursor and key-chunk probes are added.

| Old API | New API | Parity |
| --- | --- | --- |
| `q.CursorPaginate(ctx, ex, request)` | `orm.CursorPaginate(ctx, ex, q, request)` | Shared token format/scope and HTTP adapters |
| `q.ChunkByID(ctx, ex, size, visit)` | `orm.ChunkByKey(ctx, ex, q, table.Key(), size, visit)` | Primary-key seeks, stable progress before callbacks |
| `q.EachByID(ctx, ex, size, visit)` | `orm.EachByKey(ctx, ex, q, table.Key(), size, visit)` | Individual rows from closed batches |
| `q.ForUpdate().Find(ctx, tx, id)` | `q.Where(table.Key().Eq(id)).ForUpdate().First(ctx, tx)` | Typed key and transaction retained |
| Required locked keyed lookup | `q.Where(table.Key().Eq(id)).ForUpdate().Require(ctx, tx)` | Same transaction SQL |

Acceptance: Go build/vet, scoped races (segmented fixes), generation, fixture tests,
catalogue and pagination contract checks pass. All 28 probes remain within their
ceilings; two-model growth is 820 functions against 939. The surface snapshot adds
only `CursorPaginate`, `ChunkByKey` and `EachByKey` function signatures.

| New probe | Functions | Ceiling | Archive bytes |
| --- | ---: | ---: | ---: |
| cursor | 1127 | 1297 | 4968190 |
| key_chunk | 812 | 934 | 3792620 |

Ten manifest/OpenAPI/TypeScript export files are byte-identical against the HEAD
cursor implementation. Strict TypeScript/negative compilation and React/Vue tests
pass. The native HTTP/WebSocket runtime test fails at the existing nanosecond
round-trip assertion (`.123456789Z` expected, `.123456Z` actual); the same failure
was reproduced with all changed Go sources overlaid from HEAD. This slice does
not alter temporal precision or that fixture. See the
[read-slice evidence](../../docs/evidence/goway-stage31c-reads.json).

## Stage 3.1d typed raw hydration

`Table[M,K].Raw` returns `[]M`; `rows.Raw[R]` takes an explicit `rows.Schema[R]`
and returns `[]R`. The shared non-generic owner in `internal/ormcore/raw.go`
validates `Rows.Columns()` before iteration and reorders scanner destinations.
The result must match the full declared set, including for zero rows. It retains
the original stream's encryption context and the existing model/row decoders.

Raw applies no tenant, soft-delete or global scopes and uses the supplied executor
directly. Arguments go unchanged to the executor. SQL may write; a hydration
failure does not imply rollback. Level-1 APIs remain unchanged. Live PostgreSQL
sources cover aliased/reordered selections, transaction `UPDATE … RETURNING`,
missing columns, encrypted values and intentionally returned soft-deleted rows.

Two additional compiler cases pin model-result and row-schema ownership. No old
catalogue row changes disposition in this slice. The surface adds exactly one
Table method (value and pointer method sets) and one `rows.Raw` package function.
Existing footprint ceilings remain unchanged; the dedicated raw probe exercises
both APIs. Evidence: [Stage 3.1d](../../docs/evidence/goway-stage31d-raw.json).

| Raw probe | Functions | Ceiling (`ceil(measured × 1.15)`) | Archive bytes |
| --- | ---: | ---: | ---: |
| Model and row-schema raw hydration | 1,045 | 1,202 | 4,833,602 |

Go 1.27.1 darwin/arm64, warm task cache, archive T/t symbols (not runtime timing).
All 28 pre-existing probes and the model slope remain below unchanged ceilings;
model growth is 832 functions for two added models against 939. Requested build,
vet, race, generation, fixture, catalogue and documentation checks pass. PostgreSQL
sources compile/vet and skip without configuration; the lead executes them.
Stage 3.1 is complete. The lead's Stage 3 gate on 2026-10-23 passed `make verify` (655 s),
the full PostgreSQL race suite (`make test-postgres`, about 21 minutes) and
`make typescript-check`, on the source committed with 3.1d.

## Stage 4a statement-shape detector

The injected detector composes with the existing completed-statement observer.
Each test/request owns a bounded concurrent map. Exact-text slow-query fingerprints
and observer events remain unchanged. Framework transaction/savepoint control is
excluded by execution metadata, including failed commands; raw commands still count.
No relation runtime or Query/Table methods change.

Privacy is owned by the shared compiler: inside an active detector scope it
produces readable shapes alongside SQL, with values replaced by placeholders and equivalent IN/ANY/VALUES batches
collapsed. Private context metadata binds the shape to the executed SQL. Both old
and new typed APIs carry it through reads, writes, relation batches, cursor/chunk,
locked lookups and count/pagination paths. Raw SQL is displayed only as a keyed
fingerprint, never normalized SQL text. Every violation carries bounded source
call sites. Untrusted grouping normalization is internal and has no privacy role.

Configured applications opt in through `HTTP.StatementDetector`; outside the
`development` namespace they must also set `AllowNonDevelopment`. Disabled installs
nothing. Shape/call-site overflow fails test assertions and logs once per request.
See the [database guide](../../docs/guides/database-runtime.md#detecting-repeated-statement-shapes)
and [evidence](../../docs/evidence/goway-stage4a-detector.json).

Regression sources retain the 330-case failed-execution privacy matrix and add
36 CR/LF/CRLF and nested-utility cases across Report, HTTP warnings and assertions.
External consumers exercise old/new compiled paths, opaque raw loops, external
call sites and varying relation-batch lengths. Internal tests cover keyed instance
isolation, bounded call-site retention and compiler shape/value separation. The
200-savepoint test retains unchanged observer events with no detector shapes.

The privacy redesign supersedes the earlier SQL text-display parser and its
historical measurements. Current-source acceptance passed in segments (including native reruns for
sandbox-blocked loopback tests); exact commands and logs are in the evidence. Footprint ceilings
remain unchanged. The earlier raw-hydration coverage additions are preserved.

Privacy-redesign benchmarks: Go 1.27.1, Apple M4 Max, warm task cache; six rounds
per case. A competing Go process used roughly 14 cores (initial load 34.98).
Timings are indicative under contention, not a regression/speedup claim. Both
disabled execution labels run the same nil-instrumentation code and retain
688 B/op and 14 allocs/op.

| Harness / path | Median ns/op | Range ns/op | B/op range | Allocs/op |
| --- | ---: | ---: | ---: | ---: |
| execution: disabled nil instrumentation | 630.05 | 586.6–887.7 | 688–688 | 14 |
| execution: disabled not installed | 641.2 | 590.6–781.3 | 688–688 | 14 |
| execution: enabled untrusted | 3785.5 | 3464–4988 | 6552–6552 | 36 |
| execution: enabled trusted | 2588.5 | 2466–3106 | 1448–1448 | 17 |
| consumer: compiled disabled | 7517.5 | 3739–10682 | 4648–4658 | 73 |
| consumer: compiled enabled | 10472.5 | 7830–14606 | 5664–5666 | 81 |
| consumer: raw disabled | 1731 | 1533–1899 | 1592–1595 | 29 |
| consumer: raw enabled | 6644.5 | 5634–8548 | 7976–7977 | 59 |

The consumer harness measures complete Count calls, including compilation,
transport and hydration, with a real application call site. The execution harness
isolates pool Exec, including enabled stack walking (no external test frame, so
its site is unavailable). Both assert one successful driver statement per timed
operation and exact scope counts. Neither measures HTTP/network or log output.

Disabled-path follow-up: statement initialization now gates fragment tracking on
the current execution context and clears previous tracking on compiler reuse.
Inactive model, mutation and relation-batch allocation guards compare against
compilation without tracking. The earlier consumer benchmark table above predates
this correction; paired HEAD/worktree fixture measurements are in the evidence.

The disabled-path allocation correction passed the requested race and fixture
checks. Paired benchmarks restore ChainCompile to 56 allocations/op and
Hydrate1000 to 5,065. Default-CPU medians were 2,087 → 2,466 ns for ChainCompile;
large host-contention spikes persisted in focused and single-CPU controls, so
latency parity remains unproven. All samples and conditions are retained in the
evidence; no existing footprint ceiling changed.

## Stage 4b direct eager trees and existence

Stage 4b is committed at `470d585`. Local acceptance passed, and the
second review reported no findings. Live PostgreSQL execution remains for the lead;
its isolated-schema test sources compile and vet. The old API remains unchanged.
See the [4b evidence](../../docs/evidence/goway-stage4b-relations.json) and
[eager-loading recipe](../../database/orm/README.md#direct-eager-trees-and-existence).

`HasOneOf` shares the singular loader and rejects multiple matching rows.
`BelongsTo`, `HasOne` and `HasMany` declare direct `Where`, `OrderBy` and `With`
methods that return immutable descriptors. Target scopes apply, duplicate slots
and ordering columns fail validation, and ordering appends a primary-key tie-breaker.
`LoadMissing` preserves loaded slots, including empty ones.

A query load executes one parent statement plus the sum, over all tree branches,
of `ceil(distinct eligible non-null keys / batch size)` statements. Each nested
branch batches the complete fetched target collection, never one query per parent.
`Load` and `LoadMissing` issue no parent statement. `Has` and `DoesntHave` compile
to correlated `EXISTS` and `NOT EXISTS` subqueries and add zero statements.
The same rules apply inside transactions and locked reads. `Each` and `Chunk`
apply the count contract and reset the shared budget for each window.

`orm.WithRelationLimits(q, limits)` uses the existing public limits type and shared
defaults: batches of 500, depth 8, at most 10,000 fetched rows and 10,000 attached
values across the tree. Relation `Limit` and `Offset` remain unavailable;
per-parent limits are out of scope. Budget or hydration failures publish no
partial collected parents.

Exact-count tests cover depths 1–3, shared and nullable keys, loaded-empty slots,
partial `LoadMissing`, transactions, locked reads and chunk windows. Six runs of
the 1,000-parent depth-two benchmark each executed five statements per operation.
Median time was 4,170,363 ns/op, with a 4,071,851–9,046,327 ns/op range on the shared
machine. These in-memory timings do not measure database or network latency.

| New probe | Measured functions | Ceiling, ceil(measured × 1.15) |
| --- | ---: | ---: |
| `nested_with` | 1,281 | 1,474 |
| `has` | 1,302 | 1,498 |
| `has_one` | 1,281 | 1,474 |

Existing footprint ceilings remain unchanged. Shared query-state helpers avoid
constructing target `Query[N]` values in descriptor helpers; the existing `q_with`
probe measures 1,114 functions against its unchanged 1,211 ceiling. Six old
compiler-catalogue rows are ported in 4b, with five additional rejection cases.

## Stage 4c joined relation families

Stage 4c is committed at `a89f509`. Build, vet, race,
generation, fixture, catalogue and documentation checks pass after the second-review
fixes. Live PostgreSQL execution remains for the lead. See the
[4c evidence](../../docs/evidence/goway-stage4c-joined-relations.json) and
[joined relation recipe](../../database/orm/README.md#joined-relation-families).
The old API remains unchanged.

`ThroughOf` binds a typed pivot slot; `ManyToManyOf` returns a thin
`ThroughRelation[M,N,P]` descriptor over a non-generic plan. Target and pivot
`Where`, `OrderBy` and `With` variants return immutable descriptors. Links retain
separate pivot values for each parent, including distinct duplicate pivot rows.
NULL pivot keys and missing or filtered targets create no links. A pivot row
matching multiple targets returns `database.TooManyRows`.

`HasOneThroughOf` and `HasManyThroughOf` validate both key pairs and share the
joined plan. Singular descriptors support latest, oldest and custom one-of-many
choices through `DISTINCT ON` with a primary-key tie-breaker. Morph constructors
validate the discriminator and bind one concrete slot per target. Related and
pivot soft-delete scopes apply under the shared depth and row budgets.

Each joined branch issues one statement per distinct eligible non-null key batch,
plus nested target and pivot branch batches. Nested branches batch their complete
collections after the joined batches. Morph targets batch separately for each
concrete discriminator group. One-of-many adds no statements. Empty eligible key
sets issue zero statements; `Has` and `DoesntHave` add zero extra statements.
A query adds one parent statement; `Load` and `LoadMissing` add none. Joined links
consume two attached values in the shared budget, one target and one pivot.

| Count assertion | Statements |
| --- | ---: |
| Query, two parents, batch size one, nested target and pivot branches | 5 |
| Explicit Load with shared targets and nested target and pivot branches | 3 |
| LoadMissing with loaded slots, including empty slots | 0 |
| Locked source query plus joined branch in a transaction | 2 |
| Two eligible concrete morph target groups | 2 |
| Empty parent set or ineligible/NULL morph keys | 0 |
| Joined existence query | 1, with no extra relation statements |

Six catalogue rows are ported: `pivot_key`, `pivot_model`, `pivot_order`,
`pivot_payload`, `pivot_scope` and `soft_pivot_scope`. Six additional negatives
cover wrong pivot models, target/pivot predicate ownership, morph targets and both
hop key ownership. Six nested joined `Has` levels now compile each subtree once within
the outer compiler's budget, with byte-identical SQL and bindings.

| New probe | Measured functions | Ceiling, ceil(measured × 1.15) |
| --- | ---: | ---: |
| `pivot` | 1,005 | 1,156 |
| `through` | 928 | 1,068 |
| `morph` | 1,088 | 1,252 |

Existing footprint ceilings remain unchanged. The PostgreSQL test sources cover
pivot payloads and duplicate rows, cardinality failures, through relations,
one-of-many ties, morph groups, pivot soft deletion and statement counts.
Stage 4d aggregate-slot design and Stage 5b relation writes remain separate.

## B5a typed field mutators

The accepted lane B patch is integrated with direct and joined relations. The
[mutator evidence](../../docs/evidence/goway-b5a-mutators.json) records combined
acceptance and source hashes. The [ORM recipe](../../database/orm/README.md#field-mutators-and-explicit-accessors)
shows named methods, explicit transforms and primary-key inputs.

`Mutate<Field>(Input) (Stored, error)` and `Access<Field>() (Result, error)` use
value receivers and exact concrete types. Startup checks reject orphan names,
invalid signatures and incompatible declarations. Typed adapters are bound once;
writes do not dispatch through reflection. Accessors stay explicit Go calls.

`orm.Mutate(fn)` configures same-type transformations. Required and nullable input
column kinds accept a separate mutation input while comparisons retain stored
values. They preserve omission, explicit NULL and supplied zero values. Inputs
are captured with owned binary storage; transforms run once inside the owning
transaction, before encryption and binding. Mutator diagnostics expose a fixed
field-only message while retaining the cause for `errors.Is` and `errors.As`.
Required and nullable password fields bind named mutators; encrypted fields
continue to reject mutators.

`InputKeyOf` adds a typed assignment input to the primary key already bound by
`TableOf`. Stored key lookup and predicates retain K. Distinct key inputs disable
automatic UUID creation; same-type UUID mutators retain it.

Eight catalogue rows are ported: `binary_mutation_input`, `model_accessor_nullable`,
`model_accessor_result`, `mutation_input_draft`, `mutation_input_predicate`,
`mutator_nullable`, `mutator_owner` and `mutator_value`.
`mutation_input_nullable` was deferred at B5a and is now ported in B5b: draft
getters retain `Optional[Nullable[Input]]`. `mutation_input_conflict` belongs to Stage 6e. The combined fixture has
129 compiler cases and 39 footprint probes at B5a acceptance; later slices retain
those rows and probes.

| New B5a probe | Baseline functions | Unchanged ceiling |
| --- | ---: | ---: |
| `mutators` | 801 | 922 |
| `mutator_explicit` | 1,605 | 1,846 |
| `input_column_kinds` | 5,088 | 5,852 |
| `input_key` | 982 | 1,130 |

All 39 probes pass; the 35 pre-integration footprint ceilings remain unchanged.
Combined build, vet, race, generation, fixture, catalogue, documentation and formatting
checks passed at B5a acceptance. B5b now delivers hooks and change tracking;
retrieval dispatch on Raw and scopes remain B5c, while audit remains B5d.
The PostgreSQL test sources cover distinct inputs, explicit accessors, omission,
NULL, rollback and typed key inputs; live execution remains for the lead.

## Stage 6a1 and 6a2 expression foundation

The accepted lane C patches are integrated with direct/joined relations and typed
mutators. Evidence: [6a1](../../docs/evidence/goway-6a1.json) and
[6a2](../../docs/evidence/goway-6a2.json). The
[expression reference](../../database/orm/expr/README.md) describes the concrete API.

6a1 adds row and selected value composition, arithmetic, casts and comparisons,
Count/Exists, parameters, explicit NULL/nullable widening and `rows.MapSelected`.
Reference-free parameters retain their construction scope and declaration owner;
invalid exemplars stay invalid through `Param`. References form a bounded,
stable, deduplicated set. Existing model WHERE/order composition uses package
functions without adding Query or Table methods.

6a2 adds row and selected CASE/COALESCE/NULLIF. CASE requires `Else` or `ElseNull`
before selection or comparison. A required COALESCE fallback removes nullability;
NULLIF and ElseNull add one layer. Explicit nullable variants preserve one layer.
Selected CASE takes HAVING conditions; row CASE takes row predicates. Conditional
argument validation and diagnostics have one querycore owner, reused by expression
construction and compiler traversal.

SQL bridges validate seven independent pointer-backed capability families:
metadata, expression, query, observer, autocommit, outcome and collection. Issued
capabilities never enter application-implementable interfaces. Observer lookup
switches on concrete DB, primary, session, transaction and enclosing-transaction
owners; unknown wrappers use their supplied transaction. Relation collection
publication and B5a codec bridges use the appropriate validated family without
losing loaded-slot behavior or mutator-option rejection. The
[capability sweep](../../docs/evidence/goway-6a12-capability-audit.tsv) records each use.

| New probe | Baseline functions | Unchanged ceiling |
| --- | ---: | ---: |
| `expr_arithmetic` | 1,162 | 1,337 |
| `expr_selected` | 1,551 | 1,784 |
| `conditional_row` | 1,681 | 1,934 |
| `conditional_selected` | 1,511 | 1,738 |

Both lanes' catalogue rows, surface entries and probes are retained. The combined
fixture has 181 compiler cases and 43 footprint probes with all 39 pre-integration
ceilings unchanged. Build, vet, race, generation, fixture, catalogue, documentation
and formatting checks pass. PostgreSQL sources cover expression and conditional behavior;
live execution remains for the lead. Summary aggregates, FILTER, GROUP BY,
query HAVING and DISTINCT are delivered in 6a3 below; raw fragments and the no-raw
guard remain 6a4.

## B5b write lifecycle and changes

The accepted B5b implementation is committed at `af1976a`. Its
[evidence](../../docs/evidence/goway-b5b-lifecycle.json) records lane acceptance,
trusted observer lookup and current source hashes. The
[write lifecycle recipe](../../database/orm/README.md#write-lifecycle) uses
`orm.WithHooks(func() orm.Hooks[Order] { ... })`; before hooks receive
`*orm.Write[Order]`.

Per-write factories run inside the owning transaction after an existing original
is uniquely locked, hydrated and closed. `Saving` precedes `Creating` or `Updating`.
Conventions follow before hooks; the effective draft validates before mutators and
encryption. After RETURNING hydration and row closure, `Created` or `Updated`
precedes `Saved`. Deletion runs `Deleting` then `Deleted`; force deletion adds
`ForceDeleting` first and `ForceDeleted` last. Restore runs `Restoring` then
`Restored`. At each stage local callbacks run first, then observers in declaration
order. The same stages run for observer-only models without a local hook factory.

`NewObserver`, `NewDeletionObserver` and `RegisterObserver` use the existing
configured application/database registration path and instance-owned observer
sets. Discovery delegates to the generated API's trusted concrete-owner lookup
through a validated observer-family bridge. Unknown wrappers receive no capability;
per-model writes resolve observers from the actual supplied transaction.

`Changes[M]` compares only persisted fields, using locked originals and actual
RETURNING values, including database defaults, triggers and managed timestamps.
Typed `ChangeOf` variants reuse `lifecycle.FieldChange`; `Assigned` and `Changed`
remain distinct. Snapshots own binary storage and omit ignored fields and relation
slots. `WriteGet` retains the mutation input type; `WriteNullableGet` retains
`Optional[Nullable[Input]]`, including explicit NULL. Hook and codec comparison
errors share safe diagnostics while preserving `errors.Is` and `errors.As`.

Set writes skip per-model hooks, observers and change capture. They require
`WithoutModelHooks(q)` when hooks or observers apply, and for unknown executor
wrappers. Per-model writes reject that acknowledgement. InsertMany retains its
existing bulk semantics. Table ForceDelete retains default active visibility;
`orm.ForceDelete(ctx, writer, q, table.Key(), id)` retains q's visibility and other
predicates, including `WithTrashed` and `OnlyTrashed`.

After-commit callbacks merge through successful savepoints, disappear on rollback,
and run after the outer commit releases its connection. Callback failure preserves
the committed outcome and reconciliation candidate. These callbacks are
process-local; durable outbox/audit effects use the transaction supplied to after
hooks. Retrieval/scopes remain B5c, audit policy/capture remains B5d, and relation
writes and extension slots remain outside this slice.

B5b ports 21 catalogue rows, including `mutation_input_nullable`, and adds two
extra compiler negatives. The combined fixture has 203 compiler cases and 45
footprint probes. Documentation, release inventory and catalogue checks pass;
implementation acceptance is recorded in lane B's evidence. Live PostgreSQL
execution remains for the lead.

## Stage 6a3 aggregates and grouped selection

The accepted lane C patch is integrated with direct/joined relations and B5b's
write lifecycle. The [6a3 evidence](../../docs/evidence/goway-6a3.json) records
acceptance and source hashes; the [expression reference](../../database/orm/expr/README.md)
describes the API. This completes aggregate/grouping work within 6a, while raw
fragments and the no-raw guard remain 6a4.

`Sum`/`Avg` cast exact numbers to numeric and return nullable `decimal.Decimal`.
Float and interval variants return nullable `float64` and `temporal.Interval`.
`Min`/`Max` preserve the present ordered field type and add one nullable layer.
All summaries return NULL on empty or all-NULL input. Count includes rows;
CountField/CountDistinctField skip NULLs and return required int64, including zero.
The sealed ordered constraint rejects enum wrappers that merely add `Gt`. Its
private marker is shared by ordinary and input number/text/time/interval kinds,
including nullable forms. Aggregates use stored values and never invoke mutators.
InputEnum/InputJSON/InputColumn/InputBytes remain excluded. R1 counts exported
methods (limit 25); private signatures still undergo reachability checks. Each field family has an alias-aware `At`
constructor that preserves ownership and inner-source scopes.

Aggregate `Filter` immutably appends row predicates for that aggregate. GroupBy,
Having and selected OrderBy are rows package functions. Row grouping keys and
selected keys/orders have distinct phase types, so explicit conversions cannot
bypass the shared compiler's grouping and nesting checks. Nullable aggregate
`Param(V)` returns a required row expression for a COALESCE fallback; it retains
the result codec, owner and existing errors.

`rows.FromModel` supplies complete models through the read-only rows boundary,
retaining scopes and hydration, including encrypted values. Eager loading must
be collected explicitly. Distinct, DistinctOn and DistinctOnValues preserve result
types and replace previous DISTINCT clauses. DISTINCT ON ordering starts with
the keys, then precedence and a unique tie-breaker. There are no write methods
on this boundary and B5b's lifecycle dispatch is unchanged.

`AggregateExpression[S,V]` carries native aggregate metadata, ownership and the
typed codec/scanner through a validated expression-family bridge. Stage 4d can
reuse this descriptor for relation aggregate slots; no summary methods were added
to columns.

6a3 ports 34 catalogue rows, completing all 70 original Stage 6a rows, and adds
seven extra compiler negatives. The combined fixture retains all B5b/relation
coverage: 244 compiler cases and 48 footprint probes, with all 45 existing ceilings
unchanged. Build, vet, race, fixture, catalogue, release-tools, documentation and
formatting checks pass. The surface test matches the merged snapshot.

| New probe | Integrated functions | Unchanged ceiling |
| --- | ---: | ---: |
| `aggregate_group` | 2,521 | 2,790 |
| `aggregate_families` | 4,335 | 4,369 |
| `distinct` | 1,173 | 1,306 |

PostgreSQL sources cover grouped exact/float/interval summaries, FILTER, nullable
fallbacks, selected ordering, DISTINCT and empty inputs. Live execution remains
for the lead. Current integration command results and probe measurements are in
the evidence.


## Stage 6b1 join kinds and nullable scopes

The accepted lane A patch is integrated with B5b and 6a3. Its
[evidence](../../docs/evidence/goway-6b1.json) records source hashes and combined
acceptance; the [rows recipe](../../database/orm/rows/README.md) shows the API.
`ormcore.JoinSources` owns reference validation, owner snapshots and deferred checks
around the existing querycore join compiler. Required and nullable model fields
retain codecs, model ownership and exactly one nullable layer.

LeftJoin, RightJoin, FullJoin and CrossJoin join the existing InnerJoin. Distinct
join-kind and side types prevent conversions that erase nullability.
NullableLeftScope/NullableRightScope widen missing outer rows; LeftNullableScope
preserves a nullable side through another join. AtNullable lifts a required column;
AtNullableValue retains the nullability of an already nullable column.

The lead approved broader LeftScope/RightScope signatures using sealed
preserved-side interfaces. Inferred calls remain valid across inner/cross joins
and preserved outer sides. Explicit generic arguments change from `[L,R,M]` to
`[S,L,M]` and `[S,R,M]`, respectively; callers can use inference or update them.
No Query or Table methods change.

6b1 ports 15 of the 100 Stage 6b catalogue rows and adds four additional negatives.
Six old/new SQL and binding goldens cover all join kinds and a nullable chain,
filtered/windowed inputs and nullable results. Unit tests cover NULL hydration,
wrong declarations/aliases, nil inputs and rejected zero capabilities. PostgreSQL
sources cover unmatched outer rows, multiplicity, soft-delete scopes and nullable
chains; live execution remains for the lead.

Combined acceptance passes: build, vet, races, fixture vet/tests, catalogue,
release-tools, docs and formatting. The fixture contains 271 compiler cases and
50 probes, retaining all prior cases and ceilings. Stored/input aggregate tests
cover 64 unaliased/aliased SQL and hydration comparisons, plus eight input-kind
negatives; external forged markers remain rejected.

The `outer_join` and `nullable_join_chain` probes measure 1,738 and 1,765 functions
against their original ceilings of 1,919 and 1,950. Aggregate families measure
4,351 against the retained 4,369 ceiling. All prior probe ceilings remain unchanged. ON expressions, subqueries, correlation
and LATERAL are now delivered in 6b2/6b3 below. Transaction-only adapters remain
dependent on 6c; Stage 6b is not complete.


## Stage 4d typed relation aggregate slots

The accepted [4d evidence](../../docs/evidence/goway-stage4d.json) completes Stage 4.
`ValueOf` binds a non-persisted `relation.Value[V]`; `expr.Related` ties a typed
relation input to a 6a3 aggregate, and `Using` binds or replaces its computation.
`Pivot` measures concrete pivot rows. Counts, existence, exact/float/interval
summaries, extrema and FILTER retain their existing result and nullability rules.
See the [aggregate recipe](../../database/orm/README.md#typed-relation-aggregate-slots).

Each selected slot adds one grouped statement per batch of distinct eligible
non-null keys. Slots are not fused; query loads add one parent statement and
explicit loads add none. Nested target/pivot branches share row and attachment
budgets. Loaded zero/false/NULL slots survive LoadMissing; errors publish no partial
parents. No target models are hydrated to compute an aggregate. Singular and
pivot cardinality checks remain unfiltered. One-of-many, MorphTo and eager-child
aggregate inputs fail validation. Lane evidence records five statements per
operation for the 1,000-parent, two-slot benchmark; this integration does not rerun
that benchmark or live PostgreSQL.

## B5c retrieval hooks and global scopes

The accepted [B5c evidence](../../docs/evidence/goway-b5c-retrieval-scopes.json)
records separate read factories, instance-owned retrieval observers and global
scopes. Factories run once per nonempty complete batch: local factory first,
then observers in registration order; each model invokes local callbacks before
observers. Dispatch follows successful hydration and row closure through the
shared `ormcore.Collect` owner and actual returned rows' observer snapshot.
Each switches to bounded closed batches when callbacks may run. DTOs, scalars,
write hydration and unselected intermediate roles do not dispatch retrieval.
Table.Raw does dispatch retrieval, with no automatic scopes or read routing.

Lazy scope sources receive the completed typed declaration. Scope predicates are
additive, use the executing context, and fail before SQL on invalid definitions,
missing tenancy or callback errors. Source panic/Goexit failures are retained by
the shared owner; subsequent operations fail closed without retrying the source.
Scopes cover reads/writes, relation targets and pivots, aggregates, joins and
subqueries; inserts remain unfiltered. Explicit package-level bypasses retain
other predicates and soft-delete visibility. No manifests, OpenAPI, TypeScript,
datatables, relation writes or extension slots changed. Audit remains B5d.
The shared concrete-owner lookup also retains pointer primary/enclosing owners.

## Stage 6b2 and 6b3 subqueries and lateral joins

The accepted [6b2 report](../../docs/evidence/goway-6b2-report.md) and
[6b3 report](../../docs/evidence/goway-6b3-report.md) add row-phase ON comparisons,
composition and side-owned filters, independent scalar/IN/EXISTS subqueries,
explicit enclosing correlation, and model/projected cross/inner/left LATERAL.
All lower to the existing shared compiler; phase, identity and nullable boundaries
remain typed. Input/Scalar descriptors are selection sources; only framework
aliases and join chains satisfy the sealed join-source contract. Runtime source
validation rejects zero, forged or foreign metadata before SQL.

The approved 6b1 LeftScope/RightScope signature broadening remains: inferred calls
are valid; explicit arguments change from `[L,R,M]` to `[S,L,M]`/`[S,R,M]`.
Thirty more catalogue rows are ported by 6b3. `lateral_record_cte` and transaction
record/projection adapters remain 6c integration checkpoints. The
[rows recipes](../../database/orm/rows/README.md) show the shipped API.

## Integration batch 2 acceptance

The integrated build, database/internal vet, requested races, fixture vet/tests,
catalogue, generation, release-tools, docs, formatting and diff checks pass.
Current-source command results and hashes are recorded in the four slice evidence
files. The combined fixture has 352 compiler cases and 60 footprint probes. The
catalogue retains all 1,026 rows: 268 ported, 314 to port and 444 unaffected. The
five remaining Stage 4-labelled rows concern relation writes, outside the completed
read-relation scope. Existing footprint ceilings are retained. Integration moved row/selected ordering
construction into one non-generic core helper and replaced per-model retrieval-window
closures with an explicit batch record; public signatures and provenance remain
unchanged. The two combined probes that initially exceeded their limits now measure
4,362/4,369 functions (`aggregate_families`) and 1,209/1,211 (`q_with`). PostgreSQL sources
compile in the fixture gate, while live execution remains with the lead.

## Integration batch 3: raw fragments, records and audit

Accepted 6a4, 6c1 and B5d patches are integrated pending the lead’s commit.
Current integration checks and source hashes are recorded with the lane history in
[6a4 evidence](../../docs/evidence/goway-6a4.json),
[6c1 evidence](../../docs/evidence/goway-6c1.json) and
[B5d evidence](../../docs/evidence/goway-b5d-audit.json).

6a is complete. Raw fragments use the shared compiler, preserve scopes and
transaction ownership, and never become readable detector shapes. The lexer rejects
ambiguous ordinary-string backslashes (use E strings or parameters), malformed
regions and mismatched placeholders. The opt-in no-raw lint resolves direct calls,
interface/constraint contracts and called function signatures; diagnostics contain
only file:line. Reflection, unselected dependencies/build configurations and
unresolved generic results remain limitations; matching application function
signatures may also be rejected. See the [expression guide](../../database/orm/expr/README.md#raw-fragments-and-project-policy).

6c1 provides complete record extraction, aliases and preserved-scope selection,
plus explicit transaction-only composition. Derived windows, result identity,
codecs, closed-batch retrieval callbacks and inner model locks remain intact.
Scalar-only legacy row queries are rejected by record validation. CTEs/recursion,
sets, result cursors and remaining read/page/scalar/lock terminals are pending;
transaction joins remain coordinated with 6b4. See the
[records guide](../../database/orm/rows/records.md).

B5d registers `orm.Auditing(table, policy)` with the existing instance-owned audit
recorder. Automatic/Exclude/Redact policies are immutable and model-owned;
invalid, foreign, duplicate or key-hiding rules fail before capture. Updates,
soft deletes and restores capture touched stored fields; creates/deletes capture
complete snapshots. Audit and business writes share rollback. Historical readers
retain omitted fields, absent models, SQL NULL and redaction through shared stored
codecs. Set writes still require lifecycle acknowledgement and emit no per-model
audit entries. Wire contracts, relation writes and extension slots are unchanged.
See the [audit recipe](../../database/orm/README.md#audit-capture-and-history).

The integrated catalogue retains all 1,026 rows: 292 ported, 290 awaiting ports
and 444 unaffected. The fixture passes 388 compiler cases and all 64 footprint
probes with existing ceilings unchanged. Selected current T/t symbol counts:

| Probe | Functions | Existing/new ceiling |
| --- | ---: | ---: |
| Raw fragments | 1,379 | 1,531 |
| Complete records | 1,313 | 1,510 |
| Transaction records | 1,296 | 1,491 |
| Audit | 1,313 | 1,510 |
| Existing q_with | 1,211 | 1,211 |
| Existing aggregate_families | 4,362 | 4,369 |

The requested build, vet, race, fixture, generation, release-tool, catalogue,
documentation and formatting checks pass. Race acceptance is segmented: only
`internal/websockettest` required a rerun with localhost-listener permission after
the sandbox blocked its test server. The no-raw CLI reports zero diagnostics for
the typed-only fixture package and model/report packages. Live PostgreSQL remains
for the lead. Two accepted lint fixture sources under `raw.test` were omitted by
the patch's ignore rule; their accepted hashes were verified before restoring them.
A narrow ignore exception and explicit release inventory assertions retain them.

## Integration batch 4: relation writes, CTEs and upserts

Stage 5 is complete: accepted 5b relation writes join B5a–B5d. The accepted 6c2
and 6e1 slices are also integrated pending the lead’s commit. Current checks,
conflict resolutions and source hashes accompany the original lane records in
[5b evidence](../../docs/evidence/goway-stage5b-relation-writes.json),
[6c2 evidence](../../docs/evidence/goway-6c2.json) and
[6e1 evidence](../../docs/evidence/goway-6e1-conflicts-upsert.json).

Relation writes use package functions with concrete source, target and pivot
owners. Endpoints are refreshed under locks; current-link changes serialize on
the source. Scopes, filters, pivot visibility and post-hook endpoint checks remain
active. Omitted distinct mutator inputs fail before pivot hooks or mutation SQL;
explicit inputs transform once. One transaction/savepoint owns each operation,
with no per-pivot savepoints. Hooks or observers select the B5b lifecycle;
otherwise mutations are set-based. Hook-free primary-key attachment takes four
statements per nonempty batch; sync adds a current-links read and one statement
per nonempty change kind, with insertion postconditions. Internal reads skip
retrieval callbacks. Atomic row-limit excess is `fault.Invalid`; endpoint
cardinality remains `database.TooManyRows`. Errors preserve typed reconciliation
candidates, never partial success. See the
[relation-write recipe](../../database/orm/README.md#pivot-relation-writes).

6c2 retains immutable CTE definitions, dependency ordering, materialization
choices, decoder identity and transaction locks. Recursive self is non-executable,
scoped to one synchronous construction callback and checked by the shared compiler;
applications must ensure termination and use cancellation/deadlines. Record and
scalar UNION/INTERSECT/EXCEPT, including ALL, preserve operand windows and exact
value/nullability types with separate output scopes. Twenty-five Stage 6c rows
and the deferred lateral/CTE case are ported. Result cursors and remaining terminals
remain 6c3. See the [composition guide](../../database/orm/rows/composition.md).

6e1 provides immutable column/constraint conflict policies and bounded upserts.
Literal updates use declared input types and transform once; incoming updates copy
the proposed stored value. Encrypted fields cannot be conflict update targets.
Proposed insert encryption, timestamps and full RETURNING hydration remain active;
per-model hooks, observers, retrieval callbacks and automatic audit are skipped,
as in the old API. Global scopes constrain DO UPDATE; an unconditional update
suppressed by scope aborts the whole transaction. Inserts remain unfiltered.
Skipped conflicts have no returned model, and batch return order is not an input
mapping. Nine of 57 Stage 6e catalogue rows are ported; calculated targets,
calculations, source writes and remaining terminals follow the
[6e slice plan](../../docs/evidence/goway-6e-plan.md). See the
[upsert recipe](../../database/orm/README.md#conflicts-and-upserts).

No Query/Table methods, wire contracts or generated outputs are added by this
integration. Existing footprint ceilings remain fixed; live PostgreSQL execution
remains for the lead.

## Integration batch 5: result cursors, specialized expressions and outbox

Stage 6c is complete for the approved slices. Result/scalar cursors keep explicit
completed-output scopes and caller-declared unique keys; nullable navigation,
source windows and the existing cursor wire types are retained. Read/page adapters
and transaction-only scalar/projection locks add no Query/Table methods. Shared
querycore sensitivity follows both set operands, projections, CTEs (including
bounded recursive propagation), relation aggregates and computed values. Sensitive
keys fail before SQL or token creation in both APIs. The catalogue is 72/73 for
Stage 6c: `transaction_nullable_record` awaits 6b4, and streaming Each remains a
separate parity follow-up. See the [result recipe](../../database/orm/rows/results.md)
and [6c3 evidence](../../docs/evidence/goway-6c3.json).

6d1 adds typed windows, named definitions, ranking/value functions and aggregate
Over, with ROWS/GROUPS/RANGE boundaries and exclusions. 6d2 adds text, temporal,
interval and geographic functions with the old value, NULL and codec rules.
Row and selected phases stay distinct; expression and compiler owners are shared.
The lanes supply 24 window and 401 scalar old/new comparisons. Forty-eight of
60 Stage 6d catalogue rows are ported; the remaining 12 JSON rules belong to 6d3.
See the [expression guide](../../database/orm/expr/README.md),
[6d1 evidence](../../docs/evidence/goway-6d1.json) and
[6d2 evidence](../../docs/evidence/goway-6d2.json).

6e2 adds computed and partial index targets with fixed schema predicates, plus
bound stored/proposed calculations and independent/correlated scalar subqueries.
ConflictRows is an expression scope, not an executable SELECT. Upserts retain
6e1 scopes and set-based lifecycle limits: no per-model hooks, observers,
retrieval callbacks or automatic audit. Sixteen more conflict rows are ported
(25/57); source writes and remaining lifecycle/lookup/prune/explain and D12 parity
remain 6e3–6e4. See the [6e plan](../../docs/evidence/goway-6e-plan.md) and
[6e2 evidence](../../docs/evidence/goway-6e2-advanced-conflicts.json).

Stage 7 slice 1 migrates one of thirteen framework stores: internal/outboxstore,
including event/job enqueue and shared publication. Public APIs, tables, codecs,
DB defaults, ordered FOR UPDATE SKIP LOCKED claims, savepoints, retry deadlines
and unknown-commit outcomes remain unchanged. Bounded pruning does not hydrate
payloads. Ten frozen base transcripts compare SQL, bindings and transaction counts;
nine contract artifacts were byte-identical in lane acceptance. The generator
removed its obsolete model output and retained the empty ownership manifest;
outboxstore is removed from GENERATION_DIRS. See the [outbox guide](../../docs/guides/outbox.md)
and [7s1 evidence](../../docs/evidence/goway-stage7-slice1.json).

The remaining stores and Stage 7 adapters stay open. MFA needs
predicate-preserving keyed writes; its earlier readiness assessment was provisional.
Stage 8 retirement and live PostgreSQL acceptance remain separate. Current batch
checks and source hashes are recorded in each slice's integration evidence; all
existing footprint ceilings remain fixed.

## Integration batch 6a: source writes, JSON paths and store capabilities

6e3 adds typed insert-select and source update/delete package builders. Source
scopes, filters and windows remain; destination scopes constrain updates/deletes.
Mappings, match keys and plans have distinct private phases. Fixed inputs normalize
once; mapped writes cannot bypass mutators, encryption or managed timestamps.
Mapped updates reject ambiguous matches atomically. Exec returns a count; bounded
Returning hydrates complete models and rolls back on overflow, decode or close
failure. These set writes skip local hooks, observers, retrieval and automatic
audit while retaining transactions, conventions and reconciliation. See the
[source-write recipe](../../database/orm/README.md#insert-select-and-source-writes)
and [6e3 evidence](../../docs/evidence/goway-6e3-source-writes.json).

Fourteen 6e3 catalogue rows are ported. Six declaration-role negatives remain
pending the approved dedicated typed-role slice: `insert_select_mutator`,
`insert_select_timestamp`, `source_delete_force`, `source_update_mutator`,
`source_update_primary`, `source_update_timestamp`. Runtime defenses remain;
they are not substitutes for those static exclusions. The
[role feasibility record](../../docs/evidence/goway-6e3-static-role-feasibility.json)
is historical analysis, not a new approval request. Four additional per-model
rows arrive with Stage 7 capabilities; Stage 6e is now 43/57 with eight other
6e4 rows still pending.

Stage 6d is complete: all 60 catalogue rows are ported. JSON paths use
startup-validated pointer declarations and exact payload, map-key and index types.
The shared JSON/tag/compiler owners preserve promotion, codecs and parameter
binding. Missing paths and SQL-null ancestors produce SQL NULL; present JSON null
remains present for JSON/Kind, while scalar extraction yields SQL NULL. Length is
NULL for non-arrays. Optional, nullable, pointer and nested-document wrappers are
explicit. No public contract or generated DTO schema changes. See the
[JSON recipe](../../database/orm/expr/README.md) and
[6d3 evidence](../../docs/evidence/goway-6d3.json).

Stage 7 slice 2 migrates seven packages and eleven models: countries, jobarchive,
idempotency, MFA, challenge, session and token stores. With outbox, eight of
thirteen store packages now use ORM declarations. Scoped keyed `orm.Update` and
`orm.Delete` retain both selection and identity predicates and the ordinary
lifecycle. Existing schema, defaults, codecs, redaction, freshness, lock ordering
and unknown outcomes remain; session/token maintenance SQL is unchanged. The
lane captured 23 frozen transcripts and nine identical contract exports. The
generator removed twelve obsolete model/projection outputs while retaining
ownership manifests and countries enum output. See the
[scoped-write recipe](../../database/orm/README.md#scoped-keyed-writes) and
[7s2 evidence](../../docs/evidence/goway-stage7-slice2.json).

Stage 7 capabilities add typed PostgreSQL UUID seeks and bounded UpdateEach/
DeleteEach. Candidate sets lock completely in primary-key order; overflow and
duplicate identities fail before callbacks. Rows close before ordinary lifecycle
work. One transaction/savepoint covers all candidates, without per-row savepoints;
unknown or committed failures retain typed candidates and never report successful
rows or retry. Incoming locks, ordering/windows, eager clauses and hook bypasses
are rejected. See the [capability recipe](../../database/orm/README.md#uuid-seeks-and-bounded-lifecycle-writes)
and [capability evidence](../../docs/evidence/goway-stage7-store-capabilities.json).

These prerequisites unblock later work but do not migrate notifications,
webhooks, extensions, attachments or audit. Owner/slot, datatable, binding,
factory/identity and pruning adapters remain open. Stage 7 and Stage 8 retirement
are incomplete. Current integration checks and refreshed hashes accompany each
slice's evidence; existing footprint ceilings remain unchanged. Live PostgreSQL
execution remains a separate lead checkpoint.

## Integration batch 6b: transaction phases, declaration roles and adapters

Stage 6b is COMPLETE: transaction-only joins, scalar/IN/EXISTS subqueries,
correlation and cross/inner/left LATERAL retain scopes, nullability and inner lock
boundaries; see the [transaction composition recipe](../../database/orm/rows/transactions.md).
Concrete and generic conversions are checked across ORM, rows, expressions and
the old API, including relation cardinality/computed slots, row/selected phases,
aggregate capabilities, nullability, lookup kinds and source mutation mappings. The allowlist drops all 40 temporary Input-role
exceptions (111 to 71); phase and capability separation stays compiler-enforced.

Typed roles close the six deferred source-write cases. Managed timestamp pairs
use CreatedAt/UpdatedAt and their Of constructors; lone unmanaged timestamps keep
TimeOf. Mutated fields use Input carriers even for equal input/stored types.
PrimaryOf is needed only for insert-select primary mapping. NumberKey/TextKey
retain primary versus lookup roles; erasing to KeyColumn grants no write role.
MapInsert/MapUpdate exclude inputs and UpdatedAt, retain CreatedAt where the old
validator allows it, and updates reject primary/lookup targets. ForceDelete,
ForceDeleteUsing and ForceDetach require the exact declaration's SoftDeletable
value from SoftDeletes. Runtime ownership checks remain active.

Slice 7s3a supplies owner/slot adapters, thin translation/attachment/metadata
handles and extension loading through the existing With/Load/LoadMissing budget.
Scoped and nested binding preserves parent predicates and duplicate detection;
lookup diagnostics redact even private/nested wrappers. Factory batches use
InsertEach in one transaction/savepoint with ordinary lifecycle and typed unknown
outcomes. Pagination cursor aliases retain existing reflected wire identities.
The new extension models have no managed timestamps, mutators or soft deletes;
factory/binding fixtures reuse the role-migrated hook declarations and soft token.
No stored schema or generated contract changes are intended. Five store migrations,
datatables and general generator retirement remain separate work.

Current integration results and refreshed source hashes are recorded in
[6b4 evidence](../../docs/evidence/goway-6b4.json),
[typed roles evidence](../../docs/evidence/goway-declaration-roles.json) and
[7s3a evidence](../../docs/evidence/goway-stage7-slice3a.json).
Live PostgreSQL and full editor/client gates remain separate lead checkpoints.

## Integration batch 6c part 1: terminal writes, datatables and shared owners

Stage 6e is COMPLETE (57/57 catalogue rows), and all approved Stage 6 slices are
done. The Stage 6 whole-app measurement gate is still pending; this integration
acceptance does not replace it.

CreateEach runs bounded ordinary creation lifecycles atomically. FirstOrCreate
and UpdateOrCreate lock the first primary-ordered match, default omitted stored
inputs from unambiguous equalities/scopes, skip retrieval hooks and verify created
membership. Mutators need explicit inputs. CreateOrFirst inserts in a child
savepoint; only its own insert's unique violation, complete schema/table/index
identity and confirmed rollback allow competitor lookup. Hook/observer/codec
failures and uncertain outcomes never trigger recovery or retry; snapshot isolation
can hide a competitor. B35/B36 provenance checks are shared with the old API.
AfterCommit wraps errors before isolation, preserving committed outcome and cause
identity without calling application formatting.

Ordinary numeric adjustments retain positive finite magnitudes and bound guards;
primary/lookup numeric handles no longer expose Increment/Decrement. PruneBatch
preserves scopes/visibility and locks bounded candidates; ForcePruneBatch requires
the exact SoftDeletable token. Lifecycle mode runs per-model callbacks, while Mass
explicitly skips hooks/observers/audit. Earlier committed batches remain reported,
but failed or unknown batches never become confirmed removals. Explain plans;
ExplainAnalyze executes. Locked/transaction variants require a transaction. Normal
formatting is redacted; explicit plan JSON/Root snapshots can contain server predicates.

Stage 8a lane B moves shared page/chunk/insert/per-model limits to querycore and
validated concrete observer lookup to database. Public constants remain aliases;
query.FoundryObservers and prune.Model remain compatibility adapters. The ORM prune
registry is documented in [database/prune](../../database/prune/README.md).
Extension backing and page/cursor extraction are delivered in part 2 below; general generator retirement remains separate.

Datatable slices 1–3 cut the runtime over to rows.Query and expression predicates,
selected ordering and projection keys. Shared projection reads preserve ordering,
page bounds and streaming exports; enum/text and relation adapters validate actual
metadata and owners. Authorization, tenant/soft-delete filters, export bounds and
repeatable-read pagination remain intact. Reporting uses ordinary foundry:dto
annotations plus handwritten SQL schemas. The frozen pre-cutover contract fixture
compares artifact names and bytes, including manifest/OpenAPI/TypeScript; editor
and client probes cover the migrated consumers. See the
[datatable migration guide](../../docs/guides/datatable.md).

Integration source hashes and command results are recorded in
[6e4](../../docs/evidence/goway-6e4.json),
[8a lifecycle](../../docs/evidence/goway-8a-lifecycle.json), and datatable
[slice 1](../../docs/evidence/goway-stage7-datatables1.json),
[slice 2](../../docs/evidence/goway-stage7-datatables2.json),
[slice 3](../../docs/evidence/goway-stage7-datatables3.json) evidence.

## Integration batch 6c part 2: stores, validation and shared contracts

Slice 7s3b completes all 13 framework-store migrations. Webhook, notification,
attachment, extension and audit stores retain persisted names, key codecs,
scopes, lock order and reconciliation. Fifty-nine frozen old-store transcripts
pin SQL, bindings, hydration and statement/transaction counts. Managed timestamps
use typed roles; streaming consumers share rows.Each in rows/read.go.

Advisory validation/database rules now accept ORM queries and typed stored
columns through ValueLookup. Scoped predicates, nullability, mutator stored types,
early exit and rule metadata remain intact. Batches contain at most 64 equality
observations; empty inputs issue no SQL. No model hydration or retrieval hooks
run. Database constraints remain authoritative. Five validation rows are ported.

Stage 8a lanes B, C and D are integrated. Lane B owns common limits and observer
lookup; lane C moves page/cursor contracts and validation to querycore, retaining
legacy aliases, unsigned v1 token bytes and the published HTTP cursor scalar ID.
Integration also moves bounded Explain execution and plan decoding to querycore,
so part 1’s rows.Explain respects lane C’s legacy-import guard. Public Plan and
PlanNode names remain aliases; their defining package becomes querycore.
Lane D moves modern extension loading and identity backing off legacy descriptors.
The internal/extensionslot owner preserves executor affinity, loaded-empty slots,
fetched/attached budgets, shallow snapshots and deferred attachment on failure.
Capabilities reach only framework-owned state, never application callbacks.
Legacy entry points remain compatible; model generation is not retired here.

B34 editor anchors cover the migrated declarations and advisory rules. Current
acceptance and source hashes are retained in [store evidence](../../docs/evidence/goway-stage7-slice3b.json),
[validation evidence](../../docs/evidence/goway-stage7-validation-database.json),
[editor evidence](../../docs/evidence/goway-b34-editor-stage8-inventory.json),
[lane C evidence](../../docs/evidence/goway-stage8a-lane-c.json) and
[lane D evidence](../../docs/evidence/goway-stage8a-lane-d.json).
The Stage 6 whole-app measurement gate remains pending.

## Integration batch 7a: Stage 7 completion and consumer wave 1

Stage 7’s approved slices are complete. application.WithTables snapshots explicit
CheckedTable lists and calls the shared orm.Check before providers or handlers
are constructed. Each declaration is isolated separately, preserving other errors
when a declaration panics or exits. Models.Tables returns an owned interface slice;
bootstrap does not retain each model’s generic table namespace. ORM seeders use
the supplied transaction and ordinary writes. Pagination consumers retain the
existing aliases, response shape, token bytes and authorization boundaries.

Binding, decode, SQL and JSON-key diagnostics are redacted at their shared owners.
Diagnostic causes remain inspectable with errors.Is/As; retained JSON causes do
not independently classify an HTTP response. Explicit outer errors and joined
siblings retain precedence, and direct JSON-key validation is unchanged. This
integration does not claim the separately deferred H1 hardening inventory is closed.

D8 changes the existing model scaffold to emit handwritten ORM table and extension
declarations. Applications explicitly compose table registration; DTO codecs still
generate. The existing flags are retained, and complete emitted symbol families
are checked for collisions, Go keywords, predeclared names and valid identifiers.
Generated model/query emitter retirement remains Stage 8c.

Stage 9 wave 1 publishes B1 linkqueries, mutatorqueries, observerqueries and
inputqueries foundations; lane C migrates reporting authority and independent
intervalqueries/transactionqueries consumers; D1 publishes profiles, articles,
passwords and multifactor foundations, with the profile service/lifecycle cutover.
Only one lifecycle adapter family is registered per model/application. Concrete
owners, stored/input types, extension subjects, identity encodings and contract
bytes are preserved. Existing generated APIs, directives, manifests and independent
old-API oracles remain until dependent callers migrate and generation authorizes
cleanup. Full observer metadata inspection, typed draft unsetting and ORM extension
matching remain recorded handoffs for later consumer slices.

Current integration results and source hashes are recorded in the
[Stage 7 remainder evidence](../../docs/evidence/goway-stage7-remainder.json),
[B1 evidence](../../docs/evidence/goway-stage9-b1.json),
[lane C wave 1 evidence](../../docs/evidence/goway-stage9-lane-c-wave1.json),
[D8 evidence](../../docs/evidence/goway-stage8-d8-scaffold.json) and
[D1 evidence](../../docs/evidence/goway-stage9-d1.json).
The Stage 6 whole-app measurement gate remains pending.


## Integration batch 7b: lifecycle and query consumer cutovers

B2/B3 advance observer, hook, timestamp, soft-delete, tenant, audit, binary,
event, distinct-input, link, mutator and encrypted-vault consumers to ORM.
`lifecycle.CountObservers[M]` returns write, retrieval and deletion-only counts
without constructing callbacks or exposing private adapters. Tests retain
registration inspection after row closure, with one adapter family per application.

`orm.WriteUnset(write, Fields.Email.Set)` restores omission in a live before-write
callback. Omission remains distinct from SQL NULL and a supplied zero. Validation
and sticky errors use the existing draft owner; mutators, defaults and conventions
retain their existing execution rules. `orm.RestoreEach` and `orm.ForceDeleteEach`
require the declaration's soft-delete token and reuse bounded per-model writes,
locked primary ordering, callbacks, savepoints and reconciliation candidates.
Restore selects deleted rows; force deletion retains the query's visibility.

C's A1 exports ten shared model tables, six relation sets, fourteen User aggregate
slots, catalog products and ten report schemas without changing model, key, enum
or DTO identities. The query fixture keeps its public setup functions and seeds
through ORM. Eleven dependent packages now use those declarations: reports,
chunkqueries, temporalqueries, windowqueries, projectionqueries, joinqueries and
its inner/natural/outer packages, correlations/nullable, and lockqueries.

Six rows package functions preserve composition: `OrderScalar`, `LimitScalar`,
`OffsetScalar`, `HavingCorrelatedRecord`, `DistinctCorrelatedRecord` and
`DistinctOnCorrelatedScalar`. They reuse the existing clause owners and retain
codecs, errors, parent scopes and selected-source boundaries. Lateral consumers
are migrated; comparison and distinct consumers have partial cutovers with
old/new SQL assertions. No Query/Table methods or capability bridges are added.

Stage 9 remains in progress. Complete-record streaming and the remaining
compile/preflight/stream and clause adapters still have assigned owners. A1 and
WriteUnset remove prerequisites for later upsert/recovery/D2 cutovers; integrating
a foundation does not migrate those callers. Retained legacy streaming and creation
adapter tests remain explicit oracles, and generated output/manifests remain until
the lead's cleanup barrier. The Stage 6 whole-app measurement gate is still pending.

The four new Go-way probes retain the lane measurements and 15% ceilings:

| Probe | Measured functions | Ceiling |
| --- | ---: | ---: |
| observer_counts | 63 | 73 |
| write_unset | 266 | 306 |
| each_soft_writes | 1,567 | 1,803 |
| source_clauses | 2,505 | 2,881 |

Existing ceilings are unchanged. Integration commands and current source hashes
are recorded in the [B2 evidence](../../docs/evidence/goway-stage9-b2.json),
[B3 evidence](../../docs/evidence/goway-stage9-b3.json),
[A1 evidence](../../docs/evidence/goway-stage9-lane-c-a1.json),
[review-26 evidence](../../docs/evidence/goway-stage9-lane-c-review26.json) and
[source-clause evidence](../../docs/evidence/goway-stage9-lane-c-source-clauses.json).
Live PostgreSQL coverage remains for the lead's service gate.

## Integration batch 7c: read families, result clauses and upserts

`rows.CompileRead`, `rows.PreflightRead` and `rows.EachRead` accept the existing
ordinary `ReadSource[S,R]` family. Scalar, record, set and value-set adapters retain
codecs, scope context, result identity and bounded iteration. Transaction-only
sources remain restricted to their transaction terminals. No Query/Table methods
or new capability families are added.

Complete models use the shared `ormcore.RecordBatches` owner in both APIs.
Retrieval callbacks run once per nonempty closed batch, with the existing batch
size and window accounting. Proven primary-key sources can advance by key; joins,
sets and recursive sources use offsets when uniqueness cannot be proven, retaining
repeated IDs. Callback-returned errors preserve identity without calling their
formatters; callback panic/Goexit still fails through the isolation owner.

`expr.RelationFilter` adapts expression predicates to a relation's target-owned
predicate through the validated metadata bridge. Eager constraints and EXISTS
share target/declaration/reference checks. Stored relation-construction errors
remain attached when a filter is moved to another query or relation. JSON source
and relation composition consumers now exercise these adapters.

Eight rows package functions complete the ordinary result-clause family:

| Result family | Package functions |
| --- | --- |
| Scalar | `GroupByScalar`, `HavingScalar`, `DistinctScalar`, `DistinctOnScalar`, `DistinctOnScalarValues` |
| Complete record | `DistinctRecord` |
| Record set | `DistinctSet` |
| Value set | `DistinctValues` |

They delegate to the existing clause owners, preserving selected codecs, nullable
values, nested input SELECTs, scopes and bounds. DISTINCT ON keeps row keys distinct
from selected keys and retains ordering/phase validation. The upsert consumer now
uses shared `models.Users`/`WriteRecords`, typed assignments and `ConflictRowsOf`
for stored/proposed calculations, with its original defaults, identity, atomicity
and index assertions. Its 24 compiler cases retain their existing Go-way mappings.

Stage 9 remains in progress. The retained A3/HTTP consumers need no ORM declaration
dependency where they only use ordinary models, typed IDs, DTOs or service contracts.
The read and scalar-clause foundations now remove blockers recorded in the lane
reports; their remaining callers still need explicit migration. In particular,
retained complete-record streaming and direct scalar-compile oracles, the remaining
JSONKind projection boundary, A2/HTTP model callers and generated cleanup are not
claimed complete by this integration. Stage 6 whole-app measurement is still pending.

Catalogue totals remain 1,032 rows: 570 ported, 18 pending and 444 unaffected. The
Go-way fixture has 832 compiler cases and 139 probes. All 134 previous probe ceilings
are retained. Five new probe baselines and their 15% ceilings are:

| Probe | Measured functions | Ceiling |
| --- | ---: | ---: |
| read_scalar | 1,165 | 1,340 |
| read_record | 1,002 | 1,153 |
| read_set | 1,050 | 1,208 |
| relation_filter | 1,831 | 2,106 |
| result_clauses | 2,188 | 2,517 |

These are lane measurements of T/t symbols in consumer archives; integration
acceptance checks their unchanged ceilings. Current commands and source hashes
are recorded with the [read terminals](../../docs/evidence/goway-stage9-read-terminals.json),
[relation filters](../../docs/evidence/goway-stage9-relation-filters.json),
[upserts](../../docs/evidence/goway-stage9-upsert.json) and
[result clauses](../../docs/evidence/goway-stage9-lane-c-result-clauses.json).
The [A3 inventory](../../docs/evidence/goway-stage9-a3-report.md) retains the
consumer ownership roster. Live PostgreSQL acceptance remains the lead's gate.

## Integration batch 7d: application consumers and computed keys

A2 migrates `pivothooks`, `relationshipfilters`, `productionprofile`,
`configuredprofile`, `bootstrap` and `correlations`. Application assembly validates
thin table handles through `WithTables`. Pivot writes retain their shared lifecycle
callbacks; actor references, scoped writes, nested EXISTS and nullable correlated
queries retain their concrete identities. Office declarations are available, but
ad-hoc relation aggregate selection/order/filter without model slots is still an
open framework gap. Its original acceptance assertions remain.

The D3 batch migrates recovery, authentication, nested bindings, notification and
realtime identities, with an audit of the other service-only consumers. Recovery's
Saving hook calls `orm.WriteUnset(write, fields.EmailRevision.Set)` when the email
is unchanged. This removes the assignment: it does not send NULL, zero or the old
value. The retained PostgreSQL trigger regression proves omission. Model-first
locking, stored-hash compare-and-swap, request snapshots and unknown-commit
reconciliation remain unchanged. Nested binding retains parent scope, alternate-key
uniqueness, authorization order and retrieval callbacks.

Client handlers use ORM page/simple-page/cursor aliases and explicit DTO
presentation. The existing frozen-byte checks and native TypeScript tests cover
client/reporting manifests, OpenAPI and generated client output; no transport or
persisted schema changes are intended. Generated files and ownership manifests
remain at the lead's cleanup barrier.

Four package functions complete the computed-key clause adapters:

| Function | Typed scope and behavior |
| --- | --- |
| `rows.GroupByCorrelatedScalar` | Lifted row group keys in the enclosing/inner correlation |
| `rows.DistinctOnCorrelatedScalarValues` | Selected keys in that same correlation |
| `rows.OrderCorrelatedScalar` | Selected ordering, including aggregate/window keys and the DISTINCT ON prefix |
| `rows.DistinctOnValueSet` | Selected keys in the combined value-set output scope |

These functions delegate to shared clause and ordering owners, retaining input
windows, immutable descriptors, aliases and nullable codecs. They add no public
Query/Table methods or capability families. Ten negative cases preserve owner,
phase, value-type and wrapper restrictions. The new `key_clauses` lane measurement
is 2,674 T/t functions with a 3,076 ceiling; all 139 pre-existing ceilings remain
unchanged, for 140 probes total.

The catalogue has 1,032 rows: 576 ported, 12 pending and 444 unaffected. Go-way has
848 compiler cases. Six Stage 7 rows now have concrete ORM counterparts:
`nested_bundle_handler`, `notification_payload_owner`,
`notification_recipient_owner`, `session_metadata_model`, `token_id_model` and
`websocket_disconnect_model`.

Stage 9 remains in progress. Original pagination relational-clause tests remain
in the named legacy files; the ordinary complete-record DISTINCT ON adapter and
any remaining scalar cursor clause gaps need their owners. Existing read/filter
foundations are available but retained callers are not automatically migrated.
Office ad-hoc aggregates, JSONKind projection identity, remaining consumers and
generated cleanup stay open. Historical lane reports describe their own bases;
current integration results are recorded in the [A2 evidence](../../docs/evidence/goway-stage9-a2.json),
[D3 evidence](../../docs/evidence/goway-stage9-d3.json),
[client-contract evidence](../../docs/evidence/goway-stage9-clientcontracts.json)
and [computed-key evidence](../../docs/evidence/goway-stage9-lane-c-key-clauses.json).
Live PostgreSQL remains the lead's gate.

## Integration batch 7e: annotations, extension consumers and redaction

`expr.RelatedValue` preserves aggregate nullability and composes with existing row
predicates/orderings. `rows.WithValue` returns complete models and computed values
as `rows.Annotated`, retaining retrieval callbacks and eager loading. Office,
tooling and HTTP-model consumers now use handwritten declarations.

`orm.LoadFrom` and `LoadMissingFrom` accept the query carrying `WithRelationLimits`
and `With` descriptors. They load copies of supplied parents, issue no parent SQL,
and share batching, depth and row budgets with existing loaders. Parent predicates,
ordering and windows are validated but do not filter/reorder the supplied parents;
callers retain responsibility for parent authorization.

Attachments, translations and metadata expose bounded owner-typed `Matching`;
`attachments.AddFilesTo` preserves bound-slot validation, batch limits and partial
publication outcomes. Extension consumers and the root harness use ORM declarations.
Lane B’s accepted nestedbindings implementation is retained. Editor anchors follow
handwritten declarations; generated artifacts and wire contracts remain at their
existing cleanup barrier.

Predicate, statement, assignment, numeric-bound, plan, cursor and pivot diagnostic
state uses private holders so nested formatting cannot expose retained values.
Explicit SQL/argument/value accessors retain their contracts and cursor equality.

The registry union contains 861 Go-way compiler cases and 146 footprint probes.
Existing Go-way ceilings are unchanged; the root assignment probe records its new
measurement separately from the frozen legacy baseline. Stage 9 remains in progress:
legacy root relation/through/aggregate oracles still need migration using the new
loader adapter, remaining pagination callers and generated cleanup remain open.

Current integration commands and source hashes are recorded with the
[office evidence](../../docs/evidence/goway-stage9-office.json),
[loader evidence](../../docs/evidence/goway-stage9-load-query.json), and
[root/redaction evidence](../../docs/evidence/goway-stage9-d4.json).

## Integration batch 7f: read terminals and relation loaders

Retrieval CTE streaming uses `rows.EachRead`, retaining closed-row callback windows,
limits, cancellation and callback error identity. Cursor and page consumers use
shared read/result contracts; complete-record DISTINCT ON preserves input ordering,
including unselected input columns. The unused audit-query `Domain` and `EmailAfter`
helpers are removed; generated drafts, directives and ownership manifests remain.

Derived fields use the 18 `expr` row-aggregate functions with existing ordered
capabilities and nullable codecs. `database.JSONKind` is the single owner, with
compatible aliases in `query` and `expr`. `rows.DistinctOnRecord`,
`expr.RelationOrder`/`RelationPivotOrder`, and cursor compile/preflight functions
reuse the shared compiler and validation paths. They add no Query/Table methods.
Scalar, key, JSON, frame, recursive, set and report consumers retain their result,
SQL, ordering and error assertions; explicit old/new SQL parity oracles remain.

The three root relation suites now call `orm.LoadFrom`/`LoadMissingFrom`, retaining
statement counts, nullability, cardinality, cancellation, no partial publication
and parent immutability assertions. Editor probes follow the migrated APIs and
handwritten schemas, including lifted nullable fields and typed JSON paths.

The integrated registry has 872 compiler cases and 150 footprint probes. All 146
inherited ceilings are unchanged. The four added ceilings are 3,229 functions for
derived aggregates, 2,383 for record DISTINCT ON, 4,867 for relation expression
ordering and 2,062 for cursor terminals. The catalogue retains 1,032 rows: 577
ported, 11 pending and 444 unaffected; this batch adds native negatives rather
than claiming those remaining catalogue rules are ported.

Stage 9 remains in progress. Generated cleanup, retained automatic-mutator producers
and the remaining catalogue rows require their own migration. Current command
results and source hashes are recorded in the integrated
[read evidence](../../docs/evidence/goway-stage9-b-read-completion.json),
[derived-field evidence](../../docs/evidence/goway-stage9-lane-c-derived-gaps.json),
[terminal evidence](../../docs/evidence/goway-stage9-lane-c-final-gaps.json) and
[loader evidence](../../docs/evidence/goway-stage9-d4-loaders.json).

## Integration batch 7g: catalogue completion and cleanup barrier

Stage 7 is complete. The Stage 9 catalogue now covers all 1,032 cases: 587 ported
rows map to 579 distinct Go-way negatives, and 445 rows are unaffected. No row is
pending or obsolete. Existing footprint ceilings and runtime method sets stay unchanged.

The barrier removes 63 generated Go files, independent model/projection directives
and legacy adapter factories from the accepted B/C/D consumers. The generator
confirms 27 updated ownership manifests; 882 Go-way compiler cases and all 150
unchanged footprint ceilings remain registered. Retained live SQL and
hydration oracles and shared model identities still need their coordinated retirement;
Stage 9 and the barrier are therefore in progress.

The [migration guide](../../docs/guides/orm-migration.md) has compiled examples and
[breaking-change notes](../../docs/evidence/goway-breaking-changes.md). Business
helpers return typed assignments, including explicitly supplied empty values; root
consumer assertions now call CurrentEmailAssignment and CreatorAssignment directly.

The [oracle harness](../../tests/fixtures/goway/testdata/oracles/README.md) records
paired legacy/new SQL, bindings and hydration observations with a reviewed source
census, including the later scalar/key, JSON, window, set and relation-order
comparisons added after the harness patch base. Its state is pending: this
integration does not freeze expectations from the
new implementation or claim PostgreSQL capture. Independent pre-removal snapshots
remain checked in for retired consumers. Regeneration owns generated outputs and
ownership manifests; no generated conflict is hand-merged.

See [integration evidence](../../docs/evidence/goway-batch7g-integration.json) for
conflicts, exact removals, source fingerprints and segmented acceptance results.

## Integration batch 7h: oracle prerequisites and helper retirement

CurrentEmailDraft and CreatorDraft are removed; consumers use the typed assignment
helpers and retain exact-value/presence assertions. PostgreSQL sensitive and mutator
behavior tests use ORM declarations. The dual-runtime redaction test has a
handwritten legacy declaration, independent of generated models.

The oracle import guard parses escaped import paths and requires recording files
to contain only reviewed paired entry points and their helpers. Ordinary tests and
benchmarks live separately. Direct, target, pivot and mixed relation ordering retain
their recorded SQL/bindings comparisons. The reviewed census covers 540 test sources
and 103 paired entry points. The only migration-before exceptions are
`migration/guide_test.go`, `migration/examples_test.go` and `migration/sql_test.go`
under the Go-way fixture. Stage 8c removes this exception when before snippets become
documentation-only; after snippets remain compiled.

Stage 7 and the Stage 9 catalogue remain complete; the cleanup barrier and final
PostgreSQL oracle freeze remain pending. Offline capture/replay uses temporary
storage and does not publish expectations. This integration checked 907 observations
across 134 assertion sites and 12 footprint packages. See the
[prerequisite decisions](../../docs/evidence/goway-stage8c-base-dependencies-report.md)
and [helper retirement](../../docs/evidence/goway-stage9-d-helper-report.md).

Current-source command results and hashes are recorded in
[batch 7h evidence](../../docs/evidence/goway-batch7h-integration.json).
