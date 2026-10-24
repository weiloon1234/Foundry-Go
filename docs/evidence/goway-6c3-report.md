# Stage 6c3: completed-result cursors and read/lock terminals

## API and implementation

`CursorFor(RecordSource[R])` and `ValueCursorFor(ValueSource[V])` create distinct
completed-output scopes. Both builders expose Scope, Where, OrderBy, UniqueBy
and Paginate; the scalar builder also exposes Value and Key. Original filters,
ordering and windows remain behind an allocated alias. Cursor builders cannot
enter ordinary record composition or mutation. `UniqueBy` is an explicit caller
assertion, not deduplication. Typed request/page adapters remain the existing
`query.CursorRequest` and `query.CursorPage` contracts.

The existing querycore cursor token, fingerprint, nullable keyset and boundary
compilers are reused. Complete-result alias construction and canonical order
and field-codec validation were extracted into querycore and are now shared
with the old API. Count/exists statement envelopes also have a single querycore
owner, preserving statement-shape tracking in both APIs.
Complete model, projection and scalar records retain field getters and decoding
codecs. Sensitive keys fail before SQL. Forward/backward navigation reads one
lookahead row and discards the page on decoding, closure, lifecycle or token error.

Read, ReadScalar, ReadRecord, ReadSet and ReadValues return a thin Result adapter
with All, First, RequireFirst, Count, Exists, Paginate and SimplePaginate plus
Where, OrderBy, Limit and Offset. Numbered pages require explicit ordering and
no outer window; inner windows remain. Complete model terminals retain retrieval
callbacks through the existing row/lifecycle owner. No Query/Table method changed.

SelectTransactionValue retains an exact scalar type and requires `*database.Tx`.
TransactionValue and TransactionRead have All, First, RequireFirst, Count and
Exists. ForUpdate, ForNoKeyUpdate, ForShare, ForKeyShare and LockRows create a
LockedResult, with Target wrapping preserved scopes. Independent UpdateLock,
NoKeyUpdateLock, ShareLock and KeyShareLock descriptors carry per-target wait
policies. Nullable targets are excluded statically. Runtime declaration ownership
and lock eligibility remain validated by the shared compiler. LockedResult has
All, First and RequireFirst; no count, pagination or ordinary source conversion.

All new generic structs retain nongeneric state or the existing thin typed
column closures. There are no embedded generic method chains, zero-value seals,
new dependencies or capabilities passed to application callbacks.

## Catalogue rows

This slice ports 25 Stage 6c rows: eight cursor, two pagination, three exact
projection-type and twelve lock/transaction contracts. Four additional compile
failures cover transaction cursor input, cursor/record conversion, nullable lock
targets and locked/ordinary result conversion. The registry now has 441 cases.
Exact case names and hashes are recorded in the evidence JSON.

Stage 6c now has **72/73 rows ported**. The only remaining row is
`transaction_nullable_record`, which needs the deferred Stage 6b4 transaction
outer joins and their nullable scopes. This slice does not claim that row using
a missing-symbol failure.

Streaming Each is not introduced here. The delivered terminals collect complete
batches; old callback/lifecycle streaming behavior still needs a separate shared
adapter if required to close full API parity beyond the catalogue.

## Regressions and PostgreSQL sources

Runtime coverage exercises retained source windows, forward/backward and nullable
navigation, token/query mismatch, malformed tokens, foreign/repeated/missing keys,
computed/null-placement rejection, sensitive keys, failed row closure, partial
decoding, read terminals and preserved lock targets. Thirteen old/new goldens
cover model/CTE/set/scalar cursors, numbered/simple pages, first/required reads,
count/exists and ordinary/transaction scalar locking.

PostgreSQL sources cover real cursor navigation, numbered scalar pages and
transaction-only projection locks. They are compiled and vetted only; execution
belongs to the lead.

## Footprint and surface

Measurements use Go 1.27.1 darwin/arm64, the requested warm cache and CPU limits,
and the existing archive T/t-symbol guard. Exact additive signatures are in the JSON.

| Probe | Measured | Ceiling |
| --- | ---: | ---: |
| result_cursor (new) | 2,229 | 2,564 |
| result_reads (new) | 1,351 | 1,554 |
| q_with | 1,209 | 1,211 |
| aggregate_families | 4,366 | 4,369 |

