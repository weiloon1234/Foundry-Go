# Stage 9 lane C: review 26 and independent C3 contracts

No commit. This delta sits on the accepted Wave 1 and publish-ready A1 work.

## Review fix and sweep

`lock_clause_source` now calls `rows.AsRecord[alias, models.User]` and requires
`does not implement rows.RecordSource[models.User] (missing method recordData)`.
The explicit result parameter makes the diagnostic identify the missing ordinary
record capability rather than generic inference or the model-query adapter.

`lockqueries.TestUnlockedSourcesCanBeAliased` supplies the unlocked counterparts
for both `lock_clause_source` and `locked_subquery`. It aliases the same ordinary
record/model source and compiles a complete projection through the alias, without
requiring PostgreSQL. The initial test incorrectly assumed a zero-limit record
read would avoid executor use; it was corrected to test compilation directly.

Swept all 168 previously ported consumer negatives, focusing on model versus
record aliasing, CTE inputs, ordinary/transaction/correlated source boundaries,
projection mapping, and row/selected predicate/order adapters. No further
wrong-family port was found. Model-query `locked_subquery` appropriately retains
`rows.As`; transaction record aliases and CTE negatives use `AsRecord`/`CTE`.

## Independent C3 progress

Ported 17 additional consumer cases: `aggregate_enum_sum`, `aggregate_text_sum`,
`aggregate_temporal_avg`, the six `aggregate_filter_*` cases,
`aggregate_order_model`, `field_in_having`, and the six `having_*` cases.
Case IDs, result/nullability rules and existing Go-way catalogue equivalents are
preserved. `having_in_where` and `aggregate_order_model` use `expr.Where` and
`expr.OrderBy`, so their valid row-phase counterparts use the same API family.

`projectionqueries.TestFilteredAggregatePhaseSQLParity` compares SUM, COUNT and
EXISTS with same-owner FILTER predicates and combined HAVING against the legacy
SQL and arguments. It also compiles the corresponding row predicate/order adapters.

Temporal, interval, windows and projection positive consumers were already
migrated. No further positive package on C's roster is independent of the fifteen
recorded blockers. `filterqueries` remains on that list; it is not partially ported.
Lane A owns all read-family compile/preflight/stream terminals and the subsequent
scalar/correlated/relation adapters, per the latest lead decision. C retains all
fifteen consumer migrations and will resume them once those APIs land.

Correction to the prior blocker description: JSON primary-key expression access
is already available through a codec-backed key and `expr.Value`. `jsonqueries`
needs the expression-predicate-to-relation-filter adapter, not duplicate column
binding or a new JSON-key API.

## Acceptance

All Go commands use the r6 cache, `GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.
PostgreSQL environment variables are unset locally; the lead owns live execution.

| Check | Result |
| --- | --- |
| `go build ./models ./reports` (production dependencies of the test-only packages) | PASS |
| `go vet ./lockqueries ./projectionqueries` | PASS |
| Targeted tests, then corrected unlocked-source test | PASS |
| `go test -race ./lockqueries ./projectionqueries` | PASS; PostgreSQL tests compile and skip locally |
| All 373 owned compiler cases | PASS, including the exact missing `RecordSource` diagnostic |
| Catalogue, docs-check, gofmt and `git diff --check` | PASS |

No framework/public-method additions, generated output, footprint probes, ceilings,
model/DTO identities or migration changes. The consumer footprint integration
measurement remains with lane D. Shared edits are confined to own entries in
`types_test.go` and `compilefail-mapping.tsv`. Exact case lists, source hashes and
command logs are in `goway-stage9-lane-c-review26.json`.

## Docs notes

Lead-owned status note: A1's record-alias compiler regression is corrected with a
compiling ordinary counterpart; seventeen independent aggregate/FILTER/HAVING
consumer negatives are now migrated. Remaining positive migrations await lane A's
shared terminal and source-adapter work.
