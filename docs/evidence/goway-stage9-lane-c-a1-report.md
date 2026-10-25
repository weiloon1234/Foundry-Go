# Stage 9 lane C: A1 and dependent consumers

Base: `6d80111`, on top of the accepted lane C Wave 1 delta. No commit.

## Ready for integration

- **A1:** `models` exports all ten ORM tables, six relation declaration sets and
  fourteen `UserValues` aggregate slots. `catalog.Products` and `ProductLinks.Creator`
  retain the external `catalog/status.Status` enum. Primary roles preserve concrete
  keys; these models have no managed timestamps, mutators or soft-delete roles.
- **Query fixture:** `internal/queryfixture.Run` and `RunJoins` keep their signatures,
  isolated schemas and seeded values, using ORM assignments and inserts. Legacy
  drafts remain available to consumers pending the lead's generation barrier.
- **Reports:** all ten original result identities have handwritten rows schemas;
  `reports/README.md` lists the exports. No DTO fields, package paths, directives,
  generated output, enum generation or migrations changed.
- **Migrated runtime consumers:** `chunkqueries`, `temporalqueries`, `windowqueries`,
  `projectionqueries`, `joinqueries`, `joinqueries/inner`, `joinqueries/natural`,
  `joinqueries/outer`, `correlations/nullable`, and `lockqueries`. Together with
  `reports`, this completes eleven more C packages. Prior Wave 1's `reporting`,
  `intervalqueries` and `transactionqueries` remain intact.

These declarations remove the A1 dependency for lane A's `relationshipfilters`,
`background` and `tooling`; lane B's `upsertqueries`; and lane D's root consumer,
`cursorqueries`, identity/HTTP consumers and client-contract work. Each lane still
owns its remaining local prerequisites. Reports and reporting rows exports are
available now. Retained legacy relation builders and `CurrentEmailDraft` are part
of the expressly approved temporary coexistence, not the new call path.

## Assertions and compiler rows

SQL parity covers all ten model tables, seven User relation-existence paths,
catalog enum/creator filtering, user-summary and buyer-total projections, temporal
extraction/grouping and ranking windows. New PostgreSQL relation/aggregate parity
compares loaded models against the legacy path. Migrated consumers retain original
persisted values, callback and release behavior, cancellation, empty/NULL results,
join scopes, recursive windows, lock strengths/targets/policies and savepoint
assertions. The lead must run those PostgreSQL tests live.

**104 consumer negatives ported this turn:** 18 A1 relation/aggregate/pivot/lookup,
14 temporal, 4 chunk, 20 join, 10 window, 11 projection, 18 lock, and 9 basic
numeric/enum/predicate/order/page cases. Exact IDs and diagnostics are in the JSON
and existing registry. All 373 currently owned cases pass; catalogue dispositions
retain their existing Go-way equivalents (534 ported, 48 pending, 444 unaffected).
A passing retained legacy negative is not reported as a consumer migration.

## Remaining blockers and requests to the feature owner

All paths below are under `tests/fixtures/consumer`. These fifteen packages are
unchanged. Add the missing typed adapters at the shared owner before porting;
no surrogate DTO, raw SQL or runtime-only replacement was introduced.

| Packages | Missing ORM operation and retained assertion |
| --- | --- |
| `framequeries` | Compile selected scalar: `named_postgres_test.go:26` checks named-window definition and parameter deduplication. |
| `expressionqueries` | Compile computed scalar selection: `expressions_test.go:17` checks SQL expression/parameter composition. |
| `scalarqueries` | Compile computed scalar: `geographic_test.go:16`, `scalars_postgres_test.go:202` validate expressions before execution. |
| `filterqueries` | Compile invalid correlated scalar: `filters_postgres_test.go:152` must reject outer-only aggregate scope. |
| `keyqueries` | Scalar clause composition/compilation and cursor preflight: `keys_test.go:26,50`; invalid cursor key must fail before I/O. |
| `comparisonqueries` | Order/limit scalar selected from CTE alias, usable as a scalar subquery: `subqueries_postgres_test.go:32`. Moving clauses to a model source cannot preserve this boundary. |
| `correlations` | Scalar grouping/HAVING/compile and expression predicates in relation filters: `correlations_postgres_test.go:91–97` preserves grouped correlation and rejects outer-only aggregation. |
| `advancedqueries` | Scalar streaming: `subqueries_postgres_test.go:178` preserves callback error and connection release. |
| `ctes` | Scalar compile/stream: `ctes_postgres_test.go:43,69` checks definition discovery and early release. |
| `recordqueries` | Complete-record streaming: `records_postgres_test.go:79` preserves callback/connection ownership. |
| `setqueries` | Set compile/stream: `sets_postgres_test.go:178,203` checks set SQL and early release. |
| `recursivequeries` | Set compile and complete-record stream: `recursive_postgres_test.go:144,168,260`, including session ownership. |
| `distinctqueries` | Record streaming and ordinary correlated scalar DISTINCT ON: `distinct_postgres_test.go:58,160`. |
| `lateralqueries` | Ordinary correlated record HAVING/DISTINCT: `composition_postgres_test.go:40,69` preserves aggregate filtering and derived-record selection. |
| `jsonqueries` | JSON natural-primary-key containment usable in a relation predicate: `composition_postgres_test.go:44`; the same key field cannot be bound again as a JSON column. |

`rows.Compile` and `rows.Each` currently take only `rows.Query`; `rows.SelectValue`
only accepts a direct ORM column/model query. `ReadScalar`/`ReadRecord`/`ReadSet`
provide terminals but cannot preserve these inspection, streaming or composable
scalar boundaries. Ordinary correlated records/scalars have no group/HAVING/
distinct adapters. `expr.JSONContains` produces an expression predicate while
relation `Where` takes an ORM predicate. Please route these gaps to their feature
owners. `nested_locked_source` remains dependent on lane D's nested declaration.

## Acceptance and footprint

Environment: r6 cache, `GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`. PostgreSQL environment
variables were explicitly unset for local tests. Commands/logs are in the JSON.

- Build: PASS for packages with production source; test-only packages compile in
  test/race/vet. The first build invocation included test-only packages and reported
  “no non-test Go files”; the corrected production-source build passed.
- Vet: PASS for all changed packages.
- Targeted tests and race tests: PASS; PostgreSQL tests compiled and skipped locally.
- Owned compiler cases: PASS (373, then nine changed basic cases rechecked).
- Catalogue, docs-check, gofmt and `git diff --check`: PASS.

No framework API/method-surface additions, budgets or probe sources changed.
The new consumer declarations introduce generic instantiations; their incremental
binary cost has not been measured. Lane D owns the four consumer footprint probes
and must measure them at integration; no existing ceiling was raised. `q_with`
is untouched.

Shared files touched: `blueprint/goway-data-layer/compilefail-mapping.tsv` and own
entries in `tests/fixtures/consumer/types_test.go`. Generated files, ownership
manifests, module files, Makefile and migrations are unchanged.

## Docs notes

Lead changelog/blueprint note: shared consumer models, catalog and report rows now
publish handwritten ORM declarations without changing model, key, enum or DTO
identities. ORM queryfixture seeding and eleven dependent query packages are ready;
legacy generation cleanup and live PostgreSQL/contract/editor gates remain at the
integration barrier. Fifteen C packages require the typed operations listed above.
