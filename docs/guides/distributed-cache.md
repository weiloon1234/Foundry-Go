# Distributed cache Remember

Configure coordination in application assembly; domain code keeps its typed
`Remember` call. The [independent consumer](../../tests/fixtures/consumer/caching/distributed.go)
borrows the application's lease manager:

```go
func DistributedStore(manager *lease.Manager) (*cache.Store, error) {
    return cache.NewCoordinatedStore(
        manager,
        cache.DefaultConfig(manager.Namespace()),
        cache.DefaultCoordinationConfig(),
    )
}
```

The manager's backend must implement `cache.CoordinatedBackend`, using the same
authority for leases and cached values. The existing Redis client supplies that
capability. Cache and manager namespaces must match. Construction performs no I/O;
unsupported adapters, invalid configuration and closed managers fail explicitly.
A tagged store additionally requires `CoordinatedTaggedBackend`.

Bind the same cache declarations to this store. Model-owned key types, concrete
payloads, getters, TTLs and tag views stay unchanged. The consumer's existing
`RememberProfile` continues to load a concrete Member and choose its email getter.
Ordinary `cache.NewStore` retains local coalescing. There is no fallback from
failed distributed coordination to an uncoordinated loader or memory adapter.

## Miss ownership and publication

A miss first joins the existing bounded local fill registry. Its selected owner
acquires the fill lease, rechecks the cache, then loads only if it is still absent.
Other instances wait for ownership and re-read the value after acquiring it.
Within one store, followers share the owner's encoded result and each decode an
independent snapshot. A hit does not acquire a lease.

`Remember` owns a renewable lease while its loader runs. Cancellation or renewal
loss cancels the loader context. It waits for the loader's actual exit, preserving
capacity limits even when domain code ignores cancellation. Failed owners return
errors to their local followers. A later caller may acquire and load again; this
is not exactly-once execution.

Publication checks lease identity and writes the cached value in one Redis Lua
operation. This closes the gap between a client-side ownership check and a later
write. A superseded owner receives `lease.ErrLost` and cannot replace a newer fill.
Tagged publication checks the tag snapshot in that same command; invalidation
rejects old writers and gives new generations independent fill leases.

`Put` and `Forget` retain their ordinary cache semantics. They may race with a
loader; distributed Remember does not turn arbitrary cache/domain writes into a
transaction. Protected writes to other resources need their own transactions or
fencing. Redis failover/eviction/clock and process-pause limitations remain those
in the [lease guide](leases.md).

## Limits and errors

`CoordinationConfig` selects the lease duration and maximum contention wait;
defaults are 30 seconds and two seconds. The manager validates both against its
limits. `cache.Config.Timeout` bounds the entire call, including reads, waiting,
loading and publication. Set these deliberately for expensive loaders.

`MaxFills` and `MaxFillWaiters` retain the local registry bounds. The shared lease
manager also limits active acquisitions, remote waiters and owned callback scopes.
Contention with zero wait returns `fault.Conflict`; an exhausted positive wait
returns `context.DeadlineExceeded`. Errors never cause implicit loader fallback.

A canceled local follower releases only its wait. A canceled owner releases its
lease and must still finish its callback. Propagate the supplied context into
nested Remember calls: same-store and cross-store cycles on the same canonical
fill identity fail with `fault.Cycle`. Cycles using unrelated contexts cannot be
inferred. Namespaces must distinguish independent cache authorities whose same-key
operations should not be treated as the same logical fill.

Transport failures can hide an applied write or release. The caller receives an
error; no mutation is retried and no rollback is claimed. An unacknowledged write
can be visible to subsequent readers. A cleanup failure remains available from
`Manager.Close`, even if the value was published. Lease expiry bounds remote
ownership that could not be cleaned up.

## Adapter boundary and lifecycle

`lease.WithProof` supplies an opaque, live proof for feature integrations.
`cache.ValidateFillProof` checks local cancellation/validity and binds the proof to
the exact fill address. An adapter must ALSO compare ownership at the authority
atomically with publication. A local check alone is insufficient. Proofs expire
with their guard and are not durable credentials or monotonically increasing
fencing tokens. Applications ordinarily use typed cache calls without handling
proofs, owner tokens or physical keys.

The store borrows its manager and backend. Use the existing lease provider's
reverse shutdown ownership so its callbacks and cleanup finish before Redis closes.
The explicit memory cache/lease adapters remain local capabilities and do not
implement combined distributed publication.

## Verification

Native two-client tests cover coalescing, post-election reads, stale publication,
tag invalidation during loading, timeout without fallback, callback failures,
shutdown, recursive contexts, proof binding and uncertain acknowledgements.
Consumer races, three new compiler-rejection cases and an actual gopls constructor
probe pass.

Full native verification passed in 414.1s with 1960 matching source inputs, required local PostgreSQL and Redis, all 665 compiler cases, 243 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Fresh focused lease/cache/Redis races, two-client failure tests, consumer races, three new compiler rejections and one actual gopls constructor probe also passed. No dependency was added. See the [master acceptance record](../../blueprint/00-master-architecture-and-parity.md#milestone-09-distributed-remember-acceptance).

## Namespace rotation

`store.Invalidate(ctx)` rotates the reserved namespace stamp used automatically by
native cache stores. Publication checks it in the same atomic operation as the
lease proof and application tags. A new generation gets an independent fill lease;
a running old loader stays owned until it returns, then its stale publication
fails. Namespace rotation does not delete its lease or silently retry its work.
The [namespace guide](caching.md#invalidate-a-complete-cache-namespace) owns the shared
retention and adapter contract.
