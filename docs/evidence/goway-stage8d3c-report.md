# Stage 8d3c — cursor publication and surface closure

Base: `ff2f1c4c`. Cursor publication, approved removals and the collection-terminal addendum are complete; all requested acceptance passed.

The HTTP cursor contract ID changes from the retired query namespace to
`github.com/weiloon1234/Foundry-Go/database/orm.Cursor[...]`. Token bytes, version
and model type identity stay unchanged. The historical datatable contract cohort
and all frozen oracle bundles/receipts remain untouched. `make publish-cursor-contracts`
creates the native cohort and provenance; only the cursor ID and dependent ownership
hashes may differ. The approved addendum removes one public method; footprint ceilings stay unchanged.

The migration table covers all 94 denial-ledger entries and the retired shared
contracts. CHANGELOG lists breaking changes and both positive-validation locale keys.
Current architecture/slot docs describe native ownership; dated milestone evidence
retains its original measurements and explicitly historical interpretation.

Acceptance and the categorized self-audit are recorded below.

## Protected publication

Generated only with `make publish-cursor-contracts`. Paths below are relative to
`tests/fixtures/consumer/clientcontracts/testdata/native-cursor-contracts`.
All other client and reporting artifacts match the historical cohort byte for byte.

| Artifact | Historical SHA-256 | Native SHA-256 |
| --- | --- | --- |
| `client/.foundry-gen.json` | `ef0315d4e0298f732bcd3e46ff4ede5375d998144fac71263aff34b24cb06dc6` | `16da9d961e3f739119219434a76138c6711c3ecbc0787a52db5b2714b5e56544` |
| `client/contracts_foundry.gen.ts` | `4e7d04978485be8e51660b3b183ed3630d69e25654ac8a0795bf5dae2e5d3e3d` | `16421f9e9791b786242933ed1fc7d1ffb911a9f6a8226291f9e615907a691790` |
| `client/contracts_manifest_foundry.gen.json` | `abd227d084a4fd4306c25e20bff00bcc81fdb67ef12a77f4efb3c7b6d4f0ffa3` | `adae09b0f9bd6d55a5bc43dce0352607a5d2ef2c0fe4a08a91e6ec8c9013b959` |
| `client/contracts_members_foundry.gen.ts` | `6593ec2e0c681379f94427582c11b7a48c5c47245e8f5c337bb8512e0948b926` | `42797e5e264faef943b5a6f883157b3c8ecd053bf0f9dcd2bd509fe4323639ad` |
| `client/contracts_openapi_foundry.gen.json` | `88b03c554a21065aa30d732dfdaf148a3d98a1919ce37a968da07572b92b30de` | `2ecf0fdccbbf75a884e3fd40027ea4af448be57886295f618a5db94133e9de3a` |

Full cohort provenance is [recorded here](../../tests/fixtures/consumer/clientcontracts/testdata/native-cursor-contracts/provenance.json).
The [fixture census](goway-stage8d3c-census.json) records current test digests separately
from frozen capture history. No frozen bundle, receipt or historical manifest changed.

## Final public-surface audit

The decision-9 removals remain absent from the snapshot. Keyed table operations
and scoped writes stay distinct by visibility and duplicate-row behavior. Row and
aggregate `.Value()` retain separate phases. Transaction/locked terminals require
concrete transactions; model reads retain retrieval callbacks. Relation getters
and iterators have different loaded-state/copy contracts. Native internal aliases
have one public owner, not compatibility re-exports.

The closure addendum removes `rows.Query.All`; all collection callers now use
`rows.Read(query).All`. The shared `Result.All` reader retains complete model
metadata and retrieval dispatch. Existing retrieval resource/failure matrices now
exercise both model queries and `rows.Read(rows.FromModel(q))`. Scalar/DTO reads
remain excluded from model callbacks. The denial ledger rejects reintroduction,
including aliases and promoted methods.

Exact snapshot subtractions (value and pointer method sets):

```text
rows.Query
  All func(ctx context.Context, ex github.com/weiloon1234/Foundry-Go/database.Executor) ([]R, error)
*rows.Query
  All func(ctx context.Context, ex github.com/weiloon1234/Foundry-Go/database.Executor) ([]R, error)
```

Framework stores, row tests, independent consumers, migration excerpts and seven
footprint probes use the retained terminal. Editor probes and synthetic native
examples already used the retained route. Historical oracle records are unchanged.

## Search disposition

