# Stage 9: iteration identity correction and expression relation filters

On laneA9 at `6d80111`, above the saved remainder and read terminals. No commit.

## Changes

The shared `ormcore.Iteration` owner captures a consumer's returned error inside
callback isolation and returns it unchanged after successful completion. It does
not pass that error through isolation's formatting path. Regressions cover both
`Error` and `Format` methods calling Goexit across legacy and ORM complete-record
iteration, CTE/set/recursive reads, pool/session release, batch factory counts and
stop identity. Actual callback panic/Goexit still uses callback isolation.

The additive `expr.RelationFilter(relation, predicate)` returns a target-owned
`orm.Predicate` for the existing relation `.Where` methods. Its bridge,
`orm.FoundryRelationFilter`, requires a validated metadata capability. The shared
relation owner validates actual source/target types, the target declaration and
expression references, also serving `expr.Has`. No Query/Table methods changed.
Public surface additions: `expr.RelationFilter`, `orm.FoundryRelationFilter`.

Old/new SQL tests cover belongs-to, has-one, has-many-through, many-to-many and
correlated EXISTS under through-relation aliases. Invalid/foreign declarations,
nil descriptors and a borrowed target marker fail closed. Three new compiler
negatives pin target, source and WHERE/HAVING phase typing; no catalogue rows are
ported by these new regressions.

## Review 24 correction

The shared `relationPredicatePlan` validator now returns stored construction
errors before adapting any predicate. Direct and through regressions use a slot
and local key from different declarations of the same model, then move the
adapted predicate into a valid target query and a valid relation. Compilation,
Check and execution must reject it before SQL. Has, DoesntHave and expr.Has
are covered too. No new surface or footprint ceiling changes.

Sweep: ORM existence already retained plan.err; both expression bridges now
share the early rejection. Legacy direct/hop/morph EXISTS validates the relation
spec before lowering; through EXISTS validates metadata, preserving declaration
and binding errors. Aggregate relation validation recursively checks its retained
input plan. No additional error-dropping constructor was found.

Targeted acceptance for this correction: PASS on r8. ORM build/vet/races,
fixture vet/tests/races, JSON consumer vet/tests/races, all 134 footprint budgets,
catalogue, formatting, docs and diff checks passed. The initial regression
interface omitted CheckedTable; it was corrected and the three affected fixture
checks passed on rerun. No runtime correction was needed after verification.
Counts remain relation_filter 1,831/2,106, q_with 1,211/1,211 and
aggregate_families 4,366/4,369. No additional live PostgreSQL run.
Commands, initial failures, reruns and current source hashes are in the evidence JSON.

## Consumers

`jsonqueries` now publishes `DocumentTable`, `PolicyTable`, `Documents`,
`Policies`, `DocumentLinks` and the `Theme` JSON field declaration, and uses them
for the source/relation composition assertions and invalid-before-I/O checks.
The JSON natural key is bound once and reused by both relation and expression
operators. Model, key, payload and DTO identities, generated files and schema are
unchanged. SQL parity covers the JSON key filter using the legacy expression counterpart; the migrated PostgreSQL test
covers filtered eager loading, CTE scalar paths and correlated JSON results.

This is a partial migration. `query_postgres_test.go` still needs scalar
GroupBy/Having and Snapshot's retained `query.JSONKind` projection identity;
no substitute DTO or cast was added. The remaining legacy paths are retained.
`correlations` still lacks A1's `models`/`reports` ORM declarations on this base,
as well as scalar grouping/HAVING. Its through-relation expression blocker is
closed by the framework change, with an independent parity regression.

## Acceptance and footprint

Targeted acceptance is segmented across the user-requested cache rotation from
r7 to r8, with `GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`. The new independent
`relation_filter` probe measured 1,831 functions before rotation; its ceiling is
2,106. All 134 budgets passed. Existing counts remain q_with 1,211/1,211,
aggregate_families 4,366/4,369, read_scalar 1,165/1,340, read_record 1,002/1,153,
and read_set 1,050/1,208. Counts are T/t symbols in consumer package archives,
not incremental binary cost or runtime speed. Results and source hashes are recorded in
[the evidence JSON](goway-stage9-relation-filters.json). PostgreSQL sources are
compiled/vetted; live execution is lead-owned. Existing footprint ceilings stay
unchanged. The new probe ceiling is ceil(measured functions × 1.15).

Acceptance: PASS. Root build/vet/tests/races passed on r7 for database, ORM,
legacy query and ormcore. The corrected fixture parity/ownership tests and races
and JSON consumer vet/tests/races passed on r8. All six owned compiler cases,
134 footprint budgets, smoke-source compilation, catalogue, formatting, docs
and diff checks passed. The initial fixture result-dispatch error and consumer
alias call were corrected; only affected checks were repeated. No live
PostgreSQL acceptance is claimed.

Shared files: `internal/ormcore/record_iteration.go`,
`database/orm/projection_bridge.go`, `database/orm/relation_scope.go`,
`database/orm/relation_predicate.go`,
`database/orm/expr/filter.go`, `database/orm/method_surface_test.go`,
`database/orm/testdata/public-methods.txt`, `database/orm/rows/report_read_test.go`,
`tests/fixtures/goway/compiler_cases_test.go`, and
`tests/fixtures/goway/footprint_test.go`. New tests, consumer declarations and
probe files are listed in the evidence JSON. No module/generator files changed.

## Docs notes

Lead changelog note: bounded read iteration preserves returned error identity
without formatting consumer errors. Expression predicates can constrain existing
relation descriptors with `expr.RelationFilter`, retaining typed target ownership
and correlated scopes for eager loading and EXISTS. Construction errors remain
attached when an adapted filter is moved to another query or relation.

Requests: integrate A1 before correlations/A2 migration; route scalar grouping and
the retained JSONKind DTO projection identity to their feature owners.
