# Stage 9 lane C: remaining result clauses

Base `6d80111`, on top of the accepted cumulative Wave 1/A1/source-clauses delta.
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
allowlist additions. The cumulative snapshot also includes the six previously
accepted source-clause functions.

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

Native Go 1.27.1 darwin/arm64, r7 cache, GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off.
New `result_clauses` probe exercises all eight functions and measures **2,188 T/t
functions**, ceiling **2,517 = ceil(2,188 × 1.15)**. All 127 probes pass; existing
ceilings are unchanged. `q_with` remains **1,211/1,211**, `aggregate_families`
**4,366/4,369**, and the accepted `source_clauses` probe **2,505/2,881**.
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