The [line census](goway-stage8d3c-audit.json) lists paths/lines for the retired import,
Legacy marker, deprecation marker, retired filenames and all 94 ledger spellings.
Hits fall into historical evidence/design/release notes, frozen records, guard and
negative-fixture data, migration before-blocks, or historical assertion names.
The synthetic old-style generator targets only an explicitly separate historical
checkout. Unrelated native names (`ErrLegacyLayout`, `DisableLegacyXSSFilter`) and
HTTP `Deprecated` metadata are retained supported protocols, not removed API aliases.
No live retired import, exported deprecation or guard exemption was added.

## Acceptance

All commands passed on r19, `GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`. PostgreSQL
configuration was sourced privately with `FOUNDRY_TEST_POSTGRES_REQUIRED=1`.
The supplied Node, TypeScript and gopls executables were used.

| Check | Result |
| --- | --- |
| `make publish-cursor-contracts` | PASS; new cohort, exact five-file change set |
| `make generate` | PASS; zero retained Go-output changes |
| `go build ./...`; `go vet ./...` | PASS |
| Root `make test` | PASS, including exemption-free retirement guard and all negatives |
| `go test -race ./http/pagination/... ./testkit/dbassert/...` | PASS |
| `make generate-check`; `make datatable-contracts` | PASS |
| `make typescript-check` | PASS; HTTP/WebSocket and React/Vue clients |
| `make agent-smoke` | PASS; real gopls |
| `make fixture-check` | PASS; all four modules, vet/tests, PostgreSQL, frozen replay and footprint budgets |
| `python3 tools/goway_catalogue.py` | PASS; 587 ported, 445 unaffected, zero pending |
| `make release-tools-check` | PASS; Go vet/tests and Python 37/37 |
| `make docs-check`; `make fmt-check`; `git diff --check` | PASS |

The first publisher attempt required creating its output directories; the corrected
Makefile publication passed. The initial sandboxed root-test batch could not open
local PostgreSQL/listeners or write module metadata. It was stopped and the complete
root target passed with the required permissions. No source guarantee was weakened.
Release tooling's printed `phase: failure, exit: 7` is an expected negative fixture;
the enclosing 37-test suite passed.

Cursor publication adds no generic surface. The addendum subtracts the two method-set
lines above; existing footprint ceilings remain unchanged. Historical baselines,
oracle bundles and receipts have no diff.

## Docs and release notes

Updated native model/query/extension architecture, current guides and consumer
READMEs; removed stale transaction, model-draft and generated-owner descriptions.
The migration guide lists the denied declarations and retired shared contracts
in a labelled doc-only before block. CHANGELOG includes the complete breaking set,
canonical SET ordering, EnvelopeV1, cursor contract regeneration, and the two locale
keys `foundry.decimal_positive` / `foundry.positive`. Root/scoped instructions,
editor sources and scaffold templates need no API edits. The Makefile generation
comment now describes retained DTO/configuration output.

Master status records Stage 8 completion, including the approved collection-terminal
closure.
There is no compatibility exemption or new alternate path. No commit was made.

## Closure-addendum footprint

Paired counts use the same Go 1.27.1 darwin/arm64 settings and the shared
`tests/oracle.CountSymbols` T/t archive-symbol counter. The before run overlays
HEAD's removed method and the seven original probes without modifying the checkout.
This measures generic archive surface, not runtime speed. The common `Result`
terminal adds record-reader instantiations to the seven direct projection probes;
all remain below their existing ceilings. No budget changes.

| Probe | Before | After | Ceiling |
| --- | ---: | ---: | ---: |
| aggregate_families | 4331 | 4326 | 4369 |
| json_functions | 2708 | 2809 | 3147 |
| json_paths | 5680 | 5781 | 7068 |
| q_with | 1173 | 1173 | 1211 |
| raw_fragments | 1349 | 1449 | 1531 |
| scalar_temporal | 2598 | 2699 | 3019 |
| scalar_text | 2284 | 2385 | 2659 |
| window_frames | 1738 | 1837 | 2033 |
| window_rank | 1315 | 1413 | 1547 |

## Closure-addendum acceptance

The addendum migrates 224 typed receiver uses in 92 source files, plus public
comments and guide excerpts. No new terminal or compatibility alias is introduced.
Both value/pointer method-set subtractions are recorded above. The migration
census retains all original assertions; current digests are refreshed separately
from frozen evidence.

Build, vet, root `make test`, four-module `make fixture-check`, and all touched-package
races passed on r19 with PostgreSQL required. `make agent-smoke` passed all real
gopls probes (310.211s), including its generation check prerequisite. Catalogue
validation passed: 587 ported, 445 unaffected, zero pending. Final `make docs-check`,
`make fmt-check` and `git diff --check` passed.

