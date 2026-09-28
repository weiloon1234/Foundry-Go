# 04 — Database runtime and migrations

## Purpose and prerequisites

Prerequisites: [02](02-foundation-and-application-lifecycle.md), [03](03-generation-and-language-tooling.md). Establish reliable PostgreSQL execution and explicit schema evolution before model APIs.

Rust references: `src/database/runtime.rs`, `lifecycle.rs`, `scaffold.rs`, `src/foundation/app.rs`, `database/migrations`, `tests/database_acceptance.rs`, `database_lifecycle_acceptance.rs`.

## Boundaries and interfaces

`database` owns Foundry execution, transaction, result, and error contracts. The PostgreSQL driver adapts an approved upstream client; Foundry consumers do not need driver-specific row types for ordinary operations. Pool and transaction implement the same execution capability used by the query layer. Driver-specific operations are explicit escape hatches.

The implementation uses standard `database/sql` pooling and connector contracts underneath Foundry-owned database, row, and transaction APIs. The delivered `database/postgres` adapter uses [pgx v5.11.0](https://github.com/jackc/pgx/releases/tag/v5.11.0), verified as the latest upstream release on 2026-09-11 and installed after explicit user approval. Its `stdlib` adapter supports Go 1.27's direct PostgreSQL column scanning while preserving standard scalar and `sql.Scanner` behavior. Root `go.mod` owns the installed version.

Concrete runtime decisions:

- Explicit connector construction; no consumer blank import, process-global driver configuration, or automatically loaded credentials.
- Pool acquisition has a separate deadline from statement execution. Result streams retain their connection until closed; early-exit helpers close them on every path.
- Transactions receive their begin context for lifetime and do not automatically retry user callbacks. Failure or panic triggers rollback; commit transport failures retain an uncertain outcome instead of encouraging a blind retry.
- After-commit failures identify a committed transaction. Savepoint rollback discards callbacks registered inside that scope; successful savepoints defer their callbacks until the outer commit.
- Pool shutdown rejects new work and drains acquired resources. A caller deadline bounds waiting without falsely declaring leaked transactions or rows closed.
- Migration runners use an exclusive session advisory lock, deterministic IDs and checksums, and per-migration transactional history. CLI status is read-only; up is explicit. There is no reset/wipe command.

PostgreSQL verification uses the existing `foundry-go` VM and stable `foundry_go_test` project key provisioned through the shared-database broker. Credentials remain in ignored private configuration. Tests isolate their objects and never drop/truncate existing data.

The runtime is implemented in `database`, including prepared construction, lifecycle module integration, callback-scoped sessions, and uncertain-connection disposal. Its public contracts and limitations are in the [database runtime guide](../docs/guides/database-runtime.md). Immutable migration registries, history checks, the session-lock PostgreSQL runner, transactional seeders, consumer command adapters, and developer scaffolding are documented in the [migration guide](../docs/guides/migrations-and-seeding.md). The [PostgreSQL guide](../docs/guides/postgresql.md) documents explicit endpoint/TLS settings, configuration isolation, SQLSTATE classification and bounded driver resources. Standard-library protocol fixtures and real PostgreSQL tests verify complementary failure behavior.

The delivered command split keeps typed application registries in the consumer binary: `database/command.Parse` and `Command.Run` own `migrate status/up` and `seed list/run`. `cmd/foundry make migration/seeder` creates one consumer-owned declaration through the shared generator, checks its package before publication, and requires explicit registration. The CLI kernel's later command registry will reuse this feature behavior. Scaffolding currently selects an existing Go package; it does not create application structure.

Delivered transaction usage:

```go
err := db.Transaction(ctx, func(tx *database.Tx) error {
    // Every query in this unit receives tx explicitly.
    return nil
})
```

Use context cancellation/deadlines for execution and connection acquisition. Transactions have a defined terminal state and cannot be reused after commit/rollback. Do not hide transactions in process globals or automatically retry arbitrary callbacks with side effects.

## Implementation slices

1. Pool configuration, connect/readiness, acquisition deadlines, close/drain, parameter binding, row iteration and error classification.
2. Transactions, isolation, explicit savepoints, rollback on callback failure/panic, and after-commit registration. Never report a commit failure as a successful rollback without knowing its outcome.
3. Migration registry with stable IDs, deterministic ordering, checksums, application history, and a database lock preventing concurrent migration runners.
4. Explicit `migrate status`, `migrate up`, and migration/seeder scaffolding through the shared generator. Published framework migrations preserve origin and version metadata.
5. Seeders use normal transactions/model APIs when those exist. Idempotence is a seeder contract, not an excuse to reset tables.

Models describe current application types. Migrations preserve historical schema changes. A model edit never silently alters a live database. Generated migration suggestions require review and explicit application; they must not rewrite applied migration history.

## Failure behavior

Classify not-found separately from query failure. Preserve constraint identity and causes for unique, foreign-key, serialization and deadlock errors. Surface ambiguous commit outcomes so callers can reconcile with application idempotency keys. Close result streams on early exit and expose iteration errors.

Schema changes use ordinary explicit migrations. There is no `fresh`, reset, database-wipe, or automatic destructive synchronization command. Read `~/.local/share/sbx/SHARED_DATABASES.md` before database work and use a stable project-specific account and separate test key.

## Acceptance scenarios

Test pool exhaustion, canceled queries, unavailable database, unique/FK violations, transaction rollback, savepoint rollback, commit failure classification, after-commit suppression on rollback, row-stream cleanup, migration ordering/checksum mismatch, and concurrent migration runners. Use isolated records/schemas without wiping existing databases.

The consumer fixture must run a parameterized query and a transaction through public contracts. Apply the [common gate](README.md#common-completion-gate); PostgreSQL coverage is required, not replaced by an in-memory fake.

The delivered [consumer test](../tests/fixtures/consumer/postgres_test.go) also verifies typed application module registration. `make test-postgres` runs required real database acceptance with race detection in both modules, using the private test URL and retaining unique schemas. Live tests cover constraints, deferred commit rejection, serialization/deadlocks, streaming/cancellation, migration concurrency/drift/recovery and seeding. Protocol tests cover lost commit responses and cleanup faults; TLS configuration is unit-tested, with a deployed TLS server outside the local acceptance setup. See the [master evidence](00-master-architecture-and-parity.md) for the completed gate and consumer review.
