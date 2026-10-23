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
| R1 | Keep public generic method sets small, approximately 25 methods per type. |
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
| 4c | Typed pivots, through, one-of-many and polymorphic relations | Accepted; Done pending the lead's commit. Local acceptance passed after second-review fixes; live PostgreSQL execution remains for the lead. |
| 4 | Full read relations, pivots, through, one-of-many, filters and aggregate slots | In progress; 4a–4c delivered, 4d aggregate-slot design and implementation remain planned. Relation writes belong to Stage 5b; per-parent limits are out of scope. |
| 5 | Lifecycle, mutators, change sets, scopes and observers | Planned |
| 6a | Expressions, aggregates, grouping, having and raw fragments | Planned |
| 6b | Joins, aliases, correlation and lateral queries | Planned |
| 6c | Rows/projections, CTEs, recursive queries, sets and result cursors | Planned |
| 6d | Windows, JSON, temporal, interval, geographic and text expressions | Planned |
| 6e | Conflict/upsert, source writes, insert-select, adjustments, prune and explain | Planned |
| 7 | Framework integrations and wire-contract parity | Planned |
| 8 | Retire model generation and old query API; extend scaffold | Planned; no deletion before parity |
| 9 | Migrate consumer fixtures and compiler catalogue | Planned |
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
| D7 | Explicit application table registration is planned; `orm.Check` already validates declarations in tests. Do not rely only on first-query validation. |
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
| `defineOrderRelations() OrderRelationSet` | Separate typed relation namespace using direct, through and morph `orm.*Of` constructors | Direct kinds ported in 4b, joined families in 4c; aggregate slots remain 4d |
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
| Generated `Distinct`/set query surface | `rows.Distinct(...)`, `rows.Union(a, b)` | Planned, Stages 6a/6c |
| `query.SelectValue(QueryOrders(), ...)` | `rows.SelectValue(Orders.Query(), ...)` | Basic scalar selection ported |
| Model annotation plus `foundry generate` | Plain struct plus `orm.Define` | Core ported; DTO/enum generation retained |

## Feature parity contract

“Preserve” states the program's target, not completion of the feature. The current
consumer retains Stage 1 golden coverage and adds core, direct and joined-relation cases.