The receiver rewrite initially missed same-package tests and a local `rows` name
shadowing an import; both were corrected before the passing verification runs.
Root model/retrieval, scalar/DTO exclusions, callback failures and cancellation
assertions remain intact. The retirement guard includes the new ledger entry and
continues to pass without exemptions.

## Decision 10 final audit closure

Removed eleven redundant declarations, with all runtime, fixture, guide and editor
callers migrated atomically:

| Removed | Retained operation |
| --- | --- |
| rows.Paginate / rows.SimplePaginate | rows.Read(q).Paginate / SimplePaginate |
| rows.Query.OrderBy | rows.OrderBy(q, orders...) |
| rows.ExplainLocked / rows.ExplainAnalyzeLocked | rows.ExplainTransaction / ExplainAnalyzeTransaction |
| orm.DoesntHave | orm.Not(orm.Has(...)) |
| expr.JSONOrderedScalar | expr.OrderNullableRow(expr.JSONScalar(...)) |
| expr.NullFor | expr.NullableRow(v).Param(value.Null[T]()) |
| extensions.Scope | extensions.ORMIdentity(owner).Scope |
| extensions.Owner.ModelName / Reference | extensions.ORMIdentity(owner).ModelName / Reference |

The audit's borderline groups remain: distinct query phases and transaction
capabilities, scoped keyed writes, typed expression families, persisted owner
scope and storage-aware parsing. No lowering or callback implementation was copied.
The denial ledger now has 105 entries and no exemptions.

Package guides now describe shipped features and complete-model EachRead callbacks.
The migration table uses rows.Field[R,V] and describes scoped Update/Delete as
keyed writes with visibility, lifecycle and cardinality checks. Release notes cover
each removal plus RequireFirst → Require and InsertOrFirst → CreateOrFirst.
The docs guard scans inline code and complete Go fences for unqualified retired
names, including saved/returned function values, while ignoring prose, comments,
strings and qualified retained owners. Regression sources cover both directions.

Exactly nine public snapshot lines are subtracted by this decision (both method
sets for Query.OrderBy plus seven functions). The three extensions declarations
were not included in this snapshot; the retirement ledger checks them.

```text
orm.DoesntHave func[M, N any](r github.com/weiloon1234/Foundry-Go/database/orm.ExistenceRelation[M, N], ps ...github.com/weiloon1234/Foundry-Go/database/orm.Predicate[N]) github.com/weiloon1234/Foundry-Go/database/orm.Predicate[M]
rows.Query
  OrderBy func(orders ...github.com/weiloon1234/Foundry-Go/database/orm/expr.Order[S]) github.com/weiloon1234/Foundry-Go/database/orm/rows.Query[S, R]
*rows.Query
  OrderBy func(orders ...github.com/weiloon1234/Foundry-Go/database/orm/expr.Order[S]) github.com/weiloon1234/Foundry-Go/database/orm/rows.Query[S, R]
rows.ExplainLocked func[S, R any](ctx context.Context, tx *github.com/weiloon1234/Foundry-Go/database.Tx, q github.com/weiloon1234/Foundry-Go/database/orm/rows.LockedResult[S, R]) (github.com/weiloon1234/Foundry-Go/database/orm.Plan, error)
rows.ExplainAnalyzeLocked func[S, R any](ctx context.Context, tx *github.com/weiloon1234/Foundry-Go/database.Tx, q github.com/weiloon1234/Foundry-Go/database/orm/rows.LockedResult[S, R]) (github.com/weiloon1234/Foundry-Go/database/orm.Plan, error)
rows.Paginate func[S, R any](ctx context.Context, ex github.com/weiloon1234/Foundry-Go/database.Executor, q github.com/weiloon1234/Foundry-Go/database/orm/rows.Query[S, R], request github.com/weiloon1234/Foundry-Go/database/orm.PageRequest) (github.com/weiloon1234/Foundry-Go/database/orm.PageResult[R], error)
rows.SimplePaginate func[S, R any](ctx context.Context, ex github.com/weiloon1234/Foundry-Go/database.Executor, q github.com/weiloon1234/Foundry-Go/database/orm/rows.Query[S, R], request github.com/weiloon1234/Foundry-Go/database/orm.PageRequest) (github.com/weiloon1234/Foundry-Go/database/orm.SimplePage[R], error)
expr.NullFor func[S, V any](v github.com/weiloon1234/Foundry-Go/database/orm/expr.RowValue[S, V]) github.com/weiloon1234/Foundry-Go/database/orm/expr.RowExpression[S, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
expr.JSONOrderedScalar func[S any, P github.com/weiloon1234/Foundry-Go/database/orm/expr.orderedScalar](path github.com/weiloon1234/Foundry-Go/database/orm/expr.JSONPath[S, P], c github.com/weiloon1234/Foundry-Go/database/codec.Codec[P]) github.com/weiloon1234/Foundry-Go/database/orm/expr.NullableOrderedRowExpression[S, P]
```

