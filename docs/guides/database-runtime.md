# Database runtime

The `database` package provides connector-based pooling, application lifecycle integration, parameterized SQL execution, streaming rows, callback-scoped transactions and sessions, explicit savepoints, and after-commit callbacks. [Migrations and seeders](migrations-and-seeding.md) build on these contracts. The [PostgreSQL adapter](postgresql.md) supplies the concrete driver. Protocol fixtures and real PostgreSQL acceptance verify their behavior; the [master roadmap](../../blueprint/00-master-architecture-and-parity.md) records milestone progress.

## Pool ownership

Infrastructure adapters supply a standard Go `driver.Connector` and a safe error classifier through `database.Adapter`. `database.Open(ctx, adapter, database.DefaultPoolConfig())` creates and pings an application-owned pool. It closes a newly created pool on failure and never applies schema changes. Consumers normally use `postgres.Open(ctx, config)` or register `postgres.Module` with application assembly.

`database.Prepare(adapter, config)` validates and constructs a pool without connecting or starting goroutines. Call `Start(ctx)` before operations; earlier calls fail with `database.NotReady`. One connectivity attempt is shared by concurrent callers, whose waiting deadlines are independent. A failed attempt is terminal for that pool; construct a fresh pool to try again. Closing a prepared pool is valid and does not connect.

`database.Module(providerID, poolKey, adapterFactory, config)` integrates this ownership with application assembly. The typed pool key is a `foundation.Key[*database.DB]`. The factory only constructs the adapter; it must not acquire resources. Foundry constructs a fresh pool per application, registers shutdown before connecting, and starts it during provider boot. Later startup failure closes it. The application retains its stopping state until the pool actually drains, even if a shutdown caller stops waiting.

