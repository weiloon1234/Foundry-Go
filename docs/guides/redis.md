# Redis connections and typed caching

Foundry owns a standalone Redis connection pool through `redis.Client`. Applications
keep using the [typed cache API](caching.md): model-specific keys, concrete values,
explicit TTLs and typed counters. The vendor client remains private; advanced
capabilities use the explicit [scoped command boundary](redis-commands.md).

## Application ownership

```go
var RedisConnection = foundation.NewKey[*redis.Client]("app.redis")

config := redis.DefaultConfig()
config.Host = "redis.example.test"
config.User = "application"
config.Password = secret.New(password)

app, err := foundry.New().Register(
    redis.Module("app.redis", RedisConnection, config),
).Build(ctx)
if err != nil {
    return err
}
// Start when the assembled application is ready; normal kernel execution shares
// the application's lifecycle. Module installs cleanup before its health probe.
if err := app.Start(ctx); err != nil {
    return err
}
client, err := foundation.Resolve(app.Services(), RedisConnection)
if err != nil {
    return err
}
store, err := cache.NewStore(client, cache.DefaultConfig(cache.Namespace{
    Application: "example", Environment: "production",
}))
if err != nil {
    return err
}
profiles, err := ProfileEntries.Bind(store)
```

The [independent consumer](../../tests/fixtures/consumer/caching/redis.go) uses the
same `Profiles` handle and getter-based snapshots as its memory configuration.
An application's domain services receive their bound handles through ordinary
constructors. They do not assemble Redis commands or physical cache keys.

`Build`, `redis.Prepare` and cache binding perform no network I/O. The module starts
a fresh client for each application. `Start` verifies connectivity once; a failed
attempt is terminal for that instance. Concurrent callers can cancel their own wait.
`redis.Open` is the standalone convenience constructor and closes resources on
startup failure. Direct owners call `Close`; applications use `app.Shutdown`.

Shutdown rejects new operations, retains ownership of in-flight commands, then
closes connections. A canceled `Close` caller stops waiting while cleanup continues.
`Done` closes only after cleanup finishes. `Stats` exposes connection counts,
operation owners and readiness without exposing credentials or the vendor client.

## Explicit connection settings

`DefaultConfig` enables verified TLS with TLS 1.2 or newer. Set `Host` explicitly;
there is no environment, URL, credential-file or plaintext fallback. Local development
can explicitly use `config.TLS = redis.DisableTLS`. Credentials use `secret.String`.
Custom TLS configuration is snapshotted; certificate/key objects must remain immutable.

`MaxConnections` caps the pool (default 64, comparable to go-redis's 10 per CPU
on a typical host, but fixed so validation is deterministic). `MaxOperations`
(default 1024) also bounds commands waiting for a connection. A burst beyond it
queues in FIFO order for at most the operation timeout (capped at five seconds)
and the caller's deadline, then returns retryable `fault.Overloaded`, which HTTP
maps to 503 with `Retry-After`. `Close` stops queued callers with `fault.Closed`.
The per-command admission path uses atomics and a semaphore rather than a shared
client mutex. The application-wide `Redis.MaxConnections` budget defaults to 1024
connections across all configured Redis connections, including subscriptions.
A command whose reply arrived is a success even if its deadline expires
immediately afterwards. Connection, operation and pool wait timeouts must be positive. Context deadlines can shorten them. Cancellation
before acquisition is checked immediately. Cancellation during socket I/O can wait
until its existing socket deadline; the command keeps its owner until it exits.

The initial adapter targets standalone Redis using RESP2. Cluster/Sentinel routing,
client-side caching and automatic pipelining are not provided. Each cache namespace
must be unique to its application/environment. Redis persistence and eviction remain
server deployment decisions; this adapter does not change them or silently fall
back to local memory.

## Cache semantics and provider differences

`Get` distinguishes absence from present empty bytes. `Add` is atomic and preserves
an existing entry's TTL. `Put` replaces its TTL; `Forever` explicitly persists the
entry. Positive finite TTLs round up to milliseconds, so a nanosecond never becomes
an accidental persistent entry. These choices follow Redis's [SET expiry semantics](https://redis.io/docs/latest/commands/set/).

Counters use Redis signed integer arithmetic. Results return as decimal bytes,
which preserves values above 2^53. Existing counters retain their TTL; missing
counters start at zero and receive the caller's initial TTL. Overflow and corrupt
representations fail without changing the stored value.

Reads check the stored type and byte length on the server before returning the
payload. Set `redis.Config.MaxValueBytes` to cover the bound used by `cache.Config`.
The Redis bound also protects callers using the adapter boundary directly. An
entry of the wrong type or shape, or larger than the current bound (for example
written before the bound was lowered), is a miss: `Put`/`Add`/`Increment` replace
it, `Forget` removes it (reporting false) and batch removal deletes it without
counting it as live. A non-canonical counter string still fails `Increment` with
`fault.Invalid` and remains unchanged. This
bounds ordinary stored-value replies; it does not bound allocations caused by a
malicious server sending invalid RESP frames.

