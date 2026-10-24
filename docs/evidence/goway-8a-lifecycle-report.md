# Stage 8a — Lane B lifecycle and pruning

Implemented on `f3cfffd` plus the accepted 6e4 delta. No commit or git write. The four rebase conflicts were resolved by union; the regenerated surface snapshot passed. The segmented 6e4 rebase gate passed before Stage 8 began.

## API and owners

- `database.FoundryObservers(any, sqlowner.ObserverSeal)` is the validated concrete-owner lookup. `query.FoundryObservers` remains a compatibility bridge; unknown wrappers cannot prove absence or receive a capability. No ORM surface additions.
- `querycore` owns `MaxPageSize`, `MaxChunkSize`, `DefaultChunkSize`, `MaxInsertRows` and `MaxPerModelWriteRows`. Existing public constants remain aliases with unchanged values.
- Mutation kinds/assignments already belong to `querycore`; lifecycle operations, field changes and observers to `lifecycle`; nesting/redaction to `lifecore`. These shared contracts have no legacy query dependency. Legacy typed draft/hook adapters remain available.
- Prune registry bounds now use ORM. `ORMModel` and `ORMSoftModel` are the normal registrations; `Model` keeps its original signature and behavior in `legacy.go`.
- Rows retain only pagination/explain dependencies, assigned to D/C. Relation, identity and extension extraction stays with A/D.

## Acceptance

All commands below passed with `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r5 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`. PostgreSQL sources were compiled, not executed.

| Command | Result |
| --- | --- |
| `go -C tests/fixtures/consumer test -race ./hookqueries -run ^TestGeneratedAfterCommitNeverFormatsLocalOrObserverErrors$` | PASS |
| `go -C tests/fixtures/consumer vet ./hookqueries` | PASS |
| `go build ./...` | PASS |
| `go vet ./database/... ./internal/...` | PASS |
| `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/... ./database ./database/postgres ./database/prune ./database/lifecycle ./internal/lifecore -skip Postgres` | PASS |
| `go -C tests/fixtures/goway vet ./...` | PASS |
| `go -C tests/fixtures/goway test -timeout=20m -v .` | PASS |
| `go -C tests/fixtures/goway test ./pgsmoke -run ^$` | PASS |
| `python3 tools/goway_catalogue.py` | PASS |
| `make generate-check` | PASS |
| `make docs-check` | PASS |
| gofmt; git diff --check | PASS |

737 compiler cases and 115 footprint probes passed. No catalogue rows changed; all 6e ports are retained. New regressions cover observer ownership, bounded registry accounting, old/new registry SQL/lifecycle parity, redacted hook errors, and PostgreSQL rollback/mass behavior.

## Footprint and surface

All 114 existing function counts are unchanged from the verified rebase. No ceiling increased. Full before/after counts and source hashes are in [the evidence](goway-8a-lifecycle.json).

| Probe | Before | After | Ceiling |
| --- | ---: | ---: | ---: |
| aggregate_families | 4366 | 4366 | 4369 |
| prune_registry | new | 933 | 1073 |
| q_with | 1211 | 1211 | 1211 |
| terminal_explain | 1322 | 1322 | 1516 |
| terminal_prune | 930 | 930 | 1070 |

The new registry ceiling is `ceil(933 × 1.15) = 1,073`. No Query, Table or column methods changed. The sole Stage 8 public addition is the framework-only database observer bridge.

## Shared files

18 existing files changed after the verified 6e4 boundary (plus 9 new files listed in the evidence):

- `database/orm/README.md`
- `database/orm/chunk.go`
- `database/orm/hooks.go`
- `database/orm/insert_each.go`
- `database/orm/observer_capability_test.go`
- `database/orm/query.go`
- `database/orm/relation_write.go`
- `database/orm/source_write.go`
- `database/orm/write.go`
- `database/orm/write_each.go`
- `database/prune/prune.go`
- `database/query/chunk.go`
- `database/query/compile_insert.go`
- `database/query/model_hooks.go`
- `database/query/pagination.go`
- `database/query/write_each.go`
- `tests/fixtures/goway/footprint_test.go`
- `tests/fixtures/goway/terminal_writes_golden_test.go`

`database/orm/relation_write.go` changes only the limit alias/import. `database/query/pagination.go` changes only the page-limit alias/import. These are the minimal A/D coordination points.

## Docs notes

- Register ORM pruning with `prune.ORMModel` or `prune.ORMSoftModel(..., Users.Soft, ...)`. Lifecycle mode runs each deletion lifecycle; Mass explicitly skips hooks/observers/audit. Filters, scopes and visibility remain intact.
- Each batch retains the existing atomic bound. Failed or unknown outcomes are errors, never confirmed removals or automatic retries; earlier committed batches remain reported.
- Generated-query `prune.Model` and query compatibility constants/observer bridge remain until coordinated legacy retirement. Its compatibility file is the only prune import of query.
- No persisted schema, manifests, OpenAPI, TypeScript or dependency change. Lead-owned documentation and main were untouched.
