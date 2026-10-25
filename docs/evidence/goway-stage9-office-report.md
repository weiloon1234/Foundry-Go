# Stage 9 office aggregate reads

Base fdaff938 plus accepted A2 changes. This slice supplies the missing ad-hoc
relation aggregate read and migrates office; no commit.

## API and owners

- `expr.RelatedValue`: row-phase aggregate, with source/target declaration checks,
  existing codec and exact aggregate nullability. It composes through existing
  expression ordering and filtering.
- `rows.WithValue`: complete model plus computed value as
  `RecordRead[M, rows.Annotated[M,V]]`. No Query/Table methods or surrogate slots.
  Existing compile/preflight/read/stream terminals apply.
- Validated internal bridges: `orm.FoundryRelatedValue` (expression family) and
  `orm.FoundryModelRead` (metadata family). No valid capability enters app code.
- Legacy and ORM share `querycore.RelatedValueSelect`; relation inputs share the
  existing aggregate lowering. Complete model hydration, retrieval callbacks,
  scalar codecs, row partitioning and eager loading keep their existing owners.

Office's retained PostgreSQL assertions now use ORM reads, writes, relations,
aggregates and scopes. Generated declarations remain for the lead-owned cleanup
barrier. SQL parity covers intermediate/soft-delete relations, COUNT, nullable
SUM, filtering, ordering/window, filtered aggregates and morph-through inputs.

Three new negative cases: annotated_owner, annotated_selected, related_value_input.
No existing catalogue disposition is changed. nested_field_owner still waits for
lane D's nestedbindings declaration.

## Acceptance and footprint

New related_value probe: 1,757 functions, ceiling 2,021 = ceil(1,757 × 1.15).
It exercises complete-model annotation, relation loading, aggregate predicate/order,
compile inspection and execution; this counts archive functions, not runtime speed.
Existing ceilings are unchanged: q_with 1,211/1,211 and aggregate_families
4,366/4,369; all other existing probes passed. Factoring the scalar-record codec
setup by value type for reuse changes only these existing counts against the
post-rebase measurement: read_scalar +1 (1,166), result_cursor +1 (2,230),
result_reads +1 (1,386), transaction_joins +2 (1,837), value_sets +2 (2,239),
transaction_correlation −3 (2,905). Their ceilings are unchanged.

Final build/vet/races passed for database/orm/..., database/query/..., internal/querycore/...
and internal/ormcore/...; this includes B31 and the exported-type convertibility guard.
Office build/vet/tests/races and all five old/new SQL comparisons passed. The method
snapshot adds eight lines for the five entries above and removes none. The guard's
allowlist is unchanged. All three compiler cases, all footprint budgets, fixture vet/selected SQL regressions,
catalogue, formatting, docs and diff checks also passed. PostgreSQL smoke sources
passed vet and race compilation with `-run ^$`; no live database execution.

Commands use GOCACHE=/private/tmp/foundry-goway-codex-gocache-r9,
GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off. Exact commands and source hashes are
recorded in goway-stage9-office.json. Logs are under /private/tmp/goway-stage9-office.
The initial run identified only snapshot/budget setup, two parity-test API usages
and one expected compiler diagnostic spelling; final reruns passed after correction.
PostgreSQL assertions are compiled only; live execution remains lead-owned.

## Docs notes

Lead changelog: complete-model reads can select relation aggregates without model
slots and retain retrieval callbacks and eager loading; office is now migrated.

Shared files: database/orm/method_surface_test.go and testdata/public-methods.txt;
relation_aggregate.go and relation_aggregate_loading.go; rows/value_set.go and test
row-stream helper; database/query/relation_value.go; internal/querycore/relation_aggregate.go and record_core.go;
goway compiler/footprint registries. New public surface is exactly the five entries
listed above (including the Annotated result type).
