# Stage 9 lane C: scalar and correlated source clauses

Base: `6d80111`, on top of the accepted Wave 1/A1/review-26 delta. No commit.

## API and ownership

Six new `rows` package functions; no Query/Table methods, new descriptor types,
seals, or convertibility allowlist entries:

- `OrderScalar[S,V](Scalar[S,V], ...expr.ProjectionOrder[S]) Scalar[S,V]`
- `LimitScalar[S,V](Scalar[S,V], int) Scalar[S,V]`
- `OffsetScalar[S,V](Scalar[S,V], int) Scalar[S,V]`
- `HavingCorrelatedRecord[O,I,R](CorrelatedRecord[O,I,R], ...expr.HavingPredicate[Correlation[O,I]]) CorrelatedRecord[O,I,R]`
- `DistinctCorrelatedRecord[O,I,R](CorrelatedRecord[O,I,R]) CorrelatedRecord[O,I,R]`
- `DistinctOnCorrelatedScalar[O,I,V](CorrelatedScalar[O,I,V], ...expr.GroupKey[Correlation[O,I]]) CorrelatedScalar[O,I,V]`

Scalar clauses retain the inner SELECT/CTE boundary, codec and errors. Correlated
clauses reuse the existing HAVING/DISTINCT owners; parent scope and result types
remain intact. Existing provenance checks reject foreign orders even through an
embedded order wrapper. Bounds, empty keys, ordering prefixes and invalid
predicates fail closed. All six additions are pinned in the surface snapshot;
there are no removals.

## Consumers and rows

`lateralqueries` is migrated: handwritten `UserOrders`/`RankedOrders` schemas keep
DTO identities and required/nullable fields. Retained assertions cover per-parent
limits/offsets, nullable and nested lateral joins, aggregates/windows, CTEs, sets,
pagination, locks, cancellation and preflight errors.

`comparisonqueries` now uses ORM scalar subqueries for its scalar-discovery test,
including CTE ordering/limits, empty-result NULL and cardinality errors.
`distinctqueries` now has an ORM correlated DISTINCT ON PostgreSQL test.
Both packages remain partially migrated; legacy assertions remain in place.
Three public old/new SQL-and-argument comparisons pass for these new clauses.

Ported 15 lateral compiler cases (listed in the evidence JSON). Added five
Go-way negatives for scalar order ownership, correlated HAVING owner/phase and
correlated DISTINCT key owner/phase. No unrelated catalogue rows changed.

## Footprint and acceptance

Native Go 1.27.1 darwin/arm64, r7 cache, GOMAXPROCS=4, GOFLAGS=-p=4,
GOWORK=off. New `source_clauses` probe: **2,505 T/t functions**, ceiling
**2,881 = ceil(2,505 × 1.15)**, covering all six functions and their composed
sources. All 126 probes pass with existing ceilings unchanged: `q_with`
**1,211/1,211**, `aggregate_families` **4,366/4,369**.

Exact commands, final results and source hashes are in the companion JSON.
PostgreSQL execution remains lead-owned; local commands explicitly unset its
URL/required flags. Generated outputs, directives, module files and Makefile are
unchanged.

## Remaining routing

Lane A retains read-family compile/preflight/stream terminals and expression
predicates in relation filters; no competing terminals were added. TODOs preserve
blocked tests. Completing DISTINCT consumers also requires scalar
GROUP BY/HAVING/DISTINCT/DISTINCT ON and ordinary record/set DISTINCT adapters;
the lead was asked to route this remaining clause work.

Of the previous 15 blocked packages, lateralqueries is now migrated. The other
14 remain partial or blocked: advancedqueries, comparisonqueries, ctes,
recordqueries, setqueries, recursivequeries, distinctqueries, framequeries,
expressionqueries, scalarqueries, filterqueries, keyqueries, correlations and
jsonqueries. Accepted earlier consumer migrations remain intact.

## Shared files touched in this increment

- `database/orm/method_surface_test.go`
- `database/orm/testdata/public-methods.txt`
- `database/orm/rows/README.md`
- `tests/fixtures/consumer/types_test.go` (owned lateral entries only)
- `tests/fixtures/goway/compiler_cases_test.go` (five additions)
- `tests/fixtures/goway/footprint_test.go` (one new budget)
- `blueprint/goway-data-layer/compilefail-mapping.tsv` (15 owned lateral rows)

## Docs notes

For lead-owned changelog/blueprint: ordinary scalar SELECTs can retain ordering
and windows when used as subqueries, including CTE aliases. Correlated records
support HAVING/DISTINCT and correlated scalar SELECTs support DISTINCT ON through
thin rows functions. Lateral consumer migration and its 15 negative examples are
ready for integration; PostgreSQL execution is still required in the lead gate.
