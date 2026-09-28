# Isolated PostgreSQL HTTP tests

T05 native verification passed. The [master](../../blueprint/00-master-architecture-and-parity.md#typed-api-delivery)
owns acceptance status. The [independent consumer](../../tests/fixtures/consumer/isolatedhttp/application.go)
uses ordinary application assembly, generated models, typed HTTP DTOs and a real
PostgreSQL transaction per write. Handlers contain no test schema argument.

The low-level alias `pgtest` imports `testkit/postgres`; `pgapp` imports
`testkit/postgres/application` for application settings/migrations.

```go
scope := pgtest.Isolate(t)
archive := pgtest.Isolate(t)
settings := Defaults() // your concrete production settings
env, err := pgapp.Bind(settings.Services.Database,
    pgapp.On(scope, "main", "reporting").WithReads(),
    pgapp.On(archive, "archive"),
)
if err != nil { t.Fatal(err) }
settings, err = settings.WithDatabaseScopes(env.Settings())
if err != nil { t.Fatal(err) }
app, err := Build(t.Context(), settings, onCommit)
if err != nil { t.Fatal(err) }
err = env.Start(t, app,
    infrastructure.MigrationTarget{Definitions: Migrations()},
    infrastructure.MigrationTarget{Connection: "archive", Definitions: Migrations()},
)
if err != nil { t.Fatal(err) }
db, err := app.Resources().Database()
if err != nil { t.Fatal(err) }
// Generated queries/factories now use db normally, including real commits.
```

Use `FOUNDRY_TEST_POSTGRES_URL` only for the explicitly opted-in test database.
Without it these helpers skip ordinary tests; the required acceptance mode fails.
Every configured named connection must appear exactly once in `Bind`. Names in
one `pgapp.On` share a physical schema, while separate `Isolate` calls create
distinct namespaces. The default still aliases its selected named pool.

`Bind` replaces deployment endpoints/credentials with the opted-in test endpoint
and limits each pool to two connections. It preserves the global ceiling and
rejects a selection that exceeds it. Original read routing is disabled unless
`WithReads` explicitly creates a second scoped pool on that same test database.
This tests routing behavior; it does not simulate asynchronous replica lag.

`WithDatabaseScopes` uses the same persistence targets as feature migration
grouping. Auth, audit, outbox, notifications, extensions, reports and PostgreSQL
cache stores follow their selected connection. Blank/`public` feature schemas
mean the conventional default and are replaced; another explicit schema must
already match the selected scope. Runtime handlers and consumer config types
remain unchanged. Disabled features are not enabled by the helper.

`Environment.Start` registers application cleanup before setup. It combines the
application's framework migrations with explicit domain targets, validates them,
and runs one ordinary migration registry/history per retained namespace before
starting providers. Identical contributions from intentionally shared connections
deduplicate; conflicting migration definitions fail. Domain SQL must use ordinary
unqualified application tables or explicitly select the owned schema. No helper
rewrites SQL, creates a database/server, or wraps the whole test in rollback.

For the production HTTP kernel, run `app.Run(ctx, foundation.HTTP)`, obtain its
address with `app.HTTPReady(ctx)`, and use `testkit/http.Connect(t, "http://"+address)`.
The [complete integration test](../../tests/fixtures/consumer/isolatedhttp/isolation_test.go)
shows client, kernel and application ownership. Register clients after the app
so client cleanup runs first. The application drains handlers/tasks before pools.
`testkit/http.New` remains available when a handler-owned test server is enough.

## Pool scope and retained data

The typed adapter `postgres.Config.Schema` selects one validated existing schema.
New and replacement connections establish it before application access. Each
checkout restores it and UTC, verifies schema existence/USAGE, and discards a
connection if restoration fails. Sessions that created temporary tables are also
discarded on reuse to prevent table shadowing. A missing scoped table cannot fall
back to `public`; a missing/inaccessible schema fails startup. An empty Schema
preserves the existing unscoped adapter behavior.

Only the application schema is explicitly listed; PostgreSQL's implicit catalog
resolution remains available. See the [PostgreSQL schema rules](https://www.postgresql.org/docs/18/ddl-schemas.html#DDL-SCHEMAS-PATH).
This adds a bounded driver round trip per checkout. It does not reset arbitrary
session state or provide a security sandbox for fully qualified SQL/shared roles.
Use `database.Session.Discard` for custom session state such as advisory locks;
do not mutate the pool's schema from domain code. Transactions/session scopes
retain their selected connection until their callback exits.

Schemas, committed rows and migration history are **retained**. Teardown closes
resources only: no DROP, TRUNCATE, database reset, or persistent-data deletion.
The helper logs namespace identifiers without credentials. Retained schemas
accumulate; manage their long-term retention explicitly outside test teardown.
Temporary PostgreSQL session objects naturally expire when their session closes.

Cache/Redis/queue namespaces, local storage directories and external services
need their existing independent test configuration when enabled; schema isolation
does not automatically isolate those resources. Set a distinct application
namespace and use test-owned directories/backends. Tests requiring database-wide
extensions or settings remain separate, explicitly configured integration tests.


## Acceptance evidence

The [recorded native checks](../evidence/typed-api-t05.json) include full verification,
PostgreSQL/race integration, typed compiler failures and real gopls completion.
Three local SELECT 1 samples had median checkout times of 25.5 microseconds unscoped
and 75.1 microseconds scoped, with 32 and 57 allocations respectively. The extra
round trip applies once per checkout, not to each statement within one transaction.
These are local microbenchmarks, not whole-application throughput promises.
The evidence also records actual concurrent application setup/migration times
and observed pool use. The later [T07 integrated audit](typed-api-audit.md)
records acceptance of the complete typed API workflow.