q_with is unchanged. aggregate_families grows by four functions, one shared
bind/decode closure per declared result-field type, to support projection cursor
keys. The previous 65 ceilings are unchanged; 67 probes now exist. No runtime
performance claim is made.

The public snapshot adds 136 lines and removes none. Additions cover CursorScope,
CursorQuery, ValueCursorQuery, Result, TransactionValue, ReadSource, LockTarget,
RowLock and LockedResult, their documented methods, and the 18 package functions
listed in the API section. TransactionRead adds First, RequireFirst, Count and
Exists. No Query/Table method changes.

The reachability walker now permits only the existing thin Cursor, CursorRequest
and CursorPage transport DTOs alongside Page/SimplePage, continuing to traverse
all fields and methods. A regression still rejects the old CursorQuery engine.
New probes are result_cursor and result_reads. Their ceilings use exactly
ceil(measured × 1.15). Existing ceilings remain unchanged.

The model cursor codecs live behind the existing nongeneric model metadata
adapter. Row declarations use a single typed codec closure for both field access
and boundary decoding to limit growth in aggregate_families. No footprint growth
is attributed to unrelated features.

## Shared files

- `internal/querycore/result_cursor.go` and `database/query/result_cursor.go`:
  shared completed-result boundary, order and field-codec validation.
- `internal/querycore/query_core.go`, `database/query/read_result.go`:
  single-owner count/exists statement envelopes, including shape tracking.
- `internal/ormcore/record.go`, `source.go`: declared field codecs and sensitivity.
- `internal/ormcore/result_cursor.go`, `record_read.go`, `record_lock.go`:
  nongeneric planning, execution and lock adapters over existing compilers.
- `database/orm/record.go`, `source.go`: model getters and sensitivity metadata.
- `database/orm/expr/value.go`: preserve sensitivity through typed/nullable codecs.
- `database/orm/rows/declaration.go`, `record.go`, `value_set.go`: record codecs.
- `database/orm/surface_test.go`: recursively checked cursor transport allowlist.
- `database/orm/method_surface_test.go`, `testdata/public-methods.txt`:
  additive public snapshot.
- `tests/fixtures/goway/compiler_cases_test.go`, `footprint_test.go`:
  assigned compiler cases and new probes.
- `blueprint/goway-data-layer/compilefail-mapping.tsv`: only 25 assigned rows.
- `database/orm/rows/README.md`: links the dedicated results guide.

Other files are dedicated result adapters, regressions, consumer goldens,
pgsmoke sources, probes, compile-fail fixtures, documentation and evidence.
Accepted 6c1/6c2 evidence and lead-owned documents remain untouched. Delivery is
incremental against accepted 6c2, including untracked files, rather than branch HEAD.

## Acceptance

All Go commands use `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2`
`GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`. Fixture tests explicitly clear both
PostgreSQL URL/required controls. No PostgreSQL execution, cache deletion, Git
write or commit occurs. Current command results and corrections are in the JSON.

Final requested acceptance passed. Initial failures and their corrections remain
in the logs and evidence; no failed command is relabeled as a pass.

| Command | Final result |
| --- | --- |
| `go build ./...` | Pass; includes the final import-formatting cleanup |
| `go vet ./database/... ./internal/...` | Pass |
| `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/...` | Pass |
| Fixture `go vet ./...` | Pass, including pgsmoke sources |
| Fixture `go test ./...` | Pass; 441 compiler cases, 67 budgets, 13 new goldens |
| `python3 tools/goway_catalogue.py` | Pass |
| `sh tools/format-go.sh check /usr/local/go/bin/gofmt` | Pass; repository-wide gofmt clean |
| `git diff --check` | Pass |
| `make docs-check` | Pass |

The initial race run found the expected additive snapshot, the narrow transport
allowance needed by the reachability walker, and a pinned field-bound diagnostic.
All were corrected and the full race rerun passed. The initial fixture run
stopped because its COUNT golden supplied two scripted rows; that script now
supplies one, and the entire fixture suite passed. Source review consolidated
field-codec validation and count/exists envelopes before the final race. Only
import grouping followed that race; the root build and fixture vet/tests ran on
the formatted source. The module stat-cache write warning during root builds was
nonfatal. No test or ceiling was weakened to obtain these results.

## Docs notes

