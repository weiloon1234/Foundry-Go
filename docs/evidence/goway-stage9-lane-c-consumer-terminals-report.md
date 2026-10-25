# Stage 9 lane C: consumer terminal migrations

Base: `68885959`. No framework, generated, module, shared registry or footprint
files changed. Existing public DTO/model/key identities and assertions remain.

## Completed

`advancedqueries`, `comparisonqueries`, `ctes`, `recordqueries`,
`recursivequeries`, `distinctqueries`, `framequeries`, `expressionqueries`, and
`filterqueries` now use ORM declarations and rows/expr composition for their
runtime scenarios. The integrated `correlations` migration was audited unchanged.
Scalar, set and complete-record compilation/streaming use `CompileRead`,
`PreflightRead` and `EachRead`; relation predicates use `RelationFilter`.

Local numeric/window/natural-key models have handwritten declarations with typed
primary roles: `scalarqueries.Samples`, `framequeries.Samples` and
`recursivequeries.Nodes`. The DTO schemas are `scalarqueries.Calculations` and
`keyqueries.BucketCounts`; JSON payload steps are declared in `jsonqueries/tables.go`. JSON traversal uses startup-bound pointer field declarations and
typed map/index steps. SQL and bindings for named frames, recursive complete
records, JSON paths and calculation projections are compared with the retained
legacy API. Existing parity oracles remain intentional legacy imports.

The test sources retain all original test entry points and named assertion
messages, including streaming cleanup, cancellation, malformed hydration,
cardinality errors, NULL behavior, grouped/window clauses and transaction use.
This source audit supplements the tests; it does not establish live SQL results.

## Remaining framework blockers

| Package / retained assertion | Exact shared owner needed |
| --- | --- |
| `scalarqueries.TestPostgresScalarProjectionAndDerivedAggregation`, `setqueries.checkDerivedSetAggregation`: SUM of a derived `rows.Field`, including nullable decimal output | Lane C, `orm/expr` aggregate input and `orm/rows` field adapters. `expr.SumAt` accepts ordered ORM columns; `rows.At` returns a row expression, with no equivalent aggregate input. |
| `scalarqueries.TestPostgresComputedRelationOrder` and `TestPostgresComputedManyToManyOrdering`: calculated target/pivot sort | Lane A, relation ordering in `database/orm`; a validated expression-order adapter is missing. `HasMany.OrderBy`, `ThroughRelation.OrderBy` and `OrderByPivot` accept `orm.Order`, not `expr.Order`. |
| `keyqueries.testComputedCursorKey`: reject a computed cursor key during compilation | Lane A, read terminals / result cursor boundary. `CursorQuery` has no compile/preflight entry point and does not satisfy `ReadSource`; using pagination instead would change the assertion boundary. |
| `jsonqueries.checkSnapshotKind`: map JSON Kind into the unchanged `Snapshot` DTO | Lane C, shared JSON result type ownership. `query.JSONKind` and `expr.JSONKind` are distinct named types; the retained DTO uses the former. A shared owner/compatibility alias is required before cutover. |

These assertions remain executable legacy tests in `*_pending_test.go`, with
owner TODOs. Independent numeric, text, window, set, correlation, JSON predicate,
path, write and hydration scenarios in those packages are migrated. The new
`Calculation` rows schema is exercised by a SQL parity test while its derived SUM
consumer remains blocked. No raw SQL or runtime-only substitute was added.

## Acceptance and footprint

PASS: targeted build, vet, ordinary tests, races, all 373 owned compiler negatives,
phase-conversion guard, catalogue, docs-check, gofmt and diff whitespace checks.
See the companion JSON for commands, outcomes and source hashes. PostgreSQL is
compiled only; the lead owns live execution. The initial build selection included
test-only directories; the corrected build targets declarations, while test/vet
compile all 14 scenario families. Compiler negatives were already ported in the
accepted work; this delta adds no catalogue rows or ORM surface entries. Existing
footprint probes and ceilings are unchanged and are not remeasured here.

## Docs notes

The Stage 9 status can mark the completed packages above migrated, and the four
partially migrated packages blocked only on the listed assertions. Generated APIs
remain retained alongside declarations until the later retirement stage. No
changelog or lead-owned blueprint was edited.