Database modules also bind [typed observer declarations](../../blueprint/07-model-lifecycle-events-and-audit.md#observer-registration-integration) before any provider boots. Generated `Register<Model>Observer` helpers use `database.RegisterObserver` to target an explicit pool key and resolve constructor dependencies through the foundation. The immutable set belongs to that pool and follows its sessions, transactions and child savepoints. A direct `Prepare` caller can attach a `lifecycle.NewObservers(...)` set using `BindObservers` once before `Start`; an opened or module-managed pool rejects manual binding. Generated normal writes dispatch these observers alongside [declared model hooks](model-hooks.md#provider-observers), including when the model has no local factory.

The registry also accepts `lifecycle.NewRetrievalObserver[M, H]` declarations through that same database registration boundary. Write and retrieval factories retain separate hook types for each model; their names remain unique across the entire pool. `HasObservers` and `ObserverFactories` inspect writes, while `HasRetrievalObservers` and `RetrievalObserverFactories` inspect retrieval registrations. None of these lookup operations constructs hooks. Generated [retrieval observers](model-retrieval.md) connect those factories to complete-model reads after hydration and stream closure.

Modules bind the application's clock during their own boot callback, before pool startup. Earlier manual `Start` calls on a module-managed pool fail with `NotReady`. Direct construction defaults to `clock.System`; the optional `database.WithClock(source)` argument overrides either construction path. The configured source follows sessions, transactions and savepoints and is exposed by their `Clock()` methods. [Managed model timestamps](model-timestamps.md) use this source; infrastructure deadlines continue to use real context time.

`DefaultPoolConfig` returns fresh settings: 16 maximum open connections, two idle connections, a 30-minute connection lifetime, five-minute idle expiry, and five-second connect/acquisition deadlines. Change the fields explicitly and call `Validate` when integrating them with typed application configuration. Maximum open connections and both deadlines must be positive. Zero idle retention disables retained idle connections; zero lifetime/idle expiry disables the corresponding expiry. Negative values and idle counts above the open limit fail.

Acquisition has its own deadline. Once acquired, a statement uses its caller's context; the acquisition timeout does not silently become a query timeout. `Ping` verifies an acquired connection. `Stats` exposes readiness, pool use, acquisition waits, active owners, and whether shutdown has started.

`Close(ctx)` rejects new work immediately and waits for existing owners. It does not force-close a connection underneath open rows, transactions, or after-commit callbacks. A caller deadline stops waiting while cleanup continues. `Done()` closes only after owners have released and the underlying pool actually closes. Calls may repeat concurrently. Application work and driver operations must honor cancellation; Go cannot terminate uncooperative goroutines.

## Raw execution and typed hydration

`DB`, `Tx`, and `Session` implement `database.Executor`. `Exec(ctx, statement, arguments...)` returns a `Result` with `RowsAffected`; PostgreSQL generated keys belong in a `RETURNING` query, not `LastInsertId`.

`Query` returns streaming `Rows`, which retain their connection. Always defer `Close`, iterate with `Next`, call `Scan`, and check `Err`. Exhaustion and scan failure release the stream automatically; explicit early close also releases it. `Columns` returns a copied list. Keep iteration and scan sequential. `Rows` does not materialize the result set; individual field size and driver buffering remain separate bounds for adapter validation.

`Rows.Observers()` exposes the actual database owner's immutable observer registrations for framework adapters. A forwarding executor retains this ownership by returning the original `Rows`, including for session and transaction queries. The metadata remains inspectable after closing rows or the owning scope; retaining it does not hold a connection open, construct hook factories, or dispatch retrieval events. Callback lifetime and retrieval dispatch are separate lifecycle responsibilities.

Framework read adapters can enter `rows.WithObserverScope(ctx, callback)` before iterating or closing the stream. It retains work ownership independently of the SQL operation. Close rows before callback I/O on the same transaction/session; scope cleanup also closes them on return, error, panic or Goexit. The callback context preserves the supplied values, follows the query caller and owning scope cancellation, and expires after return. Premature parent return cancels and waits for callbacks before completing or committing; standard-library automatic rollback may already discard a canceled connection. This primitive does not invoke model retrieval hooks by itself.

`ScanOne(ctx, executor, statement, arguments, destinations...)` requires exactly one row and closes it on every path. No rows yields `database.NotFound`; multiple rows yield `database.TooManyRows`. `ForEach` accepts a typed hydration callback `func(database.Row) (T, error)` and a `func(T) error` yield callback. It retains no collection and closes rows on early return, error, or panic.

Closing rows may drain remaining results through the driver. For immediate server interruption, cancel a child query context before closing or returning from the yield callback. Give potentially unbounded work a deadline. Cancellation may invalidate the connection or transaction; follow the scope's error path rather than continuing an aborted transaction. [Recursive query acceptance](../../tests/fixtures/consumer/recursivequeries/recursive_postgres_test.go) exercises both a deadline and cancellation after the first delivered row.

These SQL strings, arguments, and scan destinations are explicit runtime boundaries. Use placeholders for values. Scan destinations can be partially populated on error and must then be discarded. [Generated model reads](model-queries.md) and [declared projections](model-projections.md) construct fresh typed records and publish only successfully hydrated values. Do not issue raw transaction-control statements such as `BEGIN`, `COMMIT`, `ROLLBACK`, or custom savepoints through these methods; use the framework's scope helpers.

## Transaction scopes

The [independent consumer example](../../tests/fixtures/consumer/database_example_test.go) compiles a parameterized query and transaction using only public Foundry contracts. It requires a connected database to execute; it is currently a compilation example, not PostgreSQL acceptance evidence.

`db.Transaction(ctx, callback)` runs one callback exactly once. An optional `database.TxOptions` selects default/read-committed/repeatable-read/serializable isolation and read-only mode. No transaction callback is automatically retried. Callbacks run in owned goroutines so panics and `runtime.Goexit` become safe failures followed by cleanup; the caller waits for actual callback exit.

Use the supplied `*database.Tx` sequentially. Its operations also observe the begin context even if a caller passes a separate background context. Close query rows before returning. Overlapping operations fail with `Busy`; returning with an unfinished operation or unclosed rows cancels and aborts the outer transaction. Escaped transaction handles reject later use. The callback cannot independently commit and then accidentally continue using its transaction.

Callback failure triggers rollback. A rollback error retains its cause without claiming confirmed rollback. `Tx.State` is terminal after completion; an unknown state can describe failed cleanup as well as an uncertain commit. Begin-context auto-rollback is owned by `database/sql`; when its result is unavailable, Foundry reports no commit attempt rather than asserting server-confirmed rollback.

Unconfirmed rollback and uncertain commit also discard the physical connection. Session state with an unknown transaction outcome is never returned to the idle pool.

## Savepoints and after-commit work

`tx.Savepoint(ctx, func(child *database.Tx) error { ... })` creates a generated safe savepoint name. Use the child inside that callback; the parent rejects concurrent access. A successful child reaches `TxReleased` and merges its after-commit callbacks into the parent. It has not committed. Failure rolls back that savepoint and discards its callbacks. A failed rollback/release poisons the outer transaction even if application code ignores the child error. Leaking work or rows from a child aborts the outer transaction.

`database.Transactor` names the shared transaction callback contract implemented by pools, sessions and transactions. `tx.Transaction` delegates to a savepoint and rejects nested transaction options; configure isolation/read-only settings on the outer transaction. [Generated model writes](model-writes.md) consume this interface so the same API composes into an existing transaction without committing it.

`tx.AfterCommit(func(context.Context) error { ... })` registers process-local work in order. It runs only after successful outer commit. For `DB.Transaction`, the connection is released first, allowing a callback to acquire another connection even with a one-connection pool. The transaction operation itself stays owned until callbacks finish, so pool shutdown still waits for them. Callbacks receive the original transaction caller's context; a successful commit does not extend a canceled request's deadline.

Callback errors, panics, and Goexit are collected while later callbacks continue. A returned `database.Error` with `Outcome() == database.Committed` means persistence succeeded despite an after-commit/release failure. Do not retry the whole transaction on that error. These callbacks have no crash durability; durable job/event publication belongs to the later outbox layer.

## Connection sessions

`db.Session(ctx, func(session *database.Session) error { ... })` holds one physical connection across sequential operations and transactions. It is intended for infrastructure such as migration advisory locks. Ordinary requests use `DB` or `Tx`.

`session.Transaction` shares the normal transaction implementation, but the surrounding session keeps the connection during after-commit callbacks. Those callbacks cannot reenter the session. A callback acquiring the database pool needs capacity for another connection. Parent session operations fail with `Busy` while a transaction runs.

The callback and all cleanup must exit before the connection is released. Escaped sessions reject use. Callback errors, panic, Goexit, cancellation, and unfinished work discard the connection; `session.Discard()` explicitly requests disposal when session state cannot be cleaned up. An outer session error preserves the transaction's committed or unknown outcome for reconciliation.

## Errors and verification

Use `errors.Is` with database codes, and `errors.As` for `*database.Error` metadata. Context errors remain discoverable. Structured classification includes SQL state and constraint identity when the adapter supplies them. Normal formatting omits SQL, bindings, driver messages, and panic payloads. Unwrapped causes may contain secrets and must not be logged. Adapter classifier panic/Goexit is isolated so it cannot strand startup or cleanup. Initial wrapped/joined error inspection is bounded: a cyclic or excessively large/deep callback graph reports `QueryFailed` before invoking the adapter, while the actual transaction still determines rollback/commit outcome. Custom matching and unwrap methods must return and keep a stable graph during inspection. Private causes are retained; the bound does not make arbitrary traversal of those causes safe.

Transaction outcomes distinguish `Committed`, confirmed `RolledBack`, `NoCommit` (no commit was attempted), and `Unknown`. `Unspecified` is the default for ordinary raw operation errors; it makes no persistence claim. Commit transport failures are conservative `CommitUnknown` failures unless the adapter has explicit server rejection evidence. Reconcile uncertain outcomes using application idempotency information.

Standard-library driver protocol fixtures cover ownership, exhaustion, cancellation, row errors, callback rollback, after-commit failures, savepoint order/cleanup, commit ambiguity, session disposal, and application lifecycle integration. `make verify` and `make race` include them. `make test-postgres` additionally requires real PostgreSQL acceptance in both framework and consumer modules. See the [adapter guide](postgresql.md) for concrete coverage and limits. There is no database reset command.

## Query-plan inspection

[Typed plans](query-plans.md) add separate `Explain` and `ExplainAnalyze`
operations to compiled SELECT queries. Locked/transaction sources retain their
`*database.Tx` requirement. These operations passed milestone 23 native PostgreSQL verification.

## Optional read endpoint

[Primary/read routing](database-routing.md) preserves raw primary SQL and transaction ownership while routing generated SELECTs through an optional read capability. It includes separate endpoint health, an explicit combined connection budget and shared shutdown ownership.
