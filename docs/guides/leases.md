# Typed leases

Declare a resource family once, then bind it to the application's lease manager.
The [independent consumer](../../tests/fixtures/consumer/coordination/members.go)
uses model-owned IDs and supplies only domain work:

```go
var MemberRefresh = lease.Define(
    "member-refresh",
    keyspace.TextKeys[model.ID[mutatorqueries.Member]](),
)
locks, err := MemberRefresh.Bind(manager)
if err != nil {
    return err
}
ran, err := locks.With(ctx, memberID, 30*time.Second, 2*time.Second,
    func(ctx context.Context) error {
        return refreshMember(ctx, memberID)
    },
)
```

`With` waits only on confirmed contention, renews while the callback runs, and
releases its opaque owner afterwards. The callback receives a context canceled on
caller cancellation, manager shutdown, expiry or failed renewal. `ran` reports
whether the callback started; errors preserve callback, ownership and cleanup
failures through `errors.Is`. A zero wait tries once: contention is `false, nil`.
A positive wait that expires returns `context.DeadlineExceeded`. Network/server
errors stop acquisition immediately; they are never converted into contention.

## Explicit scopes

`TryAcquire(ctx, id, ttl)` returns `(*lease.Guard, bool, error)`. `Acquire` adds a
bounded wait duration. These guards do **not** renew automatically: use
`guard.Renew(ctx)` before expiry or use `With` for framework-owned heartbeats.
Perform protected work using `guard.Context()`, check `guard.Err()` at meaningful
boundaries, and call `guard.Release(ctx)` when finished. Inspect release errors;
a deferred call alone can discard cleanup failure. Expiry or parent cancellation
also starts owned cleanup. No finalizer or detached drop handler is involved.

`lease.ErrLost` identifies a failed ownership check or local expiry. Explicit
release sets the work context's cause to `lease.ErrReleased`. `Release` reports
cleanup transport failures separately; a missing owner during conditional cleanup
is harmless. Repeated/concurrent releases share one attempt. A canceled release
caller stops waiting; `guard.Done()` still waits for cleanup completion.

## Validity and failure

Durations are finite, from 10ms through 24h. The client measures elapsed time with
Go's monotonic clock, starts its validity window before the command, and subtracts
one percent plus 2ms for drift/expiry margin. Commands are additionally bounded by
`OperationTimeout`. Heartbeats run at one third of the requested TTL. A renewal
must return before both the previous and proposed validity deadlines; a late
success cannot revive lost ownership. This is deliberately independent of the
application's injected business clock.

An uncertain acquisition gets one owner-conditional cleanup attempt. An uncertain
renewal cancels the work context and starts cleanup. There is no automatic retry
of a mutation, including release. An unacknowledged cleanup can leave a key until
its TTL expires; cleanup does not promise that an unknown remote mutation was
rolled back. Managers retain the first cleanup failure for `Close` as well as
returning it to the immediate caller.

Cancellation is cooperative: Go cannot stop arbitrary domain code. `With` waits
for the callback's actual exit even after its lease has been released, and keeps
its capacity slot until then. Downstream operations must honor cancellation and
use fencing or transactions where they require stale-process exclusion.

## Backend and application ownership

`lease.NewManager(backend, lease.DefaultConfig(namespace))` borrows a focused
`lease.Backend`. Config bounds active acquisitions, waiters, guards and callback
scopes together, plus declarations, key bytes, command timeouts and waiting.
When `MaxActive` is exhausted a new operation queues in FIFO order for at most the
operation timeout (capped at five seconds) and its own deadline, then returns
retryable `fault.Overloaded` (HTTP 503); `Close` ends queued waits with
`fault.Closed`. Backend failures keep their framework classification through the
lease wrapper (for example `Overloaded` or `Timeout` rather than `Internal`).
Contention polling uses bounded jitter; `Manager.PollInterval` exposes the
configured interval to integrations. Each live guard has at most one renewal
in flight; concurrent manual renewal fails instead of accumulating a queue.

`lease.Module` resolves a backend and installs shutdown ownership. List the backend
provider in its dependencies. The [consumer assembly](../../tests/fixtures/consumer/coordination/redis.go)
reuses its Redis connection and drains leases before that connection closes.
`Manager.Close(ctx)` rejects new work, cancels scopes and waits for all owned work
and cleanup. A canceled caller only stops waiting. `Done` signals actual draining;
calling Close synchronously from its own callback would wait on that callback.