Acceptance for this final audit is recorded separately below; earlier pass records
above describe the preceding closure revision.

The first acceptance pass exposed stale editor completion expectations, missed
extension-owner accessors (including compile-fail operands), and one compiled
query-key guide excerpt. These were migrated to their retained owners; the
corresponding checks were rerun without changing assertions or diagnostics.
Three consumer catalogue rule texts were refreshed after reviewing their unchanged
result-owner, nullable-page and relation-owner guarantees. All 74 changed fixture
test digests match current source; frozen bundles and receipts remain unchanged.

### Final-audit footprint

These deterministic T/t archive-symbol counts compare the base ff2f1c4c (temporary
Go overlay of its declarations and probes) with the cumulative slice, including
the previously approved Query.All migration. Same Go 1.27.1 darwin/arm64, r19 and
-p=4 settings; one count per archive through tests/oracle.CountSymbols. This is
compiled generic surface, not a runtime-speed benchmark. All existing budgets pass.

| Probe | Base | Current | Ceiling |
| --- | ---: | ---: | ---: |
| aggregate_families | 4331 | 4320 | 4369 |
| conditional_row | 1774 | 1768 | 1934 |
| distinct | 1143 | 1128 | 1306 |
| expr_arithmetic | 1225 | 1224 | 1337 |
| json_functions | 2708 | 2799 | 3147 |
| json_paths | 5680 | 5771 | 7068 |
| q_with | 1173 | 1173 | 1211 |
| raw_fragments | 1349 | 1439 | 1531 |
| report_pipeline | 2069 | 2063 | 2191 |
| report_reads | 1040 | 1025 | 1223 |
| scalar_temporal | 2598 | 2689 | 3019 |
| scalar_text | 2284 | 2375 | 2659 |
| terminal_explain | 1291 | 1271 | 1516 |
| window_frames | 1738 | 1827 | 2033 |
| window_rank | 1315 | 1403 | 1547 |

Compared with the preceding closure measurements above, this audit removes six
functions from aggregate_families and ten from each of json_functions, json_paths,
raw_fragments, scalar_temporal, scalar_text, window_frames and window_rank. q_with
is unchanged at 1,173 / 1,211. The cumulative growth over base in seven projection
probes belongs to the previously reported Result-based collection adapter; the
new removals reduce it. No footprint ceiling or frozen observation changed.

### Final-audit acceptance

| Check | Result |
| --- | --- |
| Root build and vet | PASS |
| Root make test | PASS, segmented: corrected editor subtest and unexecuted batches resumed |
| Touched-package races with PostgreSQL required | PASS: 20 root, 25 consumer and four Go-way packages |
| make fixture-check | PASS, segmented: first three modules; Go-way vet/tests; corrected guide excerpt rerun |
| Compile-fail catalogue | PASS: 587 ported, 445 unaffected, zero pending |
| Footprint budgets and shared-counter comparison | PASS; ceilings unchanged |
| make generate-check | PASS through make agent-smoke prerequisite; all 15 output groups current |
| make agent-smoke | PASS: real-gopls probes, 289.016s |
| make docs-check / make fmt-check / git diff --check | PASS |

No commit was made.

## Confirming-audit closure: nine forwarders and generic documentation members

Removed extensions.Owner.Active in favor of ActiveSubjects; its two metadata and
translation callers discard the unused ordered-key result. Moved scalar SQL and
codec construction into the eight retained row constructors. Callers use Value()
for the selected phase; nullable widening, one-layer nullable inputs, correlation
ownership and transaction constraints retain the same validation and lowering.

| Removed | Retained |
| --- | --- |
| rows.ScalarQuery | rows.ScalarRowQuery(...).Value() |
| rows.ScalarNullableQuery | rows.ScalarNullableRowQuery(...).Value() |
| rows.CorrelatedScalarQuery | rows.CorrelatedScalarRowQuery(...).Value() |
| rows.CorrelatedScalarNullableQuery | rows.CorrelatedScalarNullableRowQuery(...).Value() |
| rows.TransactionScalarQuery | rows.TransactionScalarRowQuery(...).Value() |
| rows.TransactionScalarNullableQuery | rows.TransactionScalarNullableRowQuery(...).Value() |
| rows.TransactionCorrelatedScalarQuery | rows.TransactionCorrelatedScalarRowQuery(...).Value() |
| rows.TransactionCorrelatedScalarNullableQuery | rows.TransactionCorrelatedScalarNullableRowQuery(...).Value() |
| extensions.Owner.Active | extensions.Owner.ActiveSubjects |

