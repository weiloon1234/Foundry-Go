# Isolated PostgreSQL HTTP tests

T05 native verification passed. The [master](../../blueprint/00-master-architecture-and-parity.md#typed-api-delivery)
owns acceptance status. The [independent consumer](../../tests/fixtures/consumer/isolatedhttp/application.go)
uses ordinary application assembly, handwritten ORM declarations, typed HTTP DTOs and a real
PostgreSQL transaction per write. Handlers contain no test schema argument.

<a id="agent-entry"></a>
## Agent entry

- [Isolate PostgreSQL applications](#recipe-isolate-application)
- [Run HTTP or worker kernels](#recipe-run-kernels)
- [Retain failed test schemas](#recipe-schema-cleanup)
- [Configure Redis tests](#recipe-redis-test-config)
- [Remove one attachment original](#recipe-missing-attachment)

<a id="recipe-isolate-application"></a>
## Isolate an application

The low-level alias `pgtest` imports `testkit/postgres`; `pgapp` imports
`testkit/postgres/application` for application settings/migrations.

<!-- compiled-source: tests/fixtures/consumer/tooling/guide_recipes_test.go#isolated-application -->
```go
func isolatedApplication(t testing.TB) *application.App {
	t.Helper()
	scope, archive := pgtest.Isolate(t), pgtest.Isolate(t)
	settings := isolatedhttp.Defaults()
	env, err := pgapp.Bind(settings.Services.Database,
		pgapp.On(scope, "main", "reporting").WithReads(),
		pgapp.On(archive, "archive"),
	)
	if err != nil {
		t.Fatal(err)
	}
	settings, err = settings.WithDatabaseScopes(env.Settings())
	if err != nil {
		t.Fatal(err)
	}
	settings.Services.Namespace.Environment = scope.Schema()
	app, err := isolatedhttp.Build(t.Context(), settings, func(context.Context, isolatedhttp.Receipt) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Start(t, app,
		infrastructure.MigrationTarget{Definitions: isolatedhttp.Migrations()},
		infrastructure.MigrationTarget{Connection: "archive", Definitions: isolatedhttp.Migrations()},
	); err != nil {
		t.Fatal(err)
	}
	return app
}
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

<a id="recipe-run-kernels"></a>
## Run test-owned kernels

<!-- compiled-source: tests/fixtures/consumer/tooling/guide_recipes_test.go#run-http-test -->
```go
func TestConfiguredHTTPKernel(t *testing.T) {
	app, err := configuredprofile.Build(t.Context(), configuredprofile.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	baseURL := appstest.RunHTTP(t, app)
	client := httptest.Connect(t, baseURL)
	response, err := client.Do(t.Context(), client.Get("/profile"))
	if err != nil {
		t.Fatal(err)
	}
	httptest.AssertStatus(t, response, 200)
}
```

<!-- compiled-source: tests/fixtures/consumer/tooling/guide_recipes_test.go#run-worker-test -->
```go
func runWorker(t testing.TB, app *application.App) {
	t.Helper()
	stop := appstest.RunKernel(t, app, foundation.Worker)
	// Exercise this application's registered jobs before stopping it.
	stop()
}
```

For the production HTTP kernel, run `app.Run(ctx, foundation.HTTP)`, obtain its
address with `app.HTTPReady(ctx)`, and use `testkit/http.Connect(t, "http://"+address)`.
For test-owned kernels, import `testkit/application` as `appstest` and use
`appstest.RunHTTP(t, app)` to obtain the base URL. It waits up to 10 seconds for
HTTP readiness and stops waiting when the application finishes. A boot failure
stops the kernel and fails the test with its run error.

`appstest.RunKernel(t, app, foundation.Worker)` returns an idempotent stop function
and registers it with test cleanup. Stop cancels the run and waits for
`StopDelay() + ShutdownTimeout() + 2*time.Second`. A nil or cancellation-only run
result succeeds. A failure joined with cancellation or an expired drain wait
fails the test. Call stop explicitly to assert on shutdown before the test ends.

The [complete integration test](../../tests/fixtures/consumer/isolatedhttp/isolation_test.go)
shows client, kernel and application ownership. Register clients after the app
so client cleanup runs first. The application drains handlers/tasks before pools.
`testkit/http.New` remains available when a handler-owned test server is enough.

## Parallel execution and application concurrency

When integration tests use `t.Parallel()`, `Environment.Start` limits concurrent top-level test groups to `pgapp.DefaultConcurrency` (8 by default) per test process.

Each bound endpoint has a two-connection pool; `WithReads` adds a separate read pool. Total connection use depends on the configured endpoints and the number of applications each test starts. The limiter does not impose a process-wide database connection ceiling. Set `FOUNDRY_TEST_APP_CONCURRENCY` to a positive integer before starting the test process to override the default. An invalid value makes `Environment.Start` return an error.

Concurrency slots are tracked per top-level test name (`t.Name()` truncated at the first `/`). If a single test or its subtests boot multiple applications (e.g. testing background workers alongside HTTP servers), all applications in that test share the single slot by reference counting. This avoids requiring another slot when a test starts a second application; it does not bound applications within that group. The slot is released only after all applications in that test have completely shut down and drained their connection pools.

> [!NOTE]
> Concurrency limits are enforced per test process. When running `go test -p N ./...`, the Go toolchain executes `N` package test binaries in parallel. Each package process enforces its own concurrency limit against the shared database server.

### Parallel isolation contract

Every test running under `t.Parallel()` must maintain strict isolation across the following resources:

| Resource | Parallel Isolation Requirement |
|---|---|
| **Database Schema** | Allocate a unique schema with `pgtest.Isolate(t)` and bind it via `pgapp.Bind` / `pgapp.On(scope, ...)`. Handlers execute within their isolated schema; never query unscoped public tables. |
| **Redis & Keyspace** | Set a unique namespace on `settings.Services.Namespace.Environment` (e.g. `scope.Schema()`). Never run `FLUSHDB` or `FLUSHALL`. |
| **HTTP Ports** | Bind exclusively to ephemeral ports via `settings.HTTP.Server.Address = "127.0.0.1:0"`. Obtain the assigned port dynamically via `app.HTTPReady(ctx)` or `httptest.NewTLSServer`. |
| **Storage & Uploads** | Use test-owned temporary directories via `t.TempDir()`. Never write to static or shared filesystem paths. |
| **Clock** | Inject `testkit.NewClock(time)` via `application.WithClock(source)`. Advance application time deterministically via `source.Advance(...)`; never call `time.Sleep`. |
| **Environment Variables** | **Never call `t.Setenv` or `os.Setenv` in parallel tests.** Mutating process-wide environment variables causes data races and triggers Go test panics. Configure settings through typed `Settings` structs or CLI options. |
| **Global State** | Do not mutate package-level variables, `http.DefaultClient`, or `slog.SetDefault`. Pass test-owned clients and loggers explicitly. |

### Marking a test serial

If a test genuinely requires process-wide environment variable mutations or unshareable host state (such as testing CLI commands that read `os.Environ` directly, or spawning external daemon processes), mark the test as serial simply by **omitting `t.Parallel()`**.

Go executes all serial tests sequentially first, before executing parallel tests. Serial tests still acquire a concurrency slot through `Environment.Start`, ensuring safe connection bounds while running in complete isolation from parallel suites.

<a id="recipe-schema-cleanup"></a>
## Pool scope and retained data

The typed adapter `postgres.Config.Schema` selects one validated existing schema.
New and replacement connections establish it and UTC, verifying schema
existence/USAGE, before application access. A checkout restores them when the
server reports a changed `search_path` or `TimeZone` (PostgreSQL 18; older
servers restore on every checkout) and discards a connection if restoration
fails. A connection whose session created temporary objects is discarded on its
next checkout, even if they were dropped, so session tables never reach another
borrower. A missing scoped table cannot fall back to `public`; a
missing/inaccessible schema fails startup. An empty Schema preserves the
existing unscoped adapter behavior.

The `search_path` lists the application schema and then `pg_temp`, so temporary
tables cannot shadow application tables; PostgreSQL's implicit catalog
resolution remains available. See the [PostgreSQL schema rules](https://www.postgresql.org/docs/18/ddl-schemas.html#DDL-SCHEMAS-PATH).
This adds one bounded driver round trip per checkout (a catalog call when the
scope is unchanged). It does not reset arbitrary
session state or provide a security sandbox for fully qualified SQL/shared roles.
Use `database.Session.Discard` for custom session state such as advisory locks;
do not mutate the pool's schema from domain code. Transactions/session scopes
retain their selected connection until their callback exits.

After a passing test, `testkit/postgres` removes only that test's generated schema,
including its committed rows and migration history. Failure, panic or
`FOUNDRY_TEST_KEEP_SCHEMAS=1` retains it for diagnosis. A test that creates an object
outside its schema must clean up only that exact object on success, with the same
retention rule. Do not add a second cleanup script or remove retained schemas as
routine maintenance. The helper logs namespace identifiers without credentials.
Temporary PostgreSQL session objects naturally expire when their session closes.

Cache/Redis/queue namespaces, local storage directories and external services
need their existing independent test configuration when enabled; schema isolation
does not automatically isolate those resources. Set a distinct application
namespace and use test-owned directories/backends. Tests requiring database-wide
extensions or settings remain separate, explicitly configured integration tests.

<a id="recipe-redis-test-config"></a>
## Configure Redis test connections

Use the testkit parser once, including its optional username and password, then
convert the result to application settings. Give each test a unique application
namespace. Do not read credentials again or flush a shared store.

<!-- compiled-source: tests/fixtures/consumer/tooling/guide_recipes_test.go#redis-test-settings -->
```go
func redisSettings(t testing.TB) infrastructure.RedisSettings {
	t.Helper()
	connection := infrastructure.RedisSettingsFromConfig(redistest.Config(t))
	settings := infrastructure.DefaultRedisSettings()
	settings.Connections = infrastructure.RedisConnections{"default": connection}
	return settings
}
```

<a id="recipe-missing-attachment"></a>
## Simulate a missing attachment object

Pass the selected file from a collection result or a loaded model slot. The helper
removes that original object and retains its metadata and other files. Use only
test-owned storage. See [attachments](attachments.md) for creating and loading files.

<!-- compiled-source: tests/fixtures/consumer/tooling/guide_recipes_test.go#missing-attachment-object -->
```go
func removeOriginal(t testing.TB, manager *attachments.Manager, file attachments.File[models.User]) {
	t.Helper()
	attachmentstest.RemoveBackingObject(t, manager, file)
}
```

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