The explicit `lease/memory` adapter uses a bounded map and never evicts a live
owner to admit another. Operations check only their own key; a new key reclaims
expired owners from an expiry heap in deadline order before checking capacity, so
no operation scans the whole table. It provides single-process behavior, with no
background server or automatic fallback from Redis. Close managers before their
adapter.

The existing `redis.Client` implements atomic acquire, compare-renew and
compare-release with one bounded Lua command. It validates stored type, owner size
and finite expiry before changing an existing key. Namespaced addresses use the
`lease` feature prefix, distinct from cache. Cache and leases share `keyspace`
namespace/codecs and a private address encoder; existing cache addresses and APIs
remain compatible.

Redis is a single authority here. Eviction, administrative key removal, server
clock changes, process suspension, restart or asynchronous failover can invalidate
coordination assumptions before a heartbeat detects loss. Use an appropriately
configured authority and enforce exclusion at the protected resource when that
is required. This API does not implement a quorum lock or a durable fencing token.
The [official Redis locking reference](https://redis.io/docs/latest/develop/clients/patterns/distributed-locks/)
describes owner-checked release and the limits of asynchronous failover.

## Verification

Memory and local Redis run shared ownership, contention, expiry, stale-owner and
invalid-input contracts. Additional tests cover bounded capacity, wait cancellation,
late/uncertain responses, heartbeat loss, callback panic/Goexit, reverse shutdown,
corrupt Redis metadata and lost replies. Consumer tests, six compiler rejections
and three actual gopls completion/hover/definition scenarios exercise typed usage.
Full native verification passed on 1945 matching source inputs with local PostgreSQL and Redis required, all 662 compiler cases, 242 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. The initial full run took 416.7s. After a regression test exposed release hiding an earlier manager cancellation, the corrected final gate took 12.2s: affected lease/Redis/consumer packages reran and 108 unchanged packages reused Go's valid test cache. Fresh focused lease/cache/Redis races also passed on the correction; consumer races, six new compiler rejections and three actual gopls probes passed. No dependency was added. See the [master acceptance record](../../blueprint/00-master-architecture-and-parity.md#milestone-09-typed-lease-acceptance).

[Distributed cache Remember](distributed-cache.md) also passed full native acceptance.
Rate limiting and pub/sub remain milestone 09 work.

## Administration, hand-off and concurrency limits

`locks.ForceRelease(ctx, id)` removes a lease whatever its owner and reports
whether one existed. Use it to repair metadata that ordinary operations reject as
corrupt (for example a Redis lease key without expiry, left by manual intervention)
or to break ownership after confirming the owning process is gone. The current
holder is not notified; it loses ownership at its next renewal or protected write.
Backends opt in through `lease.ForceBackend`; memory and Redis implement it.

`locks.Export(guard)` hands a live explicit guard to another process as a typed
`lease.Token[K]`. The guard stops renewing and ends with `lease.ErrExported`
**without** releasing the authority key. `MarshalText`/`UnmarshalText` transfer the
token; it contains the owner secret, so formatting redacts it and it must be
treated as a credential. Tokens are **single-use**: `locks.Restore(ctx, token)`
atomically replaces the token's owner secret with a fresh one at the authority and
renews the lease, then returns an explicit guard for the new owner. Restoring the
same token again (an at-least-once job retry or a redelivered message) returns
`lease.ErrLost` instead of creating a second holder, as does an expired, released
or replaced lease. If the restore's reply is lost, the fresh owner is released
once and the token stays consumed. A token cannot be restored into another
family. Backends opt in through `lease.TransferBackend`; memory and Redis
implement it.

`lease.DefineSemaphore(name, codec, slots)` bounds concurrent holders of each
typed resource key across every process sharing the authority. Each holder owns one
ordinary lease (`name` plus a slot number), so expiry, renewal, loss and cleanup
follow the lease contract. `Acquire` and `With` first try every slot once in
random order; a zero wait stops there and contention returns `false`. A waiting
call then polls with jitter, trying at most eight random slots per poll (so a busy
semaphore costs a few authority commands per interval), until the wait expires
(`context.DeadlineExceeded`). Blocking acquisition of a single
lease remains `Acquire(ctx, id, ttl, wait)`.

## Conditional feature writes

`WithProof` and `Guard.Proof` expose a live adapter capability when a backend can
check ownership atomically with a protected mutation. The [distributed cache](distributed-cache.md)
uses this for Remember publication. `Proof.Validate` is a local check; it must
never replace the authority-side comparison. `BorrowedBackend` lets feature
construction use the same authority without transferring adapter ownership.