For lead-owned changelog/status: Stage 6c3 adds complete-result and scalar cursor
adapters with explicit output identities, typed nullable navigation, ordinary
read/page adapters and transaction-only scalar/projection locks. Cursor boundary
and order validation are shared with the old API. Twenty-five more Stage 6c rows
are ported (72/73 total); transaction_nullable_record awaits 6b4. Streaming Each
remains outside this slice. The package recipe is database/orm/rows/results.md.

## Questions for the lead

Integrate transaction_nullable_record with 6b4 and run the pgsmoke sources at the
next PostgreSQL checkpoint. Full streaming Each parity requires follow-up;
this delivery does not mark the entire Stage 6c API complete.

## Second-review correction: composed cursor sensitivity

Fixed the sole High finding from last-8.md. Both ORM and old query result
cursors now use querycore.SelectSensitivity during shared field validation.
Sensitivity is independent of the left operand's decoding codec and follows
each output position through both set operands, projection mappings, computed
expressions, scalar subqueries, aliases and CTEs. Recursive outputs use bounded
fixed-point propagation. The existing expression walker owns traversal; malformed
or over-budget provenance fails closed. Declarations and source nodes remain
immutable. Sensitive ordering/identity keys fail before SQL or token extraction.

Regressions cover sensitive right/left/nested value sets, record sets, ordinary
projection targets, alias-to-scalar and set-to-scalar selection, CTEs, computed
values and parameters, recursive cross-column propagation, malformed/cyclic/deep
provenance, and differing set column names. Public output keys still paginate,
and composing a sensitive query does not contaminate a reused public source.
Old API regressions exercise the same restrictions through Compile.

No public API/surface additions, catalogue changes, new dependencies or budget
changes in this correction. Stage 6c remains 72/73 rows;
transaction_nullable_record and streaming Each remain as reported above.

Current footprint measurements (unchanged ceilings):

| Probe | Measured | Ceiling | Change from reviewed 6c3 |
| --- | ---: | ---: | ---: |
| result_cursor | 2229 | 2564 | +0 |
| result_reads | 1351 | 1554 | +0 |
| q_with | 1209 | 1211 | +0 |
| aggregate_families | 4366 | 4369 | +0 |

Acceptance on the corrected source: build, scoped vet, full requested race,
fixture vet/test (including goldens, compiler cases and all 67 budgets), catalogue,
repository gofmt, diff whitespace and docs-check all passed. All Go commands use
the requested cache, GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off. PostgreSQL controls
were cleared for fixture tests; no PostgreSQL execution or Git writes. Detailed
commands and log hashes are in the evidence JSON under review_fix.

Changed/shared source files in this correction:

- `database/orm/expr/expression.go`
- `database/orm/expr/value.go`
- `database/orm/rows/declaration.go`
- `database/orm/rows/projection.go`
- `database/orm/rows/result_sensitivity_test.go`
- `database/orm/source.go`
- `database/query/compiler.go`
- `database/query/projection.go`
- `database/query/projection_definition.go`
- `database/query/result_cursor.go`
- `database/query/result_sensitivity_test.go`
- `database/query/row_expression.go`
- `internal/ormcore/result_cursor.go`
- `internal/ormcore/source.go`
- `internal/ormcore/subquery.go`
- `internal/querycore/parameter_expression.go`
- `internal/querycore/projection_core.go`
- `internal/querycore/result_cursor.go`
- `internal/querycore/select.go`
- `internal/querycore/select_sensitivity.go`
- `internal/querycore/select_sensitivity_test.go`
- `internal/querycore/select_walk.go`
- `internal/querycore/value_expression.go`

Also updated database/orm/rows/results.md and this slice's report/evidence.
Accepted 6c1/6c2 artifacts, lead-owned docs, catalogue and public snapshot are
unchanged.

### Docs notes

Changelog note for the lead: Fix composed-result cursor sensitivity in both ORM
and database/query. Sensitive values retain protection across set operations,
projection expressions and aliases; sensitive cursor keys are rejected before
SQL, while unrelated public output fields remain usable. No new lead decision
is required for this correction.

## Re-check correction: sensitivity at model and expression owners

Fixed all findings from last-9.md. ModelDefinition.Source now owns persisted-table
sensitivity for ordinary models, relation aggregates, through/pivot sources and
relation hops in both APIs. Legacy field lowering records codec sensitivity in
immutable expression metadata before scalar operations, conditional branches or
aggregate result codecs can replace the input codec. The shared expression
visitor, mapper and cursor provenance walker retain this annotation. SQL identity
and grouping checks look through it without removing it from the stored AST.

