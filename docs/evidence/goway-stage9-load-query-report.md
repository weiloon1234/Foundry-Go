# Stage 9: relation limits when loading existing models

Added `orm.LoadFrom(ctx, executor, query, parents)` and `orm.LoadMissingFrom`.
They take the query returned by WithRelationLimits and load its With relations
onto copies of supplied models. Parent predicates, ordering and window are
validated but do not fetch, filter or reorder the supplied collection, matching
legacy Query.Load/LoadMissing. Invalid/zero queries and foreign declarations fail
before SQL. No Query/Table methods or capability bridges were added.

All four package loading functions share loadExistingPlans and the existing tree
validation, limit defaults, fetched-row/attachment budgets, depth validation,
relation batching and hydration. Existing signatures remain unchanged. Two
package-function entries are added to the public surface snapshot.

Regression sources cover custom batch sizes, tree-wide budget overflow without
partial results or input mutation, depth rejection before SQL, preservation of
loaded slots, cancellation and declaration ownership. The SQL golden compares
both terminals against legacy loading, including parent clauses, nested relations,
loaded-slot skipping and exactly four child statements across two loads. Two new
compile-fail cases retain model-owner and unlocked-query restrictions. PostgreSQL
smoke coverage is compiled only, for the lead to execute.

New footprint probe: load_query, covering both terminals with a two-level tree.
Measured 1,265 T/t functions; ceiling ceil(1265 × 1.15) = 1,455. This is an
importer archive footprint, not a runtime performance measurement. All 140 footprint budgets passed. q_with remains 1,211/1,211 and
aggregate_families remains 4,366/4,369. Extracting the single shared load-existing
engine adds one reachable helper in extension_slots: 2,103 → 2,104, below its
unchanged 2,478 ceiling. All other existing probe counts match the preceding
accepted office measurement. No existing ceiling changed.

## Acceptance

On r10 with GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off:

- Build: database/orm/..., database/query/..., internal/querycore/... and internal/ormcore/... passed.
- Vet: the same packages passed after correcting the new test's iterator/slice
  accessor and rerunning database/orm. No runtime source correction was needed.
- Race tests: all four package trees passed, including the new limit/depth tests,
  the public surface snapshot and convertibility guard.
- Fixture vet (root and pgsmoke), legacy/new relation SQL goldens under race,
  the two new compiler cases and all footprint budgets passed.
- PostgreSQL smoke sources compiled under race with `-run ^$`; no live PostgreSQL.
- Catalogue, make fmt-check, make docs-check and git diff --check passed.

Exact commands, outcomes and source hashes are in
[the evidence JSON](goway-stage9-load-query.json). Logs are under
/private/tmp/goway-stage9-load-query.

Shared files: database/orm/relation_tree.go, database/orm/method_surface_test.go,
database/orm/testdata/public-methods.txt, tests/fixtures/goway/compiler_cases_test.go,
tests/fixtures/goway/footprint_test.go. No historical catalogue row is relabelled;
the new negatives supplement the existing relation restrictions.

## Docs notes

Lane D can replace legacy `q.WithRelationLimits(limits).With(...).Load(ctx,db,parents)`
with `orm.LoadFrom(ctx,db,orm.WithRelationLimits(q,limits).With(...),parents)`, and
use LoadMissingFrom for missing-only loads. No parent SQL is issued. Parent model
provenance and authorization still belong to the caller. The three blocked legacy
relation test files can migrate once this delta lands.

The two nestedbindings negatives remain waiting on lane D's published declarations
to enter this worktree at the next integration. This slice does not edit them.
No commit.
