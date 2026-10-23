# Stage 6c1: complete records and projection adapters

## Inventory and slice plan

The old `database/query` API has complete model/projected record sources,
declaration-ordered codecs, preserved and nullable record scopes, derived aliases,
and read-only projection terminals. Its CTE family includes ordinary and recursive
CTEs, with a non-executable recursive self restricted to its construction scope.
Record/value set operations cover UNION, INTERSECT and EXCEPT, including ALL
variants, and retain exact result types with separate input/output scopes.
Result cursors retain explicit result scopes, ordering, unique keys and typed
requests. Transaction records, values, CTEs and projection locks form separate
executor families.

`orm/rows` already supplies declared row schemas, mappings, selection, model input,
typed expressions, grouping/having, scalar subqueries, joins, correlation and
LATERAL. This slice adds ordinary complete-record extraction and derived aliases,
plus the transaction record/projection foundations needed by 6b4.

The 73 Stage 6c catalogue rows group as follows. The evidence JSON lists every row.

| Family | Rows |
| --- | ---: |
| Projections, records, row value types and pages | 18 |
| CTEs and recursion | 12 |
| Projection row locks | 6 |
| Result cursors | 8 |
| Set operations | 9 |
| Transaction records, values, CTEs and execution | 20 |

The three-slice plan is:

1. Complete record/projection adapters, preserved record selection, ordinary
   aliases and transaction-only record/projection reads — this delivery.
2. Ordinary/transaction CTEs, recursive CTEs and record/value set operations;
   integrate the deferred 6b `lateral_record_cte` rejection against the actual API.
3. Result cursors, remaining read/page/scalar transaction and projection-lock
   terminals, and row-value cases. Coordinate join-dependent transaction coverage
   with 6b4.

## API and behavior

`RecordOf` adapts a complete `rows.Select` or `rows.FromModel` result;
`ModelRecord` adapts a model query. `AsRecord` introduces the existing alias/scope
boundary. `SelectRecord` selects the whole declared model or projection from a
preserved scope and produces a read-only `RecordRead`. Typed nullable scopes
cannot enter it; the shared compiler also rejects forged nullable-side metadata.
Filters, mapping order, limits and offsets remain within derived boundaries.
Only a clause-free model source can use a direct table alias.

`TransactionOf` explicitly lifts an ordinary complete record. `LockedRecord`
retains a model query's lock strength and wait policy. `AsTransaction` and
`TransactionScope` create a separate transaction scope; `SelectTransaction` and
`SelectTransactionRecord` preserve the transaction requirement through projection
or complete selection. `TransactionRead.All` accepts only `*database.Tx` and
rejects nil. Ordinary and transaction descriptors have distinct structural phases
and metadata getters; executor kind, result type and declaration identity are
validated from the getter's own metadata. Embedding cannot supply a separate
eligibility marker for unrelated SQL.

`RecordRead` and `TransactionRead` each expose `Where`, `OrderBy`, `Limit`, `Offset`
and `All`. Package-level `rows.Limit` and `rows.Offset` retain projection windows
without adding methods to `rows.Query`. Model retrieval uses the existing
retrieval callback owner after collection and row closure. Scan, close or callback
failure discards the result. Declared projections retain their own codecs and do
not acquire model lifecycle behavior.

The nongeneric owner is `internal/ormcore/record.go`. It reuses
`querycore.SelectRecordNode`, `AliasNode` and existing read/compiler paths. No
querycore compiler was copied or modified. The existing relation-EXISTS and
subquery compilers remain shared. Two concrete ORM bridges validate the existing
`MetadataSeal`; no zero seal or capability reaches application callbacks.

`RecordOf` validates scalar-only legacy `rows.Query` values at runtime because
that existing type also represents scalar selections. Scalar descriptors are
excluded statically. Count/First/Each/pages, transaction scalar terminals and
projection-lock constructors remain later-slice work.

## Surface additions

The snapshot has 52 additions and no removals. Exact signatures are in
[the evidence](goway-6c1.json). Every addition is accounted for here:

- Value and pointer entries for `Record`, `RecordRead`, `TransactionAlias`,
  `TransactionAliased`, `TransactionRead`, `TransactionRecord`: 12 lines.
- `RecordSource` with sealed `recordData`, `TransactionRecordSource` with sealed
  `transactionRecord`, and `TransactionSelectionSource` with sealed
  `transactionSelection`: 6 lines.
- `Where`, `OrderBy`, `Limit`, `Offset`, `All` on both `RecordRead` and
  `TransactionRead`, in value and pointer method sets: 20 lines.
- ORM bridges `FoundryRecord`, `FoundryLockedRecord`: 2 lines.
- Rows functions `RecordOf`, `ModelRecord`, `AsRecord`, `SelectRecord`, `Limit`,
  `Offset`, `TransactionOf`, `LockedRecord`, `AsTransaction`, `TransactionScope`,
  `SelectTransaction`, `SelectTransactionRecord`: 12 lines.

No existing Query/Table method or signature changed.

## Catalogue rows and regressions

This slice ports 22 rows; 51 Stage 6c rows remain pending:

- `derived_enum_operator`, `derived_nullable_scope`, `derived_predicate_owner`,
  `derived_record_owner`.
- `projection_mapping_owner`, `projection_nullability`, `projection_where_owner`,
  `projection_write`.
- `record_selection_nullable`, `record_selection_result`,
  `record_selection_scope`, `record_selection_unlifted`, `record_selection_write`.
