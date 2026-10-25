# Stage 9: public predicate retention redaction (review 19)

Base `6d80111`; earlier accepted D8/D1/D2 and AddFiles/articles changes are
preserved. No commit.

## Fix and sweep

The shared AST now stores comparison/membership values, row-comparison keys,
scalar parameters and schema literals, window fallback values and offsets behind
pointers to private, methodless types. Raw fragments likewise retain their SQL
and arguments behind private pointer state. SQL validation and compilation unwrap
these values only inside the shared owner. Nil, empty and SQL NULL semantics are
preserved; codecs, captured values and binding order are unchanged.

This places the formatting boundary below adapters that copy or unwrap AST nodes.
`expr.ModelPredicate`/`FoundryPredicate`, legacy `NewGlobalScope`, and both
`JoinOn.WhereLeft`/`WhereRight` therefore retain protected literals without adding
another generic wrapper or formatter. The sweep covered:

| Public retention family | Shared retained contents |
| --- | --- |
| ORM predicates, queries, locked queries, lookups, relation filters and write guards | Comparison, parameter and nested SELECT expressions |
| expr predicates, row/selected values, orders, grouping keys, comparison adapters and HAVING | Comparison and value-expression nodes |
| CASE, aggregate filters, JSON/scalar operations, raw fragments and windows | The same expression nodes, raw state, parameters, fallback and offset holders |
| rows mappings/projections, aliases/joins, records, scalar/subquery/correlated sources, CTEs/sets and transaction variants | Source, record, correlation and join-condition ASTs |
| Legacy predicates/HAVING, fixed scopes, join conditions, projections, value/annotated queries, aliases, correlation, CTEs/sets and transaction variants | The same shared querycore expression nodes |

No public method, signature or capability changed. No catalogue row or footprint
ceiling changed. `projection_bridge.go` is byte-identical to the previous accepted
evidence; this increment makes no edit to lane A's concurrent RelationFilter work.

## Regressions

74 public-API sentinel cases exercise direct values, pointers, concrete private
fields, nested private fields and interface wrappers under `%v`, `%+v`, `%#v`,
and error interpolation. Coverage includes the reported model-predicate bridge,
fixed global scope and left/right join predicates independently, plus the
retention families above. Separate shared-owner regressions cover every literal
leaf, raw SQL/arguments and zero storage; numeric window offsets have public
regressions too. An exact SQL/argument assertion checks the model-predicate
projection adapter. Existing SQL goldens, ownership, mutation, copy-isolation and
binding tests remain the behavior checks.

## Acceptance

Build, targeted vet and the ORM/query/core race suite passed. Fixture vet,
bounded-matching goldens, all 129 footprint probes, catalogue, docs-check,
gofmt and diff-check passed. Every footprint count exactly matches the previous
measurement: `q_with` remains 1,211/1,211 and `aggregate_families` 4,366/4,369.

Acceptance results and exact commands are recorded in
[the evidence JSON](goway-stage9-retention-redaction.json). All Go commands use
`GOCACHE=/private/tmp/foundry-goway-codex-gocache-r8 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.
Live PostgreSQL and integration-wide generation/TypeScript/editor gates remain
lead-owned; this change adds no generated or wire contract.

## Shared files

The evidence lists every changed source with its hash. Changes are limited to
`internal/querycore` literal storage, its validators/compiler and construction
sites in `database/orm`, `database/orm/expr`, `database/query` and
`internal/ormcore/json_predicate.go`, plus regression sources. No `rows` production
file, public-method snapshot, catalogue registry, fixture budget or lead-owned
document was edited in this increment.

## Docs notes

Predicate and expression diagnostic formatting hides retained literals through
public scope, join, projection, subquery and window adapters, including when those
handles appear inside private wrapper fields. No new API or migration step.
