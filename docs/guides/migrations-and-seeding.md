# Migrations and seeding

Foundry provides immutable migration definitions, history inspection, an explicit PostgreSQL runner, transactional seeders, consumer command adapters, and development scaffolding. Verification combines driver protocol fixtures, independent consumer modules, and real PostgreSQL acceptance through the [PostgreSQL adapter](postgresql.md).

## Historical definitions

Handwritten Go models describe current application types. A `migrate.Definition` records a historical schema change: a typed origin and ID, the release that introduced it, ordered SQL statements, and typed prerequisites. The [consumer migration package](../../tests/fixtures/consumer/migrations/migrations.go) and its [test](../../tests/fixtures/consumer/migrations_test.go) demonstrate public usage.

```go
const (
    Origin migrate.Origin = "app"
    CreateRecords migrate.ID = "20260911000000_create_records"
    Introduced migrate.Version = "v0.1.0"
)

registry, err := migrate.New(migrate.Definition{
    Key: migrate.Key{Origin: Origin, ID: CreateRecords},
    Version: Introduced,
    SQL: []string{"CREATE TABLE records (id bigint PRIMARY KEY)"},
})
```

Use imports from `github.com/weiloon1234/Foundry-Go/database/migrate`. Semantic names use lowercase letters, digits, underscores, hyphens, and dots; they start with a letter or digit and contain at most 128 bytes. Define constants once and reuse them. Origins separate framework, plugin, and application history. The introducing version is immutable metadata, not the currently installed release version.

`migrate.New` rejects duplicate identities, invalid SQL text, missing dependencies, repeated/self dependencies, and cycles before database I/O. Independent definitions sort by ID then origin; declared dependencies execute first. Input slices and returned metadata are copied. The SHA-256 checksum covers versioned canonical metadata, SQL in execution order, and sorted prerequisites. Even SQL whitespace changes the checksum. Keep applied definitions unchanged and add a new migration for a later change.

By default, each statement must be valid inside a PostgreSQL transaction. Use separate SQL entries for separate statements. Do not include `BEGIN`, `COMMIT`, `ROLLBACK`, or manual savepoint commands, and use explicit `Mode: migrate.NonTransactional` for operations requiring execution outside a transaction. Foundry owns the transaction and its history insertion. SQL content is an explicit reviewed boundary; Go types cannot prove SQL semantics.

## History and explicit execution

`registry.Inspect(history)` returns copied status entries and all detected problems. It validates record shape, checksum encoding, positive batch numbers, and timestamps. Changed or missing definitions and unapplied dependencies block `Report.Check` and `registry.Pending`. A caller-owned report is informative; it is never authorization to skip the runner's locked history check.

```go
runner, err := migrate.NewPostgres(db, registry, migrate.DefaultPostgresConfig())
// Handle err before using runner.
report, err := runner.Status(ctx)
// Review report and handle err. Explicitly execute when intended:
result, err := runner.Up(ctx)
```

Constructing the runner performs no I/O. `Status` only reads committed history; an absent history table yields pending definitions without creating a table or taking a migration lock. `Up` acquires a session advisory lock, ensures its history namespace, rereads and validates history, then executes each pending definition according to its declared mode. The default commits SQL and history in one transaction. It never synchronizes schema during ordinary application boot.

All cooperating runners must use the same configured history schema and table. Defaults are `foundry_ops.schema_migrations`, a 30-second lock wait, 50-millisecond polling, five-second cleanup, and at most 10,000 history records. Identifiers are quoted and restricted to simple names of at most 63 ASCII bytes to prevent PostgreSQL name truncation. Reads request at most the configured history limit plus one row and fail on overflow. Before executing migration SQL, `Up` also rejects a pending set that would grow history beyond that limit. PostgreSQL's [identifier rules](https://www.postgresql.org/docs/18/sql-syntax-lexical.html#SQL-SYNTAX-IDENTIFIERS) and [session lock behavior](https://www.postgresql.org/docs/18/functions-admin.html#FUNCTIONS-ADVISORY-LOCKS) define these boundaries.