Regressions cover RelatedValue(Min(secretField)) with a public Coalesce fallback
for direct, through and pivot relations, using both model metadata and codec-only
marks. Define without optional ModelField getters is covered through Lower,
nested operations, row/selected/nullable Coalesce and NULLIF, CASE results and
conditions, aggregate codec changes, CountField, column comparisons, subquery
membership, window ordering/partitioning, grouping and requalification. Sensitive
cursor keys fail before executor calls or token creation; public controls compile.
Shared-source tests cover ordinary, through and hop sensitivity. Existing scope
checks continue to reject foreign fields under the metadata annotation.

No public API/surface additions, catalogue changes or footprint ceiling changes
in this correction. No new Query/Table methods. Stage 6c remains 72/73 catalogue
rows; transaction_nullable_record awaits 6b4, and streaming Each remains deferred.

| Probe | Measured | Ceiling | Change from reviewed source |
| --- | ---: | ---: | ---: |
| result_cursor | 2229 | 2564 | +0 |
| result_reads | 1351 | 1554 | +0 |
| q_with | 1209 | 1211 | +0 |
| aggregate_families | 4366 | 4369 | +0 |

Acceptance passed on the final source: go build ./...; scoped database/internal
vet; the requested ORM/query/querycore/ormcore race suite; fixture vet and test;
catalogue validation; repository-wide gofmt; diff whitespace; docs-check. All Go
commands used the requested cache, GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off.
Initial build and vet caught a mechanical identifier typo and a regression-helper
phase-type assignment; both were corrected before the final passing runs. No
PostgreSQL execution, Git writes, dependency changes or existing budget increases.
Exact commands, logs and source hashes are in review_recheck in the evidence JSON.

Shared/changed files for this correction (plus this report and its evidence):

- `database/orm/declaration.go`
- `database/orm/expr/aggregate_fields.go`
- `database/orm/relation_aggregate_loading.go`
- `database/orm/relation_join_plan.go`
- `database/orm/relation_predicate.go`
- `database/orm/rows/results.md`
- `database/orm/source.go`
- `database/query/aggregate.go`
- `database/query/column_comparison.go`
- `database/query/comparison.go`
- `database/query/compiler.go`
- `database/query/composition_sensitivity_test.go`
- `database/query/conditional_expression.go`
- `database/query/conflict_mutator.go`
- `database/query/definition.go`
- `database/query/field.go`
- `database/query/field_binary.go`
- `database/query/field_capabilities.go`
- `database/query/field_interval.go`
- `database/query/field_numeric.go`
- `database/query/join_condition.go`
- `database/query/read_lifecycle_chunks.go`
- `database/query/relation_aggregate_source.go`
- `database/query/relation_through.go`
- `database/query/relation_through_hop.go`
- `database/query/row_expression.go`
- `database/query/scalar_operation.go`
- `database/query/subquery.go`
- `database/query/value_expression.go`
- `database/query/write_lookup.go`
- `internal/ormcore/aggregate.go`
- `internal/querycore/aggregate.go`
- `internal/querycore/compile_conflict_target.go`
- `internal/querycore/compile_mutation.go`
- `internal/querycore/compile_source_write.go`
- `internal/querycore/compiler.go`
- `internal/querycore/computed_expression.go`
- `internal/querycore/computed_mapping.go`
- `internal/querycore/encrypted_fields.go`
- `internal/querycore/key_compiler.go`
- `internal/querycore/model_source.go`
- `internal/querycore/model_source_test.go`
- `internal/querycore/query_core.go`
- `internal/querycore/relation_join.go`
- `internal/querycore/select_sensitivity.go`
- `internal/querycore/select_sensitivity_test.go`
- `internal/querycore/select_walk.go`
- `internal/querycore/value_expression.go`
- `internal/querycore/value_sensitivity.go`

### Docs notes

Changelog note for the lead: Preserve sensitive-value provenance through relation
aggregates and computed/conditional expressions in both query APIs, including
legacy models without optional field getters. Cursor validation rejects these
keys before SQL or token creation. Shared source metadata and expression traversal
own the rule; public results retain their existing behavior.

No new questions for the lead. Accepted 6c1/6c2 artifacts and lead-owned docs remain
unchanged. The fix-only patch is /private/tmp/goway-6c3-rereview.patch; the full
6c3 patch against accepted 6c2 remains /private/tmp/goway-6c3.patch.
