# T05 — Isolated PostgreSQL tests for complete HTTP applications

Prerequisites: T01–T04. Status belongs to the
[master](../00-master-architecture-and-parity.md#typed-api-delivery).

## Problem and consumer outcome

[Namespace](../../testkit/postgres/postgres.go) creates a unique schema, but does not
configure every pooled connection to use it. Tests such as the
[bootstrap consumer](../../tests/fixtures/consumer/bootstrap/auth.go) manually use
schema-setting transactions, while features receive individual schema settings.
Add a test-owned scope that runs production application assembly against isolated
tables, with ordinary handler pool queries and real transaction commits.

The consumer flow is: allocate an isolated test scope, apply explicit application
and framework migrations, build/start the app from its scoped settings, create
typed fixtures, send HTTP through the existing client, assert with the same scoped
database and finish. Domain handlers contain no test-only SQL or schema parameter.
No enclosing rollback transaction may hide real commits or after-commit behavior.

## Isolation and pool contract

Default to a fresh schema per test within the already configured test database.
Provide an owned scope/config result in `testkit/postgres`; reuse namespace creation,
application startup, factories, migration runner and HTTP client. Reuse typed
PostgreSQL configuration for a validated schema setting; do not expose an arbitrary
runtime-parameter map. Every connection opened by the pool, including replacements,
must apply the scope before application statements. A one-time `SET` through the
pool or `SET LOCAL` in a setup transaction is insufficient.

Search paths contain only the owned test schema and trusted system catalog behavior,
with no fallback to public application tables. Validate/quote identifiers at the
adapter boundary. Migrations must create application objects in the test schema,
not the catalog. Statement caches and reconnects must not retain another scope.
Pool scope is immutable; separate tests do not mutate/reconfigure a shared pool.
Verify the owned schema exists before scoped application access and fail startup if
it is absent; an ignored missing schema must never leave the catalog as a writable
application default. Establish scope on connection creation and define checkout/
reset behavior so accidental session state cannot leak into the next borrower.

Schema isolation protects ordinary test data from accidental overlap; it is not a
security sandbox for arbitrary fully qualified SQL or shared database roles.
PostgreSQL documents both [name resolution and schema privileges](https://www.postgresql.org/docs/18/ddl-schemas.html#DDL-SCHEMAS-PATH).
Tests requiring database-wide extensions/settings remain explicit integration tests
outside this helper, without automatic database/server creation.

## Complete application integration

Derive scoped copies of selected named connection settings, preserving default-name
aliasing and consumer configuration types. Configure every application-owned pool
used by the test, including direct/read routes. Disable replica routing explicitly
or require each selected endpoint to target the opted-in test database with the
same isolated schema; never route a test to an unscoped replica silently.

Use the existing migration-target grouping and feature declarations to set auth,
audit, outbox, notification and other persistence schemas consistently. Avoid a
second handwritten list of infrastructure feature names. Detect incompatible
explicit schema overrides before starting the app. Multiple logical connections
are intentional: settings must state which share one physical schema and which
need distinct namespaces, while a default aliases its selected named connection.

The test scope owns migration preparation, pools/application lifetime and bounded
cleanup. Stop HTTP, workers and other users before closing pools. Ordinary I/O and
real callbacks/outbox publishers use that same scope; shared caches, queue backends
and storage use existing independent namespaces/test directories when enabled.

Schemas and data are retained for inspection, matching repository safety policy.
No `DROP`, `TRUNCATE`, database reset, clone overwrite or cleanup deletion is part
of test teardown. Report retained namespace identifiers without credentials and
document accumulation. Lifecycle cleanup releases processes/connections only.

## Acceptance

- Run two complete apps concurrently with identical business keys and migration
  names. Their HTTP writes/reads, fixtures and assertions must never cross schemas.
- Ordinary generated queries through the configured pool work without `withinSchema`
  wrappers; exercise multiple acquired connections and a forced reconnect.
- Missing test tables fail rather than resolving similarly named public tables.
- Real commit, rollback, after-commit and outbox behavior are visible through HTTP;
  callback assertions happen before teardown rather than being suppressed by it.
- Named/default pools, persistence feature migrations, invalid overrides and selected
  read routing are covered. Existing explicit schema/migration APIs keep working.
- Startup failure and cancellation release acquired resources; retained test data
  remains available. No credentials enter diagnostics or generated files.
- Native integration/races, public consumer/gopls and relevant compiler checks pass.
  Measure migration/setup overhead and bounded parallel pool usage; keep ordinary
  tests cheap and integration explicitly opted in.

Complete the common milestone gate. T06 uses this real-commit harness for concurrent
claims, replay, failure injection and outbox coupling.

## Concrete delivered implementation

The delivered API uses `postgres.Config.Schema`, `pgtest.Isolate`, `pgapp.On`,
`Selection.WithReads`, `pgapp.Bind`, `Environment.Settings/Migrate/Start` and
`application.Settings.WithDatabaseScopes`. A selected test endpoint has two pool
connections; each explicit read endpoint adds two. Blank/public feature schemas
follow the selected connection; conflicting custom schemas are rejected.
Application persistence declarations are shared by validation, schema mapping
and migration grouping. Temporary-table sessions are discarded on reuse; other
custom session state uses the existing explicit `Session.Discard` boundary.
See the [consumer guide](../../docs/guides/isolated-http-tests.md) for ownership,
retention and complete production HTTP kernel usage. Native verification passed; see the master and
[T05 evidence](../../docs/evidence/typed-api-t05.json).

Application helpers live in `testkit/postgres/application`; lightweight schema
creation stays in `testkit/postgres` without an infrastructure dependency.