Concurrent runners serialize and reload history after acquiring the lock. Earlier migrations remain committed if a later one fails. `RunResult.Applied` lists confirmed commits; `Interrupted` identifies an attempted migration without a successful confirmation. With an unknown commit outcome, that attempt may have committed. A later explicit `Up` reconciles from history under the lock. A cleanup error can accompany fully applied results; inspect both result and error.

Failed lock cleanup discards the physical connection, releasing session-owned locks when the server session ends. Caller cancellation still governs the session lifetime; a new cleanup deadline cannot revive a canceled session. There is no reset, wipe, or destructive schema synchronization command. Test against an isolated project database and never reset existing data.

## Transactional seeders

`seed.New` validates named `seed.Definition` values and their dependencies. Each definition has a typed `seed.ID` and `Run func(context.Context, *database.Tx) error`. Use the supplied transaction for all work. The registry snapshots dependency lists and orders them deterministically before execution.

`registry.Run(ctx, db, selectedIDs...)` expands selected seeders to include prerequisites. Omitting selection runs the full registry. Invalid/duplicate selection fails before database I/O. Every seeder has a separate transaction and uses the normal [transaction lifecycle](database-runtime.md#transaction-scopes), including rollback on callback error/panic and after-commit behavior.

`seed.Result.Committed` lists confirmed commits. `StoppedAt` identifies the failing invocation and can also appear in `Committed` if only after-commit work failed. Earlier seeders remain committed. Foundry never retries arbitrary business callbacks or silently suppresses a later explicit invocation. Repeat execution and idempotence are the seeder's domain contract; neither is implemented by resetting tables. Shared callbacks must be safe if callers invoke a registry concurrently.

## Consumer commands

The public `database/command` package owns parsing and reporting for:

```text
migrate status [--format text|json]
migrate up [--format text|json]
seed list [--format text|json]
seed run [--format text|json] [--id app.regions --id app.roles]
```

The [consumer command example](../../tests/fixtures/consumer/database_command_test.go) compiles these APIs. Parse with `command.Parse(args, stderr)` before assembling services. Help returns `flag.ErrHelp`; invalid arguments fail before database use. Call the resulting invocation's `Run(ctx, resources, stdout)` with `command.Resources` containing the migration runner, seeder registry, and database required by that command. Resource ownership and shutdown stay with application assembly. `seed list` only needs the registry.

These commands run in the consumer's compiled binary, which owns its declarations and configuration. The separately installed `foundry` development tool owns generation/scaffolding and does not dynamically load an application registry. The later CLI kernel will host these same feature commands through shared bootstrap.

Status prints the report and returns a conflict error when history has drift. Migration and seeder execution print confirmed progress even if later work fails; the returned error preserves the database outcome. An output-writer failure can follow committed work and reports the confirmed count. Neither command retries an operation because output failed. JSON contains typed metadata and progress, never migration SQL, credential values, or unwrapped database causes. Commands propagate the caller's context.

## Development scaffolding

From a consumer workspace, select an existing Go package with current generated output:

```sh
foundry make migration --dir migrations --name AddRecordLabel \
  --id 20260911000001_add_record_label --origin app --version v0.1.0
foundry make seeder --dir seeders --name SeedRegions --id app.regions
```

Inside the framework repository, use `go run ./cmd/foundry make ...` with the same flags. These commands create one `add_record_label_migration.go` or `seed_regions_seeder.go` file. Names must be exported Go identifiers; identity fields follow the registry's semantic grammar. The explicit ID/version makes output repeatable and keeps historical metadata under review. No schema inspection or database connection occurs.

Scaffolds are ordinary Go functions returning typed definitions, with typed ID constants for dependencies and selection. Complete the SQL or seeder domain work, then explicitly register those definitions with `migrate.New` or `seed.New`. An unfinished migration fails registry validation; an unfinished seeder returns an error. There is no silent successful placeholder.

The generator checks the complete proposed package in memory before publication and refuses stale generated declarations, name conflicts, path traversal, symlink targets, and existing files. The current command requires an existing Go package; creating a new package directory is outside this scaffold slice. It uses the shared guards, source/module checks, staged writes, and recovery path. A new file is published without replacing a file created concurrently. Existing declarations and package imports are not rewritten.

Once created, a scaffold belongs to the consumer. Edit it normally; it has no generated-file marker and never enters `.foundry-gen.json`. `foundry generate` leaves the implementation alone. Interrupted creation uses a version 3 creation-only journal and the ordinary `foundry generate --recover --dir package` command. Recovery preserves a fully published scaffold, clears an unpublished creation, and refuses to overwrite subsequent edits. There is no `--force` overwrite option.

## Verification limits

Protocol tests exercise apply-once history, drift, bounded history reads/growth, concurrent runners, lock timeout, partial commits, lost commit responses, connection disposal, seeder dependencies, and after-commit outcomes. Command tests cover service-free help/listing, drift exit errors, typed selection, progress on failure, and writer failure after commit. Scaffold tests compile a separate consumer, verify that unfinished work fails, and cover deterministic output, ownership, name/file collisions, exclusive creation, and recovery. Consumer metadata tests inspect declarations without SQL. Real PostgreSQL tests additionally run competing migration runners, verify read-only status, reject checksum drift, recover after a failed pending migration, and execute an explicitly repeatable seeder twice. The [master roadmap](../../blueprint/00-master-architecture-and-parity.md) records verification evidence. Lost commit responses are tested through protocol fault injection, not a live network interruption.

## Concurrent indexes and nontransactional statements

```go
migrate.Definition{
    Key: migrate.Key{Origin: "app", ID: "202609240001_index_email"},
    Version: "v0.1.0",
    Mode: migrate.NonTransactional,
    SQL: []string{"CREATE INDEX CONCURRENTLY users_email_idx ON users (email)"},
}
```

Execution mode participates in the immutable checksum. The zero/default mode
keeps existing transactional checksums unchanged. Switching an already applied
migration's mode is definition drift; create a new migration instead.

Nontransactional SQL executes on the same locked session, outside a transaction.
Before each statement, Foundry durably records an in-flight checkpoint. After a
confirmed result, it records the number of completed statements. The final history
insert and removal of the progress row are atomic with each other; earlier SQL
has already committed independently. Progress lives in a separate bounded table
in the configured history schema, with a name derived from the history table.

`Status` includes `Progress` on an incomplete migration. A ready checkpoint can
resume its next unstarted statement. An in-flight or uncertain checkpoint blocks
`Up`, even after restart. Earlier effects are preserved and no uncertain SQL is
automatically replayed. A completed checkpoint can retry only final bookkeeping.
Definition drift/removal or missing prerequisite history also blocks an unfinished
migration and its reconciliation. Read a fresh `Status` after an interruption; the
error may carry an earlier snapshot if recording a checkpoint also failed. The
consumer command `migrate status --format json` exposes the full progress record.

After inspecting the real catalog/data, pass that exact progress snapshot to:

```go
ready, err := runner.Reconcile(ctx, inspectedProgress, migrate.StatementApplied)
// Or StatementNotApplied only after confirming that no durable effect remains.
// Handle err, then explicitly call runner.Up(ctx) to resume.
```

Reconciliation takes the same advisory lock, checks definition identity and
revision, and records your attestation without executing migration SQL. Stale
snapshots are rejected. Inspect again after an ambiguous reconciliation result.
A wrong attestation can repeat or omit effects; partial effects require operator
repair before choosing either outcome. Concurrent index failure can leave an
invalid index. Foundry never drops it or resets a database automatically.

Ordinary historical definitions remain transactional. Do not put explicit
transaction-control statements in either mode, and keep each nontransactional
entry to one reviewed SQL statement. Consumers own SQL semantics and deployment
coordination; typed declarations cannot prove a statement is safe to repeat.
