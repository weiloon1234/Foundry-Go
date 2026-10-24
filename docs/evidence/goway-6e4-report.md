# Stage 6e4 report

Implemented on top of the accepted declaration-role slice; no commit or git-writing command.

## API and surface

- orm: `CreateEach`, `CreateOrFirst`, `FirstOrCreate`, `UpdateOrCreate`, `PruneBatch`, `ForcePruneBatch`, `Explain`, `ExplainAnalyze`, `ExplainLocked`, `ExplainAnalyzeLocked`.
- rows: `Explain`, `ExplainAnalyze`, `ExplainLocked`, `ExplainAnalyzeLocked`, `ExplainTransaction`, `ExplainAnalyzeTransaction`, `ExplainTransactionValue`, `ExplainAnalyzeTransactionValue`, `ExplainCursor`, `ExplainAnalyzeCursor`, `ExplainValueCursor`, `ExplainAnalyzeValueCursor`.
- prune: `ORMModel`, `ORMSoftModel`.
- query_framework_bridge: `FoundryExplain`.
- aliases: `orm.Plan`, `orm.PlanNode`.

Numeric key adapters retain reads/ordering/identity assignment and source-insert roles, but lose `Increment`/`Decrement`. Ordinary numeric adjustment methods remain unchanged. No Query/Table/column method names were added. The regenerated surface snapshot adds 59 lines, removes 12 forbidden numeric-key method entries, and replaces the `NumberKey` return signature.

## Rows

Eight newly ported: `each_create_draft`, `lookup_create_draft`, `lookup_result`, `lookup_update_create_draft`, `lookup_update_draft`, `lookup_update_model`, `locked_explain_executor`, `locked_analyze_executor`.

The four remaining planned rows were already ported: `each_delete_result`, `each_update_draft`, `each_update_model`, `each_write_limit`. All 57 Stage 6e rows are now ported. Added 17 external negatives in this slice; all 659 fixture negatives pass.

## Footprint

All 106 probes pass. Existing ceilings are unchanged. Archive T/t symbol counts use the same toolchain; the new probe ceiling is ceil(measured × 1.15).

| Probe | Before review | After | Ceiling |
| --- | ---: | ---: | ---: |
| aggregate_families | 4366 | 4366 | 4369 |
| q_with | 1211 | 1211 | 1211 |
| terminal_create_or_first | new | 880 | 1012 |

Full slice comparisons and source hashes are in [the evidence JSON](goway-6e4.json).

## Acceptance

`GOCACHE=/private/tmp/foundry-goway-codex-gocache-r4 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

| Command | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./database/... ./internal/...` | PASS |
| `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/... ./database/prune -skip Postgres` | PASS |
| `go -C tests/fixtures/goway vet ./...` | PASS |
| `go -C tests/fixtures/goway test -timeout=20m -v .` | PASS |
| `go -C tests/fixtures/goway test ./pgsmoke -run ^$` | PASS |
| `python3 tools/goway_catalogue.py` | PASS |
| `make generate-check` | PASS |
| `make docs-check` | PASS |
| `gofmt -l changed Go files; git diff --check` | PASS |

PostgreSQL was compiled only. Initial failures and their corrections are recorded in the evidence.

## Review fixes

- CreateOrFirst is a typed package function. Both query.InsertOrFirst and ORM share ormcore.InsertFailure/InsertRows/OwnInsertConflict: statement-only provenance, concrete child TxRolledBack before recovery, bounded full error-graph uncertainty checks, own-table unique-index lookup, no retries and no retrieval hooks. Outer unknown commits retain a typed reconciliation candidate.
- Explain compilation, execution and decode failures use lifecore.Failure inside the shared isolated callback. Old query and ORM/rows regressions assert redacted Error/%v/%+v/%#v and errors.Is/As identity; compilation failures send no SQL.
- UpdateOrCreate and UpdateEach draft errors use lifecore.Failure before isolation. The old shared modelUpdateMutation and new atomicModels owner also redact before callback formatting. Prune/CreateEach reuse atomicModels; explain variants reuse the shared explain owner.

## Shared files

Existing shared files changed in this slice:

- `blueprint/goway-data-layer/compilefail-mapping.tsv`
- `database/orm/README.md`
- `database/orm/column.go`
- `database/orm/column_role.go`
- `database/orm/fix_a_test.go`
- `database/orm/method_surface_test.go`
- `database/orm/orm_test.go`
- `database/orm/testdata/public-methods.txt`
- `database/orm/write.go`
- `database/orm/write_each.go`
- `database/query/execute_mutation.go`
- `database/query/explain.go`
- `database/query/mutation.go`
- `database/query/write_callback.go`
- `database/query/write_lookup.go`
- `internal/ormcore/result_cursor.go`
- `internal/querycore/mutation_core.go`
- `tests/fixtures/goway/compiler_cases_test.go`
- `tests/fixtures/goway/footprint_test.go`
- `tests/fixtures/goway/golden_driver_test.go`

New files, including `database/orm/create_or_first.go` and `internal/ormcore/insert_lookup.go`, are listed in the evidence. Lead-owned overviews remain untouched.

## Docs notes

- CreateEach runs one ordinary creation lifecycle per input in an atomic bounded batch. FirstOrCreate/UpdateOrCreate lock the first primary-ordered match, default omitted stored inputs from unambiguous equality/scopes, skip retrieval, verify created membership, and rely on unique constraints without retry. Mutators need explicit input.
- CreateOrFirst inserts in a child savepoint before lookup. Only its own table-index unique violation after confirmed savepoint rollback can return a matching competitor. Hook/observer/codec failures and uncertainty are returned without retry; snapshot isolation may hide the competitor. Internal hydration skips Retrieved.
- PruneBatch physically prunes ordinary models; ForcePruneBatch requires the exact Users.Soft token. Both preserve filters/scopes/visibility and lock bounded candidates. mass=false runs per-model lifecycle; mass=true explicitly skips hooks/observers/audit. Register with prune.ORMModel/ORMSoftModel.
- Explain plans; ExplainAnalyze executes. Locked/transaction variants require *database.Tx. Projections/scalars/sets use existing rows.Read* adapters; cursor variants retain canonical ordering without a page boundary. Formatting is redacted; explicit plan JSON/Root snapshots can expose server predicates.
- Ordinary Number and NullableNumber Increment/Decrement retain D12 positive finite magnitudes and AtMost/AtLeast guards. NumberPrimary, NumberLookupKey and NumberKey results no longer expose Increment/Decrement. Stored reads, ordering and primary insert mappings remain available. Managed timestamps and Input* fields cannot be numeric adjustment targets.
- No persisted schema, manifest, OpenAPI, TypeScript or dependency change. No new Query/Table/column method names. Shared lead-owned documentation was not edited.

## Second re-check

Both remaining findings are fixed. See [the second re-check report](goway-6e4-recheck.md) for current acceptance, exact main-port files, and API/docs notes. All existing footprint counts and ceilings are unchanged. The evidence JSON contains the refreshed source hashes.