A single bounded Lua operation combines validation and mutation. It is sent with
`EVALSHA`; only a `NOSCRIPT` reply (which proves the script did not run) uploads the
source, and there is no automatic command retry. A lost reply
means the mutation may already have happened; the returned error does not imply
rollback. Other clients cannot interleave commands inside the script, as described
in Redis's [Lua execution contract](https://redis.io/docs/latest/develop/programmability/eval-intro/).

## Entry inspection, expiry and atomic batches

The typed cache/Counter `Exists`, `Expire` and `ForgetMany` methods work through this
adapter without application Redis commands. The [cache entry guide](caching.md#existence-expiry-and-batch-removal)
owns the common result, cancellation, bounds and snapshot contracts.

Redis existence checks read the type/length/fingerprint without transferring the
payload. Expiry uses PEXPIRE or PERSIST with the same positive-millisecond rounding
as Put; it preserves bytes, following Redis's [expiry semantics](https://redis.io/docs/latest/commands/expire/).
Foundry reports true for any current entry accepting the TTL, including an already
persistent entry when Forever is requested.

Batch removal validates all selected envelopes and the shared snapshot before one
exact [DEL](https://redis.io/docs/latest/commands/del/). Invalid later keys cannot
partially remove earlier live entries. Counts exclude obsolete tagged payloads;
metadata is retained. The single bounded command is sent once, with no implicit
retry on an uncertain reply. These operations use cache EntryKey/TaggedKey and
private cache envelopes; they are not general-purpose hash/set accessors.

## Namespace invalidation

`store.Invalidate(ctx)` uses the same version/snapshot machinery as typed tags.
All native typed-store entries automatically include a reserved namespace stamp;
plain views, tagged views, counters and coordinated fills share its invalidation.
Only that metadata address is rotated. No key discovery, global flush or Redis
configuration change is performed. Existing application tags and other features
retain their own control metadata. See [cache namespace semantics](caching.md#invalidate-a-complete-cache-namespace)
for ownership, retention, memory costs and adapter compatibility.

Typed stores require Redis 7+ because even untagged cache operations use the shared
tag scripts and their permission preflight. The direct serialized adapter methods
remain separate lower-level contracts. A failed invalidation acknowledgement can
hide an applied rotation; the command is never retried automatically.

## Typed tags across Redis clients

Bind the same tag declarations used with memory to the Redis-backed Store:

```go
tags, err := MemberChanges.Bind(store)
if err != nil {
    return err
}
view, err := profiles.WithTags(tags.For(member.ID))
if err != nil {
    return err
}
profile, err := view.Remember(ctx, member.ID, cache.For(5*time.Minute), loadProfile)
// After the corresponding domain change has committed:
err = tags.Invalidate(ctx, member.ID)
```

Use [the existing typed tag consumer](../../tests/fixtures/consumer/caching/tags.go)
with [RedisStore](../../tests/fixtures/consumer/caching/redis.go). The model-owned
key, getter-specific profile value and `WithTags` API are unchanged. Each process
uses the same declarations and namespace; it owns its own connection pool.

Redis implements `TaggedBackend` and `TaggedCounterBackend`. Tag operations require
Redis 7 or newer for [script permission checks](https://redis.io/docs/latest/develop/interact/programmability/lua-api/#redisacl_check_cmdcommand-arg).
An unavailable script capability fails before creating metadata. Only the current
local Redis service is covered by integration acceptance; Cluster/Sentinel routing
remains outside this adapter.

Missing metadata receives a fresh random version in a versioned wire envelope;
corrupt metadata fails with `fault.Invalid` and is never overwritten. Typed reads use one script (`SnapshotReadBackend`) that resolves or
creates the namespace/tag versions and reads the tagged hash in the same atomic
round trip; obsolete, over-bound or malformed payloads are misses. Reads reclaim
only obsolete (another snapshot) or malformed payloads; a current payload that
merely exceeds this process's bound is left in place, so processes configured with
different `MaxValueBytes` never delete each other's valid entries.
Direct typed writes (`Put`, `Add`, `Forget`, `Increment`, `Expire`) use the tagged
script's resolve mode (`SnapshotWriteBackend`): it resolves or creates the versions
and mutates the payload in the same script, so each costs one round trip.
Tagged payloads store the concatenated snapshot versions (16 bytes per tag plus the
namespace stamp) as their fingerprint so the script can compare them server-side;
payloads written by earlier releases are obsolete misses that the next write replaces.
All selected metadata is validated before a single atomic replacement publishes an
invalidation set. Corruption in a later tag cannot partially rotate earlier tags.
The backend checks every snapshot again in the same script as the data operation.
Missing or changed versions return `fault.Conflict`, preventing an older loader,
writer or cleanup operation from replacing/deleting a newer cached value.

Tagged payload addresses stay stable across invalidations. Their Redis hashes hold
one fingerprint and one value, without accumulating generation keys or fields.
An older fingerprint is a miss; cleanup occurs only under a current tag snapshot.
Obsolete payloads are not decoded, including when a new counter replaces an older
non-counter value. Current payload type/shape/byte limits are checked before reads.
Counters use `HINCRBY` and return exact decimal bytes, retaining existing expiry.

A tagged Put/Add preflights both the data write and expiry permissions before any
mutation. Get can initialize missing metadata and remove obsolete data, so tag
read access also needs the corresponding metadata/cleanup command permissions.
ACL denials, corruption and stale snapshots preserve live data. Transport and
unexpected server failures retain the ordinary uncertain-write contract; Lua does
not provide rollback after a runtime error.

Metadata expires after 30 days without use. Every read or write refreshes it (when
less than half the period remains), and a finite tagged write keeps it alive at
least as long as the entry, so metadata never expires before a finite entry that
depends on it. Metadata written by earlier releases without a TTL receives one on
first use. Expired metadata is recreated with a fresh version, so an idle expiry
turns old entries into misses without resurrecting them; `Forever` payloads that
are not read for 30 days therefore become misses and are reclaimed on their next
access or replacement. Namespace rotation rewrites one metadata key in place and
never accumulates keys; stale payloads are reclaimed on access, replacement or
their own expiry. Redis server memory/eviction policy governs
total retained keys and bytes; Foundry bounds each operation, tag set and value.
Restoring server state or losing accepted writes during failover remains a Redis
durability concern. Cache tags do not claim crash-durable domain events.

Ordinary stores coalesce Remember locally. [Coordinated stores](distributed-cache.md)
add distributed fill ownership and atomic publication; typed leases and both cache
modes passed full native acceptance. Rate limiting and pub/sub are also accepted;
namespace invalidation, [typed data](redis-data.md) and
[scoped commands](redis-commands.md) are also accepted.

## Verification

Run the Redis suite against an explicitly selected local service:

```sh
FOUNDRY_TEST_REDIS_ADDR=127.0.0.1:6379 make test-redis
```

Optional `FOUNDRY_TEST_REDIS_USER` and `FOUNDRY_TEST_REDIS_PASSWORD` configure the test
connection. Do not put credentials in tracked files. Without an address, ordinary
test runs skip the external-service cases; the required flag makes that an error.
Tests use unique Foundry-Go namespaces and delete only their exact owned keys.
They never flush, scan or reconfigure a shared server. Owned protocol fixtures test
lost replies, safe error formatting, stalled responses and shutdown independently
of the real service.

Full native verification passed in 493.4s with 1905 matching source inputs, required local PostgreSQL and Redis, all 656 compiler cases, 239 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Focused Redis/cache/consumer/editor races and shared memory/Redis contracts also passed. See the [master acceptance record](../../blueprint/00-master-architecture-and-parity.md#milestone-09-redis-connection-and-cache-acceptance).

Full native verification passed in 327.6s with 1915 matching source inputs, required local PostgreSQL and Redis, all 656 compiler cases, 239 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Shared memory/Redis tag races, Redis failure/cross-client tests, consumer races and three actual-gopls probes passed. No dependency was added. See the [Redis tag acceptance record](../../blueprint/00-master-architecture-and-parity.md#milestone-09-redis-tag-acceptance).

## Typed lease adapter

The same client now implements the focused lease backend. See the [lease guide](leases.md)
for typed declarations, guarded callbacks, provider ordering, validity and failure
semantics. Shared/native races and full native acceptance pass. See the [lease acceptance record](../../blueprint/00-master-architecture-and-parity.md#milestone-09-typed-lease-acceptance).

## Coordinated cache publication

`PutLeased` and `PutTaggedLeased` check exact fill ownership and publish atomically,
reusing the ordinary/tagged cache scripts and bounded lease metadata reader. The
[distributed cache guide](distributed-cache.md) describes the typed consumer API
and uncertain-write behavior. Focused and full native acceptance pass. See the
[acceptance record](../../blueprint/00-master-architecture-and-parity.md#milestone-09-distributed-remember-acceptance).

## Typed rate-limit adapter

The same client implements atomic typed rate decisions using its server clock and
bounded versioned metadata. See [rate limiting](rate-limiting.md) for model-owned
keys, HTTP middleware, Redis 7 prerequisites and uncertain-consumption semantics.

Focused races and full native verification pass; see the
[rate-limit acceptance record](../../blueprint/00-master-architecture-and-parity.md#milestone-09-typed-rate-limit-acceptance).

## Typed pub/sub adapter

See [typed pub/sub](pubsub.md) for ephemeral typed fan-out, confirmed subscription
readiness, explicit overflow/disconnection, provider ownership and shutdown.
MaxSubscriptions bounds dedicated subscription connections independently of the
ordinary command pool. Stats expose both resources. The same namespace remains
isolated even though Redis database numbers do not isolate pub/sub channels.

## Typed data structures

Use [typed Redis hashes and sets](redis-data.md) for resource field maps and
membership. `redis/data` declarations preserve keys, fields and values while the
existing client supplies their focused adapter capabilities. Cache invalidation
does not affect these separate data addresses. Full native acceptance passed.
