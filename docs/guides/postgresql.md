# PostgreSQL

`database/postgres` adapts the approved pgx driver to Foundry's [database runtime](database-runtime.md). Consumers use Foundry rows, transactions and errors; they do not need a driver import or process-global registration. The driver version is pinned in root [go.mod](../../go.mod).

## Configuration and application ownership

Start with `postgres.DefaultConfig()`, then supply `Host`, `Database`, `User`, and `Password` (`secret.String`) from your typed application configuration. `Port` defaults to 5432. `postgres.New(config)` constructs an adapter without connecting, `postgres.Open(ctx, config)` opens and verifies a pool, and `postgres.Module(providerID, poolKey, config)` creates a fresh pool during each application's lifecycle. The [executable independent consumer](../../tests/fixtures/consumer/postgres_test.go) registers that module, resolves `foundation.Key[*database.DB]`, executes a bound query and runs a transaction with after-commit work.

`postgres.ParseURL(secret.New(value))` accepts an explicit `postgres://` or `postgresql://` URL with a user, TCP host and database. Credentials and database names are URL-decoded. An omitted port uses the default; an empty or invalid explicit port fails. Only `sslmode` and `application_name` query settings are accepted, once each. Configure other options through typed fields. Unknown settings fail rather than being ignored. Keyword DSNs, Unix sockets and multi-host failover are not exposed by this adapter.

Configuration never obtains credentials or endpoint settings from libpq environment variables, password files or service files. A nonempty `PGSERVICE` is rejected because the upstream parser resolves it before returning its configuration; remove it from the application process and supply Foundry configuration explicitly. Other upstream parsing settings are masked, and session parameters are set explicitly to the selected application name and UTC timezone. Construction never runs schema changes. Apply [migrations](migrations-and-seeding.md) explicitly.

## TLS and resource limits

`postgres.VerifyFull` is the default: TLS 1.2 or newer with server identity verification and no plaintext fallback. An optional standard `*tls.Config` supplies trusted roots, a server name or client certificates. Foundry copies the configuration and certificate pools; certificate/key material must remain immutable after registration, as required by `crypto/tls`.

`postgres.RequireTLS` encrypts transport but does not verify the server's identity. `postgres.DisableTLS` explicitly permits plaintext, for example through the existing isolated local development tunnel. Disabled TLS cannot also receive a TLS configuration. Unsupported modes or conflicting version bounds fail validation.

`Config.Pool` uses `database.DefaultPoolConfig()` as its source of defaults and owns connect/acquisition deadlines and pool bounds. `StatementCacheCapacity` defaults to 512 per connection; zero disables statement caching and uses describe/execute. `MaxProtocolMessageBytes` defaults to 64 MiB and bounds a protocol message body, not the total query result. Streaming consumers must still close rows, set execution deadlines and bound application collections. An oversized protocol message fails the query; normal pool cleanup releases its connection ownership.

## Errors and transaction outcomes

The adapter classifies structured SQLSTATE values for unique, foreign-key, not-null and check violations, serialization conflicts, deadlocks, cancellation and unavailable connections. `database.Error` retains SQLSTATE and constraint identity. Normal error formatting omits statement text, bindings and driver messages; unwrapped driver causes may contain private data.

Only explicit server rejection evidence confirms rollback after a failed commit. `40003` (statement completion unknown), connection loss and unknown driver errors retain uncertainty. A PostgreSQL COMMIT that actually returns ROLLBACK is recognized as rejected. Foundry never automatically retries a transaction callback. Reconcile uncertain writes through application idempotency information.

## Acceptance and public test helpers

Run `make test-postgres` using the [contributing instructions](contributing.md#postgresql-acceptance). It requires an isolated test URL, runs fresh root and independent consumer tests with race detection, and fails if database configuration is missing. Ordinary tests skip the real database cases when no URL is supplied.

`testkit/postgres.Config(t)` reads the explicit test URL. `Open(t, adjustments...)` creates a test pool and registers bounded cleanup. `Namespace(t, db)` creates a unique schema in that test database and returns its safe identifier. Schemas are retained for inspection; the helpers never drop, truncate or reset data. Supply only the project's isolated test database.

Real PostgreSQL coverage includes selected database/user and UTC timezone, exact bound values, streaming and pool exhaustion, cancellation, unavailable endpoints, protocol message limits, rollback, savepoints, after-commit ordering, read-only transactions, constraint metadata, deferred commit rejection, serializable conflicts, deadlocks, concurrent migrations, checksum drift and repeatable seeding. The consumer verifies application assembly and transactions through public imports.

Protocol fixtures separately inject lost commit responses, cleanup errors and callback failures that are difficult to trigger reliably against a live server. TLS settings and configuration isolation have unit coverage; the local acceptance server does not certify a deployed TLS setup. [Generated model reads](model-queries.md) now execute through this adapter; raw scanning remains an explicit runtime boundary.