- `transaction_owner`, `transaction_pool`, `transaction_projection_mutation`,
  `transaction_projection_ordinary`, `transaction_projection_type`,
  `transaction_result`, `transaction_session`, `transaction_source_projection`,
  `transaction_source_value`.

`transaction_session` tests the available All terminal's executor restriction;
the old Count terminal is later work, recorded explicitly in the mapping.
Seven additional cases reject scalar adaptation, record owner conversion,
ordinary/transaction record and read conversion, transaction alias/source escape,
and lateral alias escape. All 29 fixtures use implemented APIs, not absent-symbol
placeholders. The total registry is 381 cases.

Runtime regressions cover ordered decoding, exact declaration/result identity,
nullable record selection, scalar and zero adapters, executor-kind erasure,
validated capability rejection, lifecycle timing/failure, direct alias eligibility,
and retained transaction locks. Five old/new SQL, binding and result goldens cover
models, windows, projections, transaction models and transaction projections.
Pgsmoke sources cover derived records and two-transaction SKIP LOCKED behavior;
they were compiled and vetted only.

## Footprint

Native Go 1.27.1 darwin/arm64 archive T/t function counts, using the requested warm
shared cache and the existing footprint guard's measurement method:

| Probe | Measured | Ceiling |
| --- | ---: | ---: |
| New `records` | 1,313 | 1,510 |
| New `transaction_records` | 1,296 | 1,491 |
| Existing `q_with` | 1,209 | 1,211 |
| Existing `aggregate_families` | 4,362 | 4,369 |

New ceilings are ceil(measured × 1.15). Both sensitive existing counts match the
lead's HEAD measurements exactly. All 60 existing budgets are unchanged; all 62
budgets pass. New probes exercise actual model/projection All paths and
locked/lifted transaction reads. This is a compiled-footprint measurement, not a
runtime performance claim.

## Shared files

Existing files changed from clean HEAD `80546f4`:

- `blueprint/goway-data-layer/compilefail-mapping.tsv` — only the 22 owned rows.
- `database/orm/method_surface_test.go` and
  `database/orm/testdata/public-methods.txt` — additive API coverage.
- `database/orm/rows/README.md` — link to the dedicated records guide.
- `database/orm/rows/clauses.go` and `projection.go` — retain private record owner.
- `database/orm/rows/declaration.go` — retain result type and share declared scan.
- `database/orm/rows/correlated_record.go` and `correlated_scalar.go` — rename the
  existing private selection adapter so transaction selection reuses it.
- `internal/ormcore/source.go` — share the existing reader with transaction
  validation; ordinary execution retains its existing behavior.
- `tests/fixtures/goway/compiler_cases_test.go` and `footprint_test.go` — additive
  cases and two new probes.

New shared owners are `internal/ormcore/record.go` and `database/orm/record.go`.
Other additions are dedicated rows implementation, guide, tests, consumer goldens,
pgsmoke, compiler cases, probes and evidence. No lead-owned document, lifecycle
implementation, querycore compiler, raw fragment or aggregate implementation changed.

## Acceptance

All Go commands use
`GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2 GOWORK=off`.
Fixture tests explicitly clear `FOUNDRY_TEST_POSTGRES_URL` and
`FOUNDRY_TEST_POSTGRES_REQUIRED`. No cache was deleted and no PostgreSQL was run.

| Command | Final result |
| --- | --- |
| `go build ./...` | Pass |
| `go vet ./database/... ./internal/...` | Pass |
| `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/...` | Pass; includes all touched runtime packages |
| Fixture: `go vet ./...` | Pass, including pgsmoke |
| Fixture: `go test ./...` | Pass; 381 compiler cases, 62 budgets and all goldens |
| `python3 tools/goway_catalogue.py` | Pass |
| `sh tools/format-go.sh check /usr/local/go/bin/gofmt` | Pass; repository-wide gofmt clean |
| Focused `projection_mapping_owner` compiler test after final formatting | Pass |
| `git diff --check` | Pass |
| `make docs-check` | Pass |

Initial corrections: additive API snapshot; unsupported Gt spelling in a new
probe/fixture changed to Eq; empty model order/predicate slices normalized for
direct alias parity; catalogue reason prefixes corrected; one anonymous-struct
fixture needed a second formatting pass. The final full gate includes the runtime
fix. The final formatting-only fixture change passed its focused compiler test.
A premature catalogue/format success update caused by a masked shell status was
corrected publicly; final checks were executed independently and passed.

## Docs notes

For the lead's changelog/status: Stage 6c1 adds complete model/projection record
adapters, preserved-scope record reads and transaction-only record/projection
composition, retaining derived windows, decoder identity, lifecycle callbacks and
inner model locks. Twenty-two Stage 6c rows are ported. CTEs/recursion, set
operations, cursors and remaining terminals are pending. The package guide is
`database/orm/rows/records.md`; 6b4 can reuse these transaction foundations.

Carry forward the approved branch-only 6b1 compatibility note: broadened
LeftScope/RightScope signatures preserve inferred calls, while explicit old type
arguments require updating. Lead-owned documents remain untouched.

## Questions for the lead

No unresolved question blocks this slice. Run the pgsmoke sources at the next
database checkpoint. Coordinate transaction join/nullable-record coverage with
6b4 and the deferred lateral/CTE rejection with 6c2. Nothing was committed.
