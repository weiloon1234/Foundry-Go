# Stage 9 lane C: remaining result clauses

Rebased onto `fdaff938` (batch-7b), which already includes Wave 1, A1 and source clauses.
Resolved the reapplied cumulative patch to retain only the result-clause delta.
Integrated evidence hashes, reporting comments and snapshot order are unchanged;
registries retain lane B/D entries alongside the result additions.
No commit. Generated outputs, directives, module files and Makefile are unchanged.

## API and shared ownership

Eight thin package functions; no Query/Table methods or new descriptor types:

| Function | Input/output family | Clause inputs |
| --- | --- | --- |
| `rows.GroupByScalar` | `Scalar[S,V]` | `...expr.GroupKey[S]` |
| `rows.HavingScalar` | `Scalar[S,V]` | `...expr.HavingPredicate[S]` |
| `rows.DistinctScalar` | `Scalar[S,V]` | none |
| `rows.DistinctOnScalar` | `Scalar[S,V]` | `...expr.GroupKey[S]` |
| `rows.DistinctOnScalarValues` | `Scalar[S,V]` | `...expr.ProjectionKey[S]` |
| `rows.DistinctRecord` | `RecordRead[S,R]` | none |
| `rows.DistinctSet` | `SetQuery[R]` | none |
| `rows.DistinctValues` | `ValueSetQuery[V]` | none |

Each delegates to the existing rows clause owner. Value-set DISTINCT reuses scalar
selection to retain the legacy `AS "value"` output alias. Codecs, nullability,
source ownership, scopes, input SELECTs and result bounds remain intact. No new
seals or interfaces are introduced; concrete descriptor inputs and shared metadata
checks retain B31. Tests reject same-model foreign declarations, foreign parameter
keys, zero descriptors, wrong phases and invalid ordering. Inputs remain immutable.

All eight functions are pinned surface additions; no removals or convertibility
allowlist additions. The six accepted source-clause functions retain their integrated snapshot order.

## Consumers and compiler rows

`distinctqueries`: migrated the ordinary scalar/record/set runtime assertions,
including enum/nullable results, grouping, selected aggregate ordering, parameterized
scalar ordering, CTE membership, COUNT/FIRST/EXISTS bounds, DISTINCT ON precedence,
invalid clauses and cancellation. The original complete-record streaming assertion
is isolated in `TestPostgresDistinctRecordStreaming` with a lane A TODO.

`expressionqueries`: migrated all three PostgreSQL tests, preserving row/selected
CASE/COALESCE/NULLIF, aggregate/window grouping, CTE/correlation composition,
relation aggregate slots, enum-codec failures and write predicates. The direct
scalar Compile assertions remain unchanged in `TestTypedConditionalSQL`, with a
lane A TODO. Neither package is claimed fully migrated until those terminals land.

Ported seven consumer negatives: `computed_group_owner`, `selected_group_phase`,
`selected_distinct_owner`, `distinct_key_owner`, `distinct_write`,
`distinct_predicate_owner`, `distinct_scalar_type`. Seven new Go-way negatives cover
scalar GROUP BY/HAVING/DISTINCT ON phase and ownership errors. Only owned catalogue
rows and compiler-registry entries changed.

SQL-and-argument parity covers grouped scalar HAVING/DISTINCT, selected DISTINCT ON
keys, unselected row keys, complete-record DISTINCT, record-set DISTINCT and nullable
value-set DISTINCT with ordering/windows and nested SELECT/CTE boundaries.

## Footprint and acceptance

Native Go 1.27.1 darwin/arm64, r8 cache, GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off.
New `result_clauses` probe exercises all eight functions and measures **2,188 T/t
functions**, ceiling **2,517 = ceil(2,188 × 1.15)**. Existing
ceilings are unchanged. `q_with` remains **1,211/1,211**, `aggregate_families`
**4,366/4,369**, and the accepted `source_clauses` probe **2,505/2,881**.
All 135 footprint probes passed on this base. Build, vet, rows/consumer tests
and races, the surface/convertibility guards, all 373 owned consumer negatives and
seven new Go-way negatives passed. PostgreSQL sources compile and vet; catalogue,
docs-check, gofmt and diff checks pass.
Exact commands, final results, footprint sweep and source hashes are recorded in
the companion evidence JSON. PostgreSQL execution remains lead-owned; local runs
explicitly unset its URL/required flags.

