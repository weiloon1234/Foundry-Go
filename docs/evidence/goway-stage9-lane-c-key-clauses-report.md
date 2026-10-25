# Stage 9 lane C: remaining computed-key clauses

Base `fdaff938`, on top of the accepted result-clause delta. No commit.

## API

Four thin package functions, with no new types or Query/Table methods:

- `GroupByCorrelatedScalar(q, ...expr.GroupKey[rows.Correlation[O,I]])`
- `DistinctOnCorrelatedScalarValues(q, ...expr.ProjectionKey[rows.Correlation[O,I]])`
- `OrderCorrelatedScalar(q, ...expr.ProjectionOrder[rows.Correlation[O,I]])`
- `DistinctOnValueSet(q, ...expr.ProjectionKey[rows.Set[V]])`

The ordering function lets aggregate/window selected keys participate in the
required DISTINCT ON ordering prefix. Existing row-only ordering methods are
unchanged. Grouping/distinct delegate to the shared rows clause owner; ordering
reuses `recordOrders`. Value-set selection reuses the scalar constructor to retain
the explicit `AS "value"` alias and original result codec. Correlation ownership,
inner windows, CTEs, immutable inputs and nullable results are preserved.

All input descriptors and keys are concrete framework types. No new capabilities,
app-callable bridges, promoted markers or convertibility allowlist entries are
introduced. Retrieved ordering metadata still undergoes shared ownership checks.

## Coverage and consumers

Ten new external compiler negatives reject wrong group/selected phase, unlifted
owners, mismatched set value types, foreign ordering scopes and embedded input
wrappers. No existing catalogue row is reassigned or changed by this increment.

Runtime tests reject foreign declarations, foreign parameter exemplars, zero
keys/sources/orders, empty DISTINCT ON, invalid grouping and ordering prefixes,
and attaching the correlated result to another parent declaration. Value-set tests
verify nullable decoding and rejection before executor I/O.

Five old/new SQL-and-binding parity cases cover computed row/aggregate/window
correlated keys and nullable/window value-set keys, with CTEs and offset/limit.
`keyqueries.TestPostgresORMComputedKeyClauses` ports the two previously blocked
computed-key result assertions into ORM coverage. Live PostgreSQL remains lead-owned.
The package's direct scalar compilation assertions still need lane A's terminals;
its handwritten migration is not claimed complete yet.

## Acceptance and footprint

Targeted acceptance uses r8, GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off.
The new `key_clauses` probe exercises all four functions, selected ordering and
window keys: **2,674 T/t symbols**, ceiling **3,076 = ceil(2,674 × 1.15)**.
All **136** probes pass. Existing ceilings remain unchanged: `q_with` is
**1,211/1,211**, `aggregate_families` **4,366/4,369**, and `result_clauses`
**2,188/2,517**. Build/vet, rows and consumer tests/races, surface/R9/convertibility
guards, ten compiler negatives, fixture vet, PostgreSQL source compilation,
catalogue, docs-check, gofmt and diff checks pass. Live PostgreSQL was not run.
The companion JSON records exact commands, results and source hashes.

## Shared files

- `database/orm/rows/correlated_clauses.go`, `record_clauses.go`, `README.md`
- `database/orm/method_surface_test.go`, `testdata/public-methods.txt`
- `tests/fixtures/goway/compiler_cases_test.go`, `footprint_test.go`

## Docs notes

The remaining computed-key clause gaps are closed through package functions.
Correlated scalar GROUP BY and selected-key DISTINCT ON retain enclosing ownership;
selected ordering supports aggregate/window keys. Value sets support selected-key
DISTINCT ON while retaining their output scope and nullable codecs. Consumer
completion still waits for integration of lane A's read terminals and RelationFilter.