Eight exact snapshot lines are subtracted below. Owner.Active was outside the ORM
snapshot; the denial ledger checks it. The ledger now contains 114 entries, with
no exemptions. Migration-guide rows and CHANGELOG notes cover all nine removals.

```text
rows.ScalarQuery func[S, I, V any](outer github.com/weiloon1234/Foundry-Go/database/orm/rows.SelectionSource[S], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.Scalar[I, V]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[S, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.ScalarNullableQuery func[S, I, V any](outer github.com/weiloon1234/Foundry-Go/database/orm/rows.SelectionSource[S], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.Scalar[I, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[S, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.CorrelatedScalarQuery func[O, I, V any](s github.com/weiloon1234/Foundry-Go/database/orm/rows.CorrelatedScalar[O, I, V]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[O, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.CorrelatedScalarNullableQuery func[O, I, V any](s github.com/weiloon1234/Foundry-Go/database/orm/rows.CorrelatedScalar[O, I, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[O, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionCorrelatedScalarNullableQuery func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](s github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[O, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionCorrelatedScalarQuery func[O, I github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](s github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionCorrelatedScalar[O, I, V]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[O, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionScalarNullableQuery func[S github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](outer github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionSelectionSource[S], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionValueSource[github.com/weiloon1234/Foundry-Go/value.Nullable[V]]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[S, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
rows.TransactionScalarQuery func[S github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionScopeType, V any](outer github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionSelectionSource[S], inner github.com/weiloon1234/Foundry-Go/database/orm/rows.TransactionValueSource[V]) github.com/weiloon1234/Foundry-Go/database/orm/expr.Selected[S, github.com/weiloon1234/Foundry-Go/value.Nullable[V]]
```

The documentation scanner now checks members at the computed position after type
arguments and scans untagged fences as Go. Negatives cover Query[S,R].OrderBy,
Query[S,R].All, nested type arguments, qualified rows.Query[S,R].All, and both
backtick/tilde untagged fences. Qualified retained orm.Query methods remain valid.
Current fixture digests are refreshed after reviewing the migrated assertions;
frozen observations, bundles and receipts remain unchanged.

This round migrated 110 scalar calls in 54 Go files plus the two Active callers.
Four owned consumer catalogue rules now name the retained row constructors; their
failure classifications and expected diagnostics are unchanged. The current
census contains 85 reviewed fixture tests. The full editor run found one stale
Active completion expectation; the retained ActiveSubjects probe passed after
correcting that expectation, and root acceptance resumed at the remaining batches.

### Confirming-audit footprint

Counts compare the accepted preceding working-tree revision (captured before this
follow-up in a Go overlay) against these nine removals, using the same Go 1.27.1
darwin/arm64 / r19 / -p=4 environment and shared tests/oracle.CountSymbols T/t
archive counter. This measures compiled generic surface, not runtime speed.

| Probe | Before | After | Existing ceiling |
| --- | ---: | ---: | ---: |
| aggregate_families | 4320 | 4320 | 4369 |
| correlation | 2930 | 2933 | 3165 |
| q_with | 1173 | 1173 | 1211 |
| source_clauses | 2444 | 2446 | 2881 |
| subqueries | 3000 | 3006 | 3263 |
| transaction_correlation | 2839 | 2842 | 3345 |

The four scalar probes add two to six archive functions at sites that now use the
row-expression constructor and Value() bridge. q_with and aggregate_families are
unchanged. Every full-suite footprint budget passes, with no ceiling increases.
The pre-follow-up constructor/probe hashes and current source hashes are retained
in the JSON evidence.

### Confirming-audit acceptance

| Check | Result |
| --- | --- |
| Root build and vet | PASS |
| Root make test | PASS, segmented: corrected one editor expectation and resumed unexecuted batches |
| Touched-package races, PostgreSQL required | PASS: seven root, 15 consumer and two Go-way packages |
| make fixture-check | PASS: all four modules, including PostgreSQL, replay, compile-fail and footprint guards |
| Catalogue | PASS: 587 ported, 445 unaffected, zero pending |
| make agent-smoke, including generate-check | PASS: 15 generated-output groups current; real-gopls probes 293.292s |
| make docs-check; make fmt-check; git diff --check | PASS |

No commit was made.