| Capability | Old owner/shape | New shape and owning stage |
| --- | --- | --- |
| IDs, nullables, numeric/temporal values, enums and bytes | Plain structs and generated codecs | Constructors/codecs retained; natural keys and binary ownership covered in 3.1a |
| JSON documents and intervals | Generated JSON/interval fields | `JSONOf`/`NullableJSONOf`: equality, membership, containment, assignment, `Asc`/`Desc` and `IsJSONNull`; no range comparisons/text patterns. `IntervalOf`/`NullableIntervalOf`: ordered comparisons, sorting and assignment. Kind/Length expressions, paths and interval functions stay 6d |
| Password and encrypted fields | Typed values and lifecycle codecs | Stage 3.1b: password scalar capabilities; encrypted typed assignments/null predicates, shared sealing/opening, transaction-keyring enforcement |
| Column/default tags | Generator reads tags | Startup metadata reads the same tags, Stage 3 |
| Table name/options | Model annotation | `orm.Define` and typed options, Stage 3 |
| Accessors | Handwritten `Access<Field>` methods | Same Go methods; no per-field generated hover notices |
| Mutators, including distinct input/stored types | `Mutate<Field>` plus generated input fields | Preserve methods; validate/bind once at startup or explicit mutator option; typed input `Set`, Stage 5 |
| Lifecycle and per-write factory state | Generated hooks and drafts | Typed framework hooks/write state and column handles, Stage 5 |
| Retrieval hooks and observers | Generated adapters and registration | Explicit retrieval options/application observers, Stage 5 |
| Timestamps and soft deletes | Field-name conventions and opt-out | Same behavior, `NoTimestamps`, optional explicit temporal bindings; core ported, completion Stage 3 |
| Integer, UUID and natural keys | `primary=` annotation | `TableOf` binds concrete `K`; preserve UUID source/default rules, Stage 3 |
| Global scopes | `defineXGlobalScopes` | Typed scope options, Stage 5 |
| Direct belongs-to, has-one, has-many, self and nullable-FK relations | Slots and generated relation sets | `BelongsToOf`/`HasOneOf`/`HasManyOf`; delivered in 4b, including singular cardinality checks |
| Nested and constrained eager loads; LoadMissing | Descriptor `Where`/`OrderBy`/`With`, query `Load`/`LoadMissing` | Stage 4b: immutable descriptors, complete target batches at each level, package `Load`/`LoadMissing`, loaded-empty preservation and primary-key order tie-breakers |
| Relation limits and depth | Query `WithRelationLimits` and shared loading budgets | Stage 4b: `orm.WithRelationLimits(q, limits)`; shared row/attachment budgets and depth bound, defaults from the existing owner. No relation `Limit`/`Offset` or per-parent limits |
| Relation existence filters | `WhereHas`/`WhereDoesntHave`, descriptor `Exists` | Stage 4b: `Where(orm.Has/DoesntHave(...))`, nested correlated subqueries with related scopes and zero extra statements |
| Pivots and WithPivot | Generated relation sets, `Through` slots and joined loaders | Stage 4c delivered: `ThroughOf` and `ManyToManyOf`, typed links, target/pivot constraints and nested branches, independent pivot ownership and soft-delete scopes |
| Has-one/has-many-through | Joined hop loaders | Stage 4c delivered: `HasOneThroughOf`/`HasManyThroughOf`, both key pairs checked and shared batched plans |
| One-of-many | Latest, oldest and custom singular choices | Stage 4c delivered: immutable `OfMany`/`LatestOfMany`/`OldestOfMany`, `DISTINCT ON` per parent and primary-key tie-breaker |
| Polymorphic relations | MorphOne/Many/To/ToMany/MorphedByMany | Stage 4c delivered: corresponding `orm.*Of` constructors, validated `MorphName`, concrete target slots and discriminator-group batching |
| Relation aggregate slots | Generated computed slots | Design and implementation in Stage 4d |
| Relation writes and pivot lifecycle | Attach/detach/sync/toggle/update-pivot | Planned Stage 5b, alongside lifecycle integration |
| Stored change sets | Generated `CompareX` and field structs | Column-keyed typed comparisons and changes, Stage 5 |
| Draft, batch, lookup, conflict and source writes | Generated drafts/wrappers | Assignments and package functions, Stages 3/6e; preserve hook versus set-write semantics |
| Reference and identity | `FoundryReference`/`FoundryIdentity` | Table/column-based adapters retaining model/key and persisted identity, Stage 7 |
| Attachments, translations and metadata | Slot fields plus generated policies | Preserve fields, stored owner names, policies and cleanup; declarations in Stage 7 |
| Typed raw hydration | Executor-level raw only | Stage 3.1d: model/row decoders, exact named column set, no scopes or automatic routing |
| Chunks, pagination and locking | Generated wrappers and query runtime | Stage 3.1c: cursor/key-chunk package functions; keyed/locked lookup composition; shared tokens and unchanged HTTP shape |
| Audit/history and pruning | Generated wrappers and query runtime | Stages 5/6e/7; preserve redaction, transaction and outcome contracts |
| Projections, joins, CTEs, sets, windows and subqueries | Generated selectors and `query` families | `orm/expr` and `orm/rows` functions, Stages 6a–6d |
| Model binding, factories, datatables and database validation | Generated metadata and typed query interfaces | Table/column adapters, Stage 7; TS/OpenAPI/manifest contracts must remain identical |

Generated field hover notices move to handwritten comments and declaration docs.
Unsupported fields, undeclared persisted fields and invalid mutator signatures
move to aggregated startup errors before SQL. Owner, value and capability rules
remain compile-time constraints.

## Raw SQL levels

| Level | Boundary | Status |
| --- | --- | --- |
| 1 | Existing executor `Exec`, `Query`, `database.ScanOne` and read-routed `DB.QueryRead`; transactions unchanged | Retained |
| 2 | Typed model/declared-row hydration from raw SQL; validate selected columns rather than return partial models | Implemented Stage 3.1d: `Table.Raw` / `rows.Raw`, exact names in any order; no automatic scopes or read routing |
| 3 | Opt-in typed fragments (`expr.Raw[S,V]`, `orm.WhereRaw[M]` sketches), composed with existing scopes and transaction | Planned Stage 6a |

All levels bind arguments instead of interpolating values. Raw fragments require
the caller to declare result types. A project may forbid raw call sites through
the planned opt-in lint guard. Typed advanced SQL remains a first-class parity
requirement; raw SQL does not substitute for porting its compiler rules.

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
| ported | 93 |
| port | 489 |
| merge | 0 |
| obsolete | 0 |
| unaffected | 444 |
| **Total** | **1026** |

| Area | Cases |
| --- | ---: |
| Authentication and security | 94 |
| Bulk writes and conflict policies | 56 |
| Cache, Redis and coordination | 61 |
| Core reads, writes and codecs | 74 |
| Events, jobs and messaging | 54 |
| Expressions and aggregates | 70 |
| Framework integrations | 81 |
| HTTP, DTOs and transport contracts | 112 |
| Joins and correlation | 101 |
| Lifecycle, mutators and observers | 38 |
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
| 5 | 38 |
| 6a | 70 |
| 6b | 100 |
| 6c | 73 |
| 6d | 60 |
| 6e | 56 |
| 7 | 81 |
| — | 444 |

93 old catalogue rows are covered by 85 distinct Go-way cases.

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

Stage 4c is accepted and **Done pending the lead's commit**. Build, vet, race,
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
