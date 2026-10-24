# Stage 6e inventory and slices

Base: 80546f4, with accepted B5d changes retained. All 57 Stage 6e catalogue rows
are assigned below. No new Query/Table methods or capability families are needed.

The old upsert boundaries are `database/query/execute_insert.go:15` and
`database/query/execute_insert.go:48`: one statement, optional/skipped results,
no per-model callbacks. `internal/generate/emit_model_mutation.go:10` supplies the
typed drafts and result wrappers. `database/query/conflict.go:61` and
`database/query/conflict_target.go:9` declare conflict targets;
`database/query/conflict_rows.go:73` types stored/proposed calculations.
The shared compiler is `internal/querycore/mutation_core.go:145`.

Insert-select (`database/query/insert_select.go:30`) and source writes
(`database/query/source_write_execution.go:62`) use bounded set-based execution,
with declared mapping capabilities and literal transformations. Per-model batches
(`database/query/write_each.go:32`) run ordinary lifecycle operations in one
transaction. Lookup writes live in `database/query/write_lookup.go`. Pruning
(`database/query/prune_batch.go:17`) distinguishes ordinary lifecycle and mass
physical removal; `database/query/explain.go:211` requires a transaction for locked
plans. ORM increment/decrement already enforce D12 positive finite magnitudes,
`AtLeast`/`AtMost` atomic guards (`database/orm/adjustment.go:35`), and reject
mutator bypasses (`database/orm/write.go:100`). These need parity coverage, not a
second numeric API.

| Slice | Features and catalogue rows | PostgreSQL coverage for lead | Footprint / shared files |
| --- | --- | --- | --- |
| 6e1 (accepted) | Column/constraint targets; DO NOTHING; literal, NULL and own-EXCLUDED updates; optional/batch upserts. Nine rows: conflict_conversion, conflict_target_owner, conflict_update_owner, conflict_condition_owner, encrypted_conflict_update, mutation_input_conflict, upsert_draft_owner, upsert_policy_owner, upsert_result_owner. | Defaults/NULL, conditional skips, repeated target atomicity, enclosing rollback, tenant-suppressed batch rollback. | New conflict probe; keep read probes unchanged. ORM write pipeline, shared querycore conflict/mutator owner and old query callers; surface, compiler registry, mapping and footprint files. |
| 6e2 | Calculated/partial index targets; stored/proposed row scope; typed calculations, conditions and scalar subqueries. Remaining 16 conflict/index/mutation_input_scope rows. | Partial/expression index inference, proposed defaults, NULL calculations, correlated conditions and tenant checks. | Dedicated expression probe; expr/rows bridges, shared querycore; same catalogue/surface registries. Coordinate Lane C ownership. |
| 6e3 | Insert-select and source update/delete: eight insert_select_* and twelve source_* rows. | Mapping completeness, mutator restrictions, timestamps, source/destination scopes, ambiguous matches and rollback. | Source-write probe; ORM package functions and minimal rows bridges, shared compiler and registries. |
| 6e4 | Per-model batches and lookup writes (five each_* and five lookup_* rows), prune, explain (two locked_* rows); D12 adjustment parity. | Lifecycle ordering, lock bounds, lookup races, per-model vs mass prune, guarded updates and transaction-only explain/analyze. | Separate terminal probes; lifecycle/write owners and query plan decoder, registries. |

Each slice: gofmt changed Go sources; `go build ./...`;
`go vet ./database/... ./internal/...`; race tests on touched packages plus
`./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/...`;
`go -C tests/fixtures/goway vet ./...` and `go -C tests/fixtures/goway test -v .`
(compiler, golden and footprint); compile pgsmoke with `-run '^$'`;
`python3 tools/goway_catalogue.py`; `make docs-check`. All Go/make commands use
`GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.
PostgreSQL sources are compiled/vetted only. Existing ceilings stay fixed;
q_with and aggregate_families require particular attention.