## Shared files in this increment

- `database/orm/rows/scalar_clauses.go` and `database/orm/rows/README.md`
- `database/orm/method_surface_test.go` and `database/orm/testdata/public-methods.txt`
- `tests/fixtures/consumer/types_test.go` (seven owned entries)
- `tests/fixtures/goway/compiler_cases_test.go` (seven additions)
- `tests/fixtures/goway/footprint_test.go` (one new probe budget)
- `blueprint/goway-data-layer/compilefail-mapping.tsv` (seven owned rows)

## Docs notes

Lead-owned changelog/blueprint: ordinary scalar selections now support GROUP BY,
HAVING, DISTINCT and both row-key/selected-key DISTINCT ON. Complete record and
set results support DISTINCT through read-only package functions. DISTINCT and
conditional-expression runtime consumers are ready for the PostgreSQL gate;
complete-record streaming and direct scalar compilation still await lane A.

## Remaining C packages after batch-7b

No remaining package can complete independently of the pending lane A APIs.
Retained legacy tests are acceptance requirements, not compatibility implementations.
The following anchors identify the assertions that prevent whole-package cutover
(all paths are under `tests/fixtures/consumer/`):

| Package | Pending owner/API | Retained assertion |
| --- | --- | --- |
| advancedqueries | lane A `EachRead` | `subqueries_postgres_test.go:178`: scalar streaming callback error |
| comparisonqueries | lane A `RelationFilter` | `subqueries_postgres_test.go:84`: subquery expression inside referral filter |
| ctes | lane A `CompileRead`, `EachRead` | `ctes_postgres_test.go:43,69`: scalar CTE SQL and streaming |
| recordqueries | lane A `EachRead` | `records_postgres_test.go:79`: complete-record callback error |
| setqueries | lane A `CompileRead`, `EachRead`, `RelationFilter` | `sets_postgres_test.go:178,194,203`: set SQL, related membership and streaming |
| recursivequeries | lane A `CompileRead`, `EachRead`, `RelationFilter` | `recursive_postgres_test.go:144,152,168,260`: set SQL, related membership and session-owned recursive streaming |
| distinctqueries | lane A `EachRead` | `distinct_postgres_test.go:191`: complete-record DISTINCT streaming |
| expressionqueries | lane A `CompileRead` | `expressions_test.go:18,30,35`: direct scalar conditional SQL/validation |
| framequeries | lane A `CompileRead` | `named_postgres_test.go:26`: named-window definition and parameter deduplication |
| scalarqueries | lane A `CompileRead` | `geographic_test.go:16`, `scalars_postgres_test.go:202`: geographic/scalar SQL validation |
| filterqueries | lane A `CompileRead` | `filters_postgres_test.go:152`: reject outer-only correlated aggregate FILTER before execution |
| keyqueries | lane A `CompileRead`; additional clause gaps below | `keys_test.go:26`: selected computed partition SQL |
| correlations | lane A `CompileRead` | `correlations_postgres_test.go:94,97`: invalid outer capture rejected before execution |
| jsonqueries | lane A `RelationFilter` | `composition_postgres_test.go:44–45`: JSON containment predicate on a related codec-backed key |

`keyqueries` additionally retains correlated scalar GROUP BY and selected-key
DISTINCT ON (`keys_postgres_test.go:106`), and value-set selected-key DISTINCT ON
(`keys_postgres_test.go:79`). Those adapters are absent in this base; the accepted
ordinary scalar/record/set DISTINCT functions do not cover these descriptors.
They remain explicit framework gaps for follow-up routing. No pending terminal or
relation-filter implementation was duplicated in this delta.

## Integration history

The earlier lane report used base `6d80111`, r7, and recorded 127 passing probes.
The rebase above used `fdaff938`, r8, and recorded 135. These are historical
lane measurements. Batch 7c integrated the read/filter foundations; batch 7d
adds the correlated/value-set key clauses. The pending-API inventory above
describes the earlier lane base, not the current integrated capability set.
