# Stage 9 lane C: derived aggregates, JSON identity and record DISTINCT ON

Implemented on 68885959 above the accepted consumer-terminals delta. No commits or git writes. No generated outputs, DTO declarations, module files or lead-owned documents changed.

## API and ownership

- Added `expr.SumRow`/`AvgRow`, `SumFloatRow`/`AvgFloatRow`, `SumIntervalRow`/`AvgIntervalRow`, `MinRow`/`MaxRow`, each with a `NullableRow` input variant, plus `CountRow` and `CountDistinctRow`. These take concrete scoped `RowExpression` values from `rows.At`. Existing two-argument `*At(scope, column)` APIs remain unchanged; Go cannot overload them.
- Ordered aggregate permission comes from the actual declared result field. Enum declarations do not issue it; promoted wrappers cannot satisfy the concrete input type. The shared aggregate compiler still rejects parameters/computed expressions, preserves ownership errors and checks filters. Exact sums/averages yield nullable decimal; floats yield nullable float64; intervals retain interval; min/max preserve the input codec with one nullable layer; counts yield int64. Nullable min/max fallback parameters remain present values.
- `database.JSONKind` owns the type, six constants and validation. Both `query.JSONKind` and `expr.JSONKind` are aliases. `querycore` delegates validation. Snapshot's declaration, generated source, SQL, bindings and JSON bytes remain unchanged.
- Added `rows.DistinctOnRecord(record, keys...).OrderBy(...)`. Keys and orders retain the joined input scope, including unselected fields. It delegates DISTINCT ON validation and keeps complete results read-only.

The public snapshot has **19 function additions, zero removals**: the eighteen aggregate functions above and `DistinctOnRecord`. No Query/Table/column methods were added. The convertibility guard passes; the now-identical JSONKind pair was removed from its exception list, with no new exception.

## Consumers and regressions

Migrated the derived CTE aggregate in scalarqueries, derived UNION ALL aggregation in setqueries, and Snapshot projection in jsonqueries. Added SQL/binding parity for required/nullable exact aggregates, filtered aggregates, min/max, counts, set aggregation, Snapshot and joined complete-record DISTINCT ON. Tests cover nullable codecs, enum metadata, foreign declarations/filters, zero inputs and invalid ordering prefixes.

Authorized lane B edits:

- `pagequeries/legacy_clauses_postgres_test.go`: joined winner pagination in `TestPostgresPagesRetainRelationalSemantics`.
- `cursorqueries/legacy_clauses_postgres_test.go`: joined complete-model cursor traversal in `TestPostgresGroupedAndJoinedCursors`.

Six new compile-fail cases cover selected phase, nullable input, foreign scope, promoted aggregate wrappers, foreign DISTINCT ON scope and selected-phase DISTINCT ON keys. No catalogue rows were reclassified.

## Footprint

Go 1.27.1 darwin/arm64, archive T/t symbol counts using the existing harness. All existing budgets pass and remain unchanged.

| Probe | Measured | Ceiling |
| --- | ---: | ---: |
| derived_aggregates (new) | 2,807 | 3,229 |
| record_distinct_on (new) | 2,072 | 2,383 |
| q_with | 1,211 | 1,211 |
| aggregate_families | 4,366 | 4,369 |

New ceilings are `ceil(measured × 1.15)`.

## Acceptance

All passed: targeted root/consumer build and vet; root/consumer tests and races; public surface and convertibility guards; 373 owned consumer compiler cases and six new cases; full fixture footprint sweep; fresh JSON generation regression; catalogue; docs-check; gofmt and `git diff --check`. PostgreSQL sources compile; live PostgreSQL remains the lead's gate. Commands, logs and source hashes are in the accompanying evidence JSON. Build/test runs completed on r10 before rotation; final checks use r11. No interrupted checks remained.

## Shared files touched

`internal/ormcore/source.go`; `internal/querycore/json_path.go`; `database/orm/expr/value.go` and `json_expression.go`; `database/query/json_expression.go`; `database/orm/rows/constructors.go`, `declaration.go`, `record_clauses.go` and `README.md`; `database/orm/method_surface_test.go`, `testdata/public-methods.txt` and `transaction_phase_test.go`; `tests/fixtures/goway/compiler_cases_test.go` and `footprint_test.go`; the two lane B files listed above. New owners are `database/json_kind.go` and `database/orm/expr/aggregate_rows.go`.

## Remaining blockers

Lane A owns computed target/pivot relation ordering (`scalarqueries/relation_order_pending_test.go`) and cursor compile/preflight (`keyqueries/cursor_compile_pending_test.go`). setqueries and jsonqueries are migrated; scalarqueries' derived aggregate is unblocked, with its two relation-order assertions still retained for lane A.

## Docs notes

For the lead's changelog: derived row declarations now support typed aggregates, both query APIs share JSONKind, and complete-record DISTINCT ON retains input-scoped winner ordering. Consumers can preserve existing DTOs while using rows schemas and read-only terminals.
