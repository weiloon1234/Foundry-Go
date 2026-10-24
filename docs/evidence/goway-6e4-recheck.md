# 6e4 second re-check

Both findings fixed at shared owners. No commit, git write, generator change or generated-output change.

## Fixes

- PostgreSQL classification retains schema and table alongside the constraint. Recovery requires all three, checks the resolved destination namespace/relation/index, and fails closed on missing or mismatched metadata. The cross-schema trigger regressions use identical table and index names.
- Each returned AfterCommit error receives the existing safe committed-outcome wrapper before isolation. Local hooks and observers retain errors.Is/As identity even when Error would panic or call Goexit; formatting is never invoked.

## Exact files for the main port

Production:

- `database/errors.go`
- `database/postgres/errors.go`
- `database/query/write_lookup.go`
- `database/transaction.go`

`database/query/write_lookup.go` is the main owner (`Query.ownUniqueViolation`); this branch implements the equivalent identity check in `internal/ormcore/insert_lookup.go`. Do not transplant the program-only owner onto main.

Regression files:

- `database/postgres/config_test.go`
- `database/pool_test.go`
- `database/transaction_test.go`
- `database/query/insert_lookup_postgres_test.go` (new)
- `tests/fixtures/consumer/hookqueries/after_commit_error_test.go` (new)

Program-only changes additionally cover `database/orm/lookup_review_test.go`, `database/orm/README.md`, `tests/fixtures/goway/terminal_writes_golden_test.go`, and `tests/fixtures/goway/pgsmoke/terminal_writes_test.go`.

## Acceptance

`GOCACHE=/private/tmp/foundry-goway-codex-gocache-r4 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

| Command | Result |
| --- | --- |
| `go -C tests/fixtures/consumer test -race ./hookqueries -run ^TestGeneratedAfterCommitNeverFormatsLocalOrObserverErrors$` | PASS |
| `go -C tests/fixtures/consumer vet ./hookqueries` | PASS |
| `go build ./...` | PASS |
| `go vet ./database/... ./internal/...` | PASS |
| `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/... ./database ./database/postgres ./database/prune -skip Postgres` | PASS |
| `go -C tests/fixtures/goway vet ./...` | PASS |
| `go -C tests/fixtures/goway test -timeout=20m -v .` | PASS |
| `go -C tests/fixtures/goway test ./pgsmoke -run ^$` | PASS |
| `python3 tools/goway_catalogue.py` | PASS |
| `make generate-check` | PASS |
| `make docs-check` | PASS |
| `gofmt -l changed Go files; git diff --check` | PASS |

659 compiler cases and 106 footprint probes pass. All previous symbol counts and ceilings are unchanged: q_with 1,211/1,211; aggregate_families 4,366/4,369; terminal_create_or_first 880/1,012. PostgreSQL sources compile only. All 57 Stage 6e rows remain ported.

## Docs notes

- Added `database.Detail.Schema` and `.Table` and `(*database.Error).Schema()` / `.Table()`. No Query/Table/column method additions. No schema or wire change.
- Conflict recovery requires complete server relation identity. Missing metadata preserves the original unique violation.
- AfterCommit errors expose the original cause through Unwrap without invoking its formatting, and always retain the committed outcome.

Current source hashes and all acceptance results: [evidence](goway-6e4.json).
