# Primary and read database pools

Primary/read routing passed independent connector, PostgreSQL, lifecycle and
consumer acceptance. See [production acceptance](../production-acceptance.md).

`postgres.RoutingConfig` contains `Primary Config`, optional `Read Config`, and
`MaxConnections`. Each endpoint keeps its own `PoolConfig`; their `MaxOpen` sum
must fit the combined process ceiling. Replicas are optional. Use explicit
configuration from the application's normal secret loader:

```go
routing := postgres.RoutingConfig{
    Primary: primaryConfig,
    Read: value.Set(readConfig),
    MaxConnections: 24,
}
module := postgres.RoutedModule(DatabaseProvider, DatabaseKey, routing)
// Register module through the ordinary application builder.
```

Set each endpoint's pool bounds to fit the total. The module keeps the existing
`foundation.Key[*database.DB]`, observer registration and application clock.
`postgres.OpenRouted` supports explicitly owned database lifetimes. For custom
adapters, `database.WithReadPool` accepts a pure adapter factory, pool settings
and combined ceiling; `WithConnectionLimit` can cap all contributed endpoints.
Repeated read pools or explicit limits are rejected. Preparation does not
connect. Startup verifies every configured endpoint; partial failure closes
both pools and does not publish a ready runtime.

Generated SELECT queries, counts, existence checks, streaming, relation reads,
projections and query plans use the optional `database.ReadRouter` capability.
This dispatch is based on the already compiled operation, never SQL text.
Without a configured replica, reads use the primary pool. A configured replica
failure returns an error; it never silently falls back.

```go
recent, err := models.QueryUsers().Limit(10).All(ctx, db)
consistent, err := models.QueryUsers().Limit(10).All(ctx, db.Primary())
```

Replica reads do not promise immediate visibility of a primary commit. Keep
dependent reads inside the original transaction or pass `db.Primary()`.
`PrimaryExecutor` also implements the ordinary transaction capability. Locked
queries still require `*database.Tx`; a primary executor does not weaken that
compile-time requirement.

Raw `DB.Exec` and `DB.Query` use the primary pool, including `INSERT … RETURNING`.
For raw SQL explicitly known to be safe on the read endpoint, use `DB.QueryRead`
or `database.ReadQuery`. Transactions, nested savepoints, connection-scoped
sessions, migrations and seeders retain their actual primary connection.
Retrieval observers receive the original executor and the original pool's
observer registrations. An executor wrapper owns its policy: forward
`QueryRead` explicitly when preserving routing. A wrapper exposing only
`Executor` keeps its existing `Query` behavior.

## Read-your-writes

Replica lag means a read right after a write may not see it. `database.WithStickyReads(window)` (or the connection's `sticky_read_window`, `postgres.RoutingConfig.StickyReadWindow`) makes routed reads sticky within a request scope: after a write through the DB in a context from `database.StickyReads(ctx)`, reads that would use the read pool use the primary for `window`. Writes are `Exec`, primary `Query` streams (they may write, for example `INSERT ... RETURNING`), `Session` `Exec`/`Query`, transactions that are not read-only and framework single-statement writes; a read-only transaction does not count. The window is measured from the write's completion: a write marks the scope when it starts and again when it finishes (a statement returns, a stream closes, a transaction commits or its commit outcome is unknown), so a long write never consumes its own window. Other requests keep reading the replica, and stickiness ends after the window. Add `database.StickyReadsHandler` to the HTTP middleware stack to give each request its own scope; configured applications install it automatically as the outermost global middleware (`application.StickyReadsMiddlewareID`) when any connection enables its read pool and sets `sticky_read_window`, and add nothing otherwise. Background work calls `database.StickyReads` itself. Without a read pool or a marked context the option has no effect.

`Health(ctx)` reports the primary and configured read endpoint separately.
`Ping(ctx)` checks both; `PingPrimary` and `PingRead` check individual targets.
Without a read pool, `PingRead` checks primary. Pings use one dedicated probe
connection per endpoint outside `MaxOpen`, so they never queue behind
application work and a saturated pool does not report every replica unready.
Dependency failure belongs in readiness, not process liveness. Startup success
in `Stats.Ready` is not a substitute for current health checks.

`RoutingStats()` exposes independent connection counts, cumulative acquisition
waits and connections closed by idle count, idle time and lifetime, plus the
combined ceiling; the probe connection is not included.
`Stats()` aggregates connection utilization while counting resource owners once.
Shutdown rejects new work across both endpoints and retains existing rows,
transactions and observer callbacks until they release ownership. A canceled
`Close` caller stops waiting; `Done` closes only after both pools actually close.

Coverage sources: [independent connector routing](../../database/routing_test.go),
[PostgreSQL endpoint sessions](../../database/postgres/routing_test.go), and
[consumer module/read calls](../../tests/fixtures/consumer/tooling/routing.go).
The PostgreSQL test uses two separately configured connections to the existing
project database; it proves endpoint selection, not physical replication lag.
