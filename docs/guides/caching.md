# Typed caching

Milestone 09 provides typed cache declarations, local coalescing through Remember,
exact atomic counters and typed tag views with a bounded memory adapter. The
[Redis adapter](redis.md) supplies connection ownership, ordinary caching and counters.
Redis also implements typed tags and [leases](leases.md) with full native acceptance.
[Distributed Remember](distributed-cache.md), [rate limiting](rate-limiting.md) and
[pub/sub](pubsub.md) also passed full native acceptance. Namespace invalidation also passed full native acceptance. For general Redis values and commands, see the delivered
[typed data](redis-data.md) and [raw command](redis-commands.md) guides.

## Declare once, bind once

A declaration owns its name, key codec and payload codec. Binding it to an application
store preserves both Go types through every operation. The [independent consumer](../../tests/fixtures/consumer/caching/profiles.go)
uses a model-owned ID and explicitly selects a getter when creating a cache snapshot:

```go
var ProfileEntries = cache.Define(
    "member-profiles-v1",
    cache.TextKeys[model.ID[mutatorqueries.Member]](),
    cache.JSON[Profile](),
)

profiles, err := ProfileEntries.Bind(store)
if err != nil {
    return err
}

profile, found, err := profiles.Get(ctx, member.ID)
if err != nil {
    return err
}
if !found {
    // Load the domain value and explicitly choose its presentation/accessor fields.
}
```

`Put(ctx, key, value, ttl)` replaces an entry. `Add` returns `(bool, error)` and only
inserts if the entry is absent or expired. A rejected Add preserves the existing
value and its TTL. `Forget` returns whether an unexpired entry was removed. A miss
returns zero V, false, nil. A cached zero value or nil pointer still returns true;
a backend or codec failure returns an error and never publishes a partial value.

Use `cache.For(duration)` for positive expiry or `cache.Forever()` for explicit
persistence. A zero or negative duration and the zero TTL value are invalid.
TTL begins when the backend accepts a write. Expiry does not depend on the lifetime
of the request that wrote the entry.

Copies of one declaration can bind repeatedly. A separately created declaration
with the same name fails with `fault.Duplicate`, even when its Go types match.
Use a versioned family name when an incompatible cache representation changes.
Binding validates declarations without invoking codecs or performing I/O.

## Typed tags and invalidation

A tag family has its own typed key. Bind it to the same store as the cache view:

```go
var MemberChanges = cache.DefineTag(
    "member-changes-v1",
    cache.TextKeys[model.ID[mutatorqueries.Member]](),
)

tags, err := MemberChanges.Bind(store)
if err != nil {
    return err
}
view, err := profiles.WithTags(tags.For(memberID))
if err != nil {
    return err
}
// view retains the same model-owned key and Profile value type.
profile, err := view.Remember(ctx, memberID, cache.Forever(), loadProfile)
// After the domain change commits:
err = tags.Invalidate(ctx, memberID)
```

The [compiling consumer](../../tests/fixtures/consumer/caching/tags.go) and its test
show a refreshed snapshot using the model's getter while preserving stored fields.
Tags are explicit domain invalidation contracts; they do not automatically observe
SQL writes. Dispatch invalidation after a committed change when that is the intended
lifecycle. A cache failure does not roll back an already committed database write.

WithTags requires at least one bound Tag, replaces an earlier tag set, and validates
same-store ownership and adapter support without I/O. Tag ordering and duplicate
identities do not change the view's address. The configured MaxTags bounds the input
reference count before canonicalization (default 16, hard maximum 64). Key codecs
run inside each operation's context/error boundary; reference-backed keys must stay
immutable. Tags share the store's declaration-name ownership and registration limit.

`store.InvalidateTags(ctx, tagA, tagB)` invalidates all selected identities atomically.
Entries containing any selected tag become invalid. The untagged cache and different
tag sets remain separate. Counter.WithTags retains typed atomic counter operations
when the adapter implements TaggedCounterBackend.

Reads may initialize missing tag metadata with fresh random versions. Recreating
missing or evicted metadata never restores an earlier/default version.
Redis tag and namespace metadata expires after 30 days without use (every read or
write refreshes it) and never before a finite tagged entry written under it; an
idle expiry therefore only turns old entries into misses. Namespace rotation
reuses one metadata key, so it does not accumulate keys. Tagged data
uses stable storage addresses plus stored version fingerprints, so repeated
invalidation does not create a new persistent key per generation. Stale data is
reclaimed lazily on read or replaced at its existing key. Forever remains persistent;
normal backend capacity eviction still applies.

Writes and cleanup compare their captured tag snapshot atomically. A write crossing
invalidation or metadata eviction may return `fault.Conflict`; it does not silently
retry or overwrite/delete a newer result. Pure reads (`Get`, `Exists` and the lookup
inside `Remember`) never return `Conflict`: they re-resolve the snapshot up to three
times and otherwise report a miss. Adapters implementing `cache.SnapshotReadBackend`
(Redis) resolve metadata and read the entry in one atomic round trip. Adapters
implementing `cache.SnapshotWriteBackend` (Redis) also resolve metadata inside each
direct `Put`, `Add`, `Forget`, `Increment` and `Expire`, so these cost one round
trip and apply to the snapshot current at that instant instead of returning
`Conflict`; Remember publications keep the snapshot they read. A Remember
whose loader finished after an invalidation still returns its loaded value to the
callers that started before it, but its stale publication is rejected and reported
as a write failure, never stored. Remember coalescing also
includes the captured versions, so calls after invalidation do not join an obsolete
fill. Backend errors, corruption and callback failures remain explicit errors.
Invalid TTLs and nil Remember loaders fail before tag metadata I/O.

The memory adapter charges metadata entries, physical keys, payload bytes and the
stored fingerprint to its shared entry/byte budget. A tagged write must fit its
selected metadata and payload together; it cannot evict its own tags to claim success.
Other cache pressure may evict metadata, which safely invalidates old snapshots.
These bounds cover retained storage; arbitrary allocations in codecs/loaders remain
outside the cache's control. Stable addresses avoid generation-key accumulation,
but cache eviction/retention policies still govern entries never read again.

TaggedBackend and TaggedCounterBackend are explicit adapter contracts. Memory and
[Redis](redis.md#typed-tags-across-redis-clients) implement both; the same typed view
API works across them. Redis tags passed shared contract races, cross-client
invalidation and full native verification. Redis tag scripts require Redis 7 or newer.

## Invalidate a complete cache namespace

Call `store.Invalidate(ctx)` to invalidate every typed value, tagged view and counter
in that store's application/environment namespace, including other stores and
processes using the same authority. The [consumer example](../../tests/fixtures/consumer/caching/invalidate.go)
keeps existing handles and model/getter DTO types:

```go
if err := store.Invalidate(ctx); err != nil {
    return err
}
profile, err := profiles.Remember(ctx, memberID, cache.For(5*time.Minute), loadProfile)
```

Native memory and Redis stores automatically include one reserved namespace stamp
in every operation. No per-model registration or list of known keys is required.
This stamp is separate from application declarations and their maximum of 64 tags.
Construction remains pure; even a read may initialize missing metadata. A payload
codec failure can leave initialized metadata, but never a partially encoded payload.

Invalidation atomically replaces this one version. A write with an older snapshot
fails with `fault.Conflict` when it next accesses storage (a snapshot-write adapter
resolves the current version inside the write instead); reads re-resolve.
A loader that was already running remains owned until it exits; its stale
publication is rejected and reported (see Remember).
Calls resolving the new version use a separate local/distributed fill identity.
There is no automatic retry. Reads that completed before rotation may still return
the snapshot they already obtained; invalidation cannot recall caller-owned values.

This operation performs logical invalidation, without scans or global Redis flushes.
Application tag metadata, lease ownership, rate limits, pub/sub and other namespaces
remain intact. Payload addresses remain stable across rotations, including
`Forever` entries. Old payloads are reclaimed on access/replacement or normal
expiry/eviction; invalidation is not immediate physical erasure. Losing namespace
metadata creates a fresh version and cannot resurrect retained old payloads.

The reserved stamp and fingerprint count against memory storage limits. Even an
untagged entry must fit one metadata entry plus its payload (at least two entries).
Custom adapters implementing `TaggedBackend` receive the same automatic protection.
Adapters without tags that implement `cache.FlushBackend` (the file and PostgreSQL
stores) invalidate by physically removing the namespace's entries; a flush is not a
fence, so a fill that started earlier may still publish afterwards. Other basic
`Backend` adapters support ordinary typed cache operations, but `Invalidate`
returns `fault.Invalid`; Foundry does not claim namespace invalidation for them. Counter and coordinated adapters must also implement their corresponding
tagged capability so they cannot bypass namespace protection.

The native typed-store storage layout now includes the namespace stamp for both
ordinary and explicitly tagged entries. Earlier layouts are separate cold cache
entries; rotate all participating framework versions together. Direct adapter
writes bypass the typed-store invalidation contract. Redis typed stores now require
Redis 7+ for the shared tag scripts and their metadata/read/cleanup permissions.

Full native verification passed in 350.4s with 2018 matching source inputs, required local PostgreSQL and Redis, all 248 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. The existing 675-case compiler suite reused cached results; all 20 affected cache and cross-feature compiler rejection cases were additionally rerun uncached and passed. Fresh complete milestone 09 races and independent consumer races passed. No dependency was added.

## Atomic counters

Declare a counter family when values are signed integer counts. Its key remains a
concrete Go type; amounts and results are exact `int64` values throughout the API.
The [activity-count consumer](../../tests/fixtures/consumer/caching/counters.go) uses
model-owned member IDs:

```go
var ProfileViews = cache.DefineCounter(
    "profile-views-v1",
    cache.TextKeys[model.ID[mutatorqueries.Member]](),
)

counts, err := ProfileViews.Bind(store)
if err != nil {
    return err
}
views, err := counts.Increment(ctx, memberID, 1, cache.For(24*time.Hour))
```

`Increment` accepts a signed delta; negative deltas subtract. `Decrement` takes a
nonnegative amount. Both return the resulting value. Missing or expired entries
start at zero and receive the supplied initial TTL. Existing counters keep their
expiry even if an increment supplies a different TTL or has a zero delta. Initial
TTL must always be valid. `Put` explicitly replaces both the value and TTL; `Add`
initializes only an absent/expired entry. `Get` returns `(int64, bool, error)` and
`Forget` removes the entry. A cached zero remains a hit.

Overflow returns an error without changing the existing value or expiry. Stored
integers use canonical decimal bytes; malformed data, fractional values and values
outside int64 fail rather than being rounded or treated as zero. Counts above 2^53
remain exact. This is cache storage, so eviction or expiry can discard a count;
use durable domain storage for accounting and [typed rate limiting](rate-limiting.md)
for atomic admission decisions.

Counters reuse cache declaration ownership, namespace encoding, cancellation,
codecs, error handling and the memory adapter's entry/LRU budget. The counter
facade exposes atomic arithmetic through the separate `CounterBackend` capability.
Binding rejects unsupported adapters; Foundry never substitutes a read/write pair
for an atomic increment. A Store must permit at least 20 value bytes to represent
the whole signed int64 range. Per-entry backend capacity can still reject writes.

The memory adapter implements this capability now. Get/Put/Add/Forget and Increment
operate on the same storage under the same lock. Remote errors may hide a completed
mutation, so no automatic retry is permitted. Redis implements the same counter
capability and passed real-service and full native acceptance.

## Existence, expiry and batch removal

The [compiling consumer](../../tests/fixtures/consumer/caching/entries.go) keeps its
model-owned keys through these operations:

```go
exists, err := profiles.Exists(ctx, memberID)
changed, err := profiles.Expire(ctx, memberID, cache.For(time.Hour))
removed, err := profiles.ForgetMany(ctx, memberA, memberB, memberA)
```

`Exists` returns `(bool, error)` without decoding or copying the value. A current
nil/zero/empty value is present. The adapter still validates its storage envelope
and size limit; application codec validation and `Config.MaxValueBytes` decode
limits belong to `Get`. Existence is an observation, not a reservation: a later
read can miss after expiry/invalidation or fail its own decoding checks.

`Expire` updates a live entry's expiry without replacing its bytes or running its
codec. `cache.Forever()` removes expiry. True means a current entry accepted the
requested TTL, including when it was already persistent. Missing, expired or
invalidated entries return false. A zero/negative TTL fails before I/O; use Forget
for deletion. Later counter increments retain this updated expiry. Memory uses its
injected clock; Redis rounds positive durations up to milliseconds as for Put.

`ForgetMany` returns `(uint64, error)` and removes one complete batch atomically.
Keys retain the declared Go type. Input count is checked before key encoding or
deduplication: `Config.MaxBatchEntries` defaults to 64 and cannot exceed 256.
All keys must encode successfully before metadata I/O. Duplicate physical addresses
count once; missing/expired/invalidated entries count zero. An empty batch checks
the handle and context but does not call the backend.

One namespace/application-tag snapshot covers the whole batch. Native adapters
validate every snapshot and storage envelope before deleting any live entry.
A corrupt later entry or superseded snapshot fails the batch and preserves earlier
live entries. Redis sends one EVAL containing one exact DEL after validation;
there is no chunking, scan, pipeline or automatic retry. A lost reply may hide an
applied deletion: callers receive zero and an error, not a reliable partial count.

Counter exposes the same three methods with its concrete key type. WithTags views
retain these APIs and their namespace protection. No payload codec or model getter
runs during entry inspection, expiry changes or removal; cached DTOs keep the
getter values selected when they were created.

These methods borrow focused `EntryBackend`/`BatchBackend` capabilities and their
snapshot-aware `TaggedEntryBackend`/`TaggedBatchBackend` counterparts. Unsupported
adapters return `fault.Invalid`; Foundry does not emulate them with a Get/Put loop.
Batch adapters accept canonical, same-namespace keys and validate the shared hard
bound. Tagged batches require identical exact stamps for all selected entries.
A Store operation owns key/adapter callbacks until they actually return, including
cancellation and panic/Goexit handling. Keys backed by mutable references must stay
unchanged while the operation runs.

`GetMany(ctx, keys...)` returns `([]cache.Lookup[V], error)` in input order; a
repeated key repeats its result and each `Lookup` decodes its own copy. `Found`
distinguishes a miss from a stored zero value. The same `MaxBatchEntries` bound
applies and one namespace/tag snapshot covers the batch; a snapshot replaced by a
concurrent invalidation is re-resolved like `Get` and otherwise every key is a
miss. Adapters implementing `cache.BatchReadBackend`/`TaggedBatchReadBackend`
(memory and Redis) read the whole batch in one operation (one Redis script);
other adapters read each distinct key in turn. Missing, expired, over-bound and
unusable entries are misses. `Pull(ctx, key)` reads and then forgets one entry. It
is not atomic: a concurrent writer can replace the value between the read and the
removal, so use it for single-consumer values such as one-time notices.

`PutMany(ctx, ttl, entries...)` writes up to `MaxBatchEntries` typed
`cache.Entry[K, V]{Key, Value}` pairs with one TTL. Every key and value is encoded
before the first write, so a codec failure writes nothing. Entries are then written
in input order under one snapshot (a repeated key keeps its last value); the batch
is not atomic, the first failure stops the remaining writes and earlier entries
stay stored. `Config.Timeout` bounds the whole batch and no write is retried.

As with Forget, an overlapping Remember may later repopulate removed entries.
Use tag or namespace invalidation when old fills must be rejected. Batch atomicity
does not make cache changes part of a database transaction or recall values that
callers have already read. General Redis hash/set/data-key operations use the separate
[typed data API](redis-data.md); these adapter methods target Foundry cache envelopes.

Focused native races, consumer/getter checks, five new compiler rejections and three
actual editor probes passed. Full native verification passed in 436.6 seconds.

## Load on a miss with Remember

`Remember` returns `(V, error)`. A hit skips the loader. On a miss, the caller that
wins the store's fill registration supplies the loader, context and TTL. Concurrent
followers for that key wait for the same serialized result; each caller decodes
its own snapshot, including the owner. A loader returning zero or nil is cacheable.

```go
profile, err := profiles.Remember(ctx, memberID, cache.For(5*time.Minute),
    func(ctx context.Context) (Profile, error) {
        member, err := loadMember(ctx, memberID)
        if err != nil {
            return Profile{}, err
        }
        email, err := member.AccessEmail()
        if err != nil {
            return Profile{}, err
        }
        return Profile{Email: email}, nil
    })
```

The [compiling consumer](../../tests/fixtures/consumer/caching/profiles.go) shows
this model-to-snapshot mapping with a typed loader. Foundry owns cache operations
and coalescing; the application supplies its domain lookup. The loader must be
non-nil and TTL valid even when the entry already exists.

A new fill rechecks storage before running the loader. Failed reads, loaders and
encoding remain errors. There is no implicit retry. Loader failure reaches existing
followers; a later call can attempt a fresh fill. Decoding failures never return
partial values. Each caller decodes separately, so a decoder failure is local to
that caller.

A failed publication of a successfully loaded value does **not** fail the call:
the owner and its followers receive the value, the write is not retried, and the
failure is counted (`Stats().WriteFailures`) and logged through `cache.WithLogger`
with the cache family and a redacted diagnostic (never keys, values or error text).
Application assembly passes its configured logger automatically. A failed remote
write might still have been applied.

The loader runs in a fill owned by the store, under a context that keeps the
caller's values but not its cancellation. Canceling any caller, owner or follower,
only ends that caller's wait; the fill finishes for everyone else and keeps its
`MaxFills` slot until the loader actually exits. If a loader fails only because it
ignored its supplied context and waited on the owner's own ended request context,
followers elect a new owner (up to three attempts) instead of inheriting the
owner's cancellation. `Config.LoadTimeout` (default 30 seconds, at most 24 hours)
bounds each loader; `cache.WithLoadTimeout(d)` narrows one call.
`Config.Timeout` bounds each backend step (read, publish, metadata) and every
other operation, but never caps a loader.

`Config.MaxFills` bounds active coalesced fills across the store; `MaxFillWaiters`
bounds the followers for each fill. Defaults are 128 fills and 256 followers per
fill. When `MaxFills` is exhausted a new miss loads directly without coalescing
rather than failing; a full `MaxFillWaiters` returns retryable `fault.Overloaded`
(HTTP 503 with `Retry-After`). Finished fills and canceled followers release their
slots. These limits bound active registry state, not arbitrary allocations in
loaders or values retained by callers after return.

Nested loads must propagate the provided callback context. Detected self or ancestor
fill cycles return `fault.Cycle`; dependency cycles across unrelated contexts are
not inferred. Do not wait recursively on your own key using a background context.

With an ordinary Store, coalescing is scoped to that Store. Distinct stores or processes can load the same
key concurrently. `Remember` is not transactional with `Put` or `Forget`: an
in-flight fill may overwrite a concurrent write or repopulate a forgotten entry.
Use it for cacheable snapshots whose concurrency semantics permit that behavior.
Use [NewCoordinatedStore](distributed-cache.md) to add distributed lease ownership
and atomic conditional publication.

### Stale-while-revalidate with Flexible

`Flexible(ctx, key, fresh, stale, loader, options...)` is `Remember` that keeps
serving an aging value while it is refreshed:

```go
profile, err := profiles.Flexible(ctx, memberID, time.Minute, 10*time.Minute, loadProfile)
```

The [consumer's `FlexibleProfile`](../../tests/fixtures/consumer/caching/profiles.go)
wraps this with its own model loader.

A value younger than `fresh` is returned as is. An older value (up to
`fresh+stale`) is returned immediately while one store-owned background fill,
coalesced with every other fill of the key, loads and publishes a replacement. The
refresh keeps the caller's context values but not its cancellation, is bounded by
`LoadTimeout`, and its failure is only logged (`cache.WithLogger`); the next
stale read tries again. A missing or expired value is loaded exactly like `Remember` and
stored for `fresh+stale`. Freshness is a reserved marker entry written after the
value with TTL `fresh` and read together with it in one batch, so no clock
comparison is involved and invalidation removes both. The marker is an ordinary
stored entry, so it counts toward adapter capacity (`max_entries`/`max_bytes` of
file and PostgreSQL stores, the memory budget and Redis memory). The value remains
an ordinary entry of the family (`Get`, `Forget` and invalidation apply). `Put`,
`PutMany`, `Add`, `Increment` and `Expire` leave the marker unchanged: a value
written while a marker lives counts as fresh until that marker expires, and one
written without a marker is served stale and refreshed by the next `Flexible`.

### Request memoization

`cache.WithMemo(ctx)` returns a context whose typed reads are memoized for its
lifetime, typically one request. The first `Get`, `GetMany`, `Exists` or
`Remember` of a key reads the store; later reads of that key through the same
context return the memoized result without I/O, even if another request or process
has changed the entry since. `Remember` still consults the store after a memoized
miss because a fill needs its snapshot. Writes through the context (`Put`,
`PutMany`, `Add`, `Forget`, `ForgetMany`, counter `Increment`, `Expire`) forget
the key's memoized results, and `Store.Invalidate`/`InvalidateTags` through it
forget the whole store's. A memo records at most `cache.MaxMemoEntries` (256)
results and `cache.MaxMemoBytes` (1 MiB) of payload; further reads are simply not
memoized. Each result still decodes its own copy. `Flexible` is not memoized.

## Store and memory adapter

Application assembly creates the backend and store. Domain services receive their
bound typed cache; they do not format storage keys or serialize payloads.

```go
backend, err := memory.New(memory.DefaultConfig(), clock.System{})
if err != nil {
    return err
}
store, err := cache.NewStore(backend, cache.DefaultConfig(cache.Namespace{
    Application: "shop",
    Environment: "development",
}))
if err != nil {
    return err
}
```

`store.Stats()` returns lock-free totals since construction: hits and misses of
`Get`/`Remember`, successful writes, Remember publication failures, loader runs,
coalesced followers, uncoalesced loads and snapshot re-resolutions. Optional
collaborators are passed at construction, for example
`cache.NewStore(backend, config, cache.WithLogger(logger))`.

`cache.WithObserver(func(ctx context.Context, event cache.Event))` receives every
completed typed call synchronously for metrics or tracing: the declaration name
(`Family`, empty for store-wide invalidation), the `Operation` (its `String` is a
stable label such as `get_many`), key `Hits`/`Misses` of reads, `Loaded` and
`Unpublished` for Remember and Flexible misses, `Stale` for a Flexible value served
while it refreshes, the `Duration` and, on failure, the framework fault `Code`. Keys, values and error text are never exposed. The observer must be
fast and must not use the cache; a panic is contained and never changes the result.
One observer is allowed per Store.

`cache/null` is a backend that retains nothing (every read misses, writes succeed
without storing, `Remember` always loads). It implements every capability, so a
store can be disabled without changing declarations; configured applications
select it with the `null` driver.

The caller owns backend lifecycle and eventually calls `backend.Close(ctx)` after
its use. The store borrows the cache capability, so constructing or binding another
store does not close a shared adapter. `store.Close(ctx)` must run first: it
rejects new operations with `fault.Closed`, cancels the contexts of the store's
running fills (Remember loaders whose callers have gone, uncoalesced loads and
Flexible background refreshes) and waits until they have exited, bounded by `ctx`;
`store.Done()` closes once they have. A canceled fill publishes nothing.
Configured applications close every cache store during shutdown before its
backend, lease manager or database closes. Memory construction starts no goroutines.
Close serializes with backend operations, rejects future access and releases the
entry map. It does not manage application callbacks that already hold copied data.
[Redis provider registration](redis.md#application-ownership) integrates connection
ownership with application startup and shutdown.

Memory limits include entry count and the sum of physical-key/payload bytes.
Map/list overhead is bounded by entry count; Stats.Bytes is not process RSS.
Expired entries are reclaimed lazily, earliest deadline first from an expiry heap
(no whole-table sweep), before live entries are evicted for capacity. Reads
(`Get`, tagged reads, batch reads, `Exists` and tag resolution of existing
metadata) share a read lock and do not reorder storage: they mark the entry as
referenced, and capacity eviction gives a referenced entry a second chance
(recency order, approximately LRU) instead of removing it. An expired or obsolete
entry a read meets is a miss that the read then reclaims under the exclusive
lock. Hits copy their bytes after releasing the lock. Writes, expiry changes,
counters and tag creation or invalidation take the exclusive lock. An entry that
cannot fit the total byte budget is rejected before replacing existing data.
Successful capacity eviction is normal cache behavior.

Inject `testkit.NewClock` for deterministic expiry, including exact TTL boundaries.
Request deadlines remain real context deadlines. Supply a concurrent-safe clock
whose Now method returns promptly and does not reenter this backend.

## Namespaces, keys and codecs

The application, environment and family are part of every physical key. Each
namespace/family component is 1–128 ASCII letters, digits, dots, underscores or
hyphens. Logical key text is hashed after validation; storage addresses do not
contain raw logical keys such as email addresses. Hashing is an address format,
not encryption. Custom key codecs must be deterministic and distinguish different
semantic keys. The store applies its configured key bound before hashing.

Built-ins include StringKeys for named strings, SignedKeys and UnsignedKeys for
exact integer IDs, and TextKeys for types implementing encoding.TextMarshaler,
including Foundry's model.ID. NewKeyCodec is an explicit custom encoding boundary.
Applications use their key type; the exported EntryKey/Backend interfaces are for
adapter authors and focused test transports.

JSON follows Go's encoding/json rules, preserves dynamic numbers as json.Number,
and rejects trailing documents. It creates internal cache snapshots without declaring
HTTP response schemas. Model getters remain explicit: caching a stored model does
not automatically run its presentation accessors. The consumer's Profile.Email uses
a getter-specific named type, so assigning the raw string field fails compilation.

NewCodec accepts typed encode/decode functions for other formats. Callbacks must be
concurrent-safe, leave input values unchanged and return owned results. Buffers are
copied at adapter boundaries. MaxValueBytes bounds serialized values before writes
and before decode; it does not bound arbitrary allocations performed inside a custom
codec or Go's JSON encoder. The configured operation timeout reaches backend I/O.
Cooperative callbacks must return; Foundry waits for their exit, including failure,
so a timeout never abandons an owned codec goroutine.

## Failure and verification boundaries

Errors preserve context and original cause identity for errors.Is/errors.As while
normal formatting hides codec/backend payloads. The safe cache wrapper keeps the
cause's framework classification: an `Overloaded`, `Invalid`, `Timeout`, `Closed`
or `Conflict` cause is reported with that code (not `Internal`), so HTTP maps
capacity exhaustion to 503. A stored value larger than the current `MaxValueBytes`
(written under an earlier, larger bound) or a Redis entry of the wrong type or
shape is a miss that a later write replaces and `Forget` removes. Panic and runtime.Goexit become
safe callback failures. A failed mutating remote operation may have reached its
backend; the cache facade performs no automatic retry and does not promise rollback.
Consumers decide explicitly whether a cache error permits a domain fallback.

Behavior tests cover missing/zero/nil values, copy ownership, integer precision,
codec errors/panic/Goexit, corruption, cancellation, duplicate declarations, namespace
separation, exact expiry, atomic Add, LRU/byte limits and close behavior. Remember
adds deterministic concurrent-fill, cancellation, capacity, recursion, snapshot
ownership and loader/backend/codec failure tests with Go's testing/synctest. Consumer
compiler tests reject wrong ID owners, payloads, TTLs, codecs, raw backend keys and
raw values assigned to getter-specific fields. Counter cases also reject unrelated
model IDs, floating-point deltas and string values. An arbitrary-precision fuzz
oracle checks exact arithmetic and overflow boundaries. Actual gopls probes check the bound
model/value signatures and definitions. The master roadmap owns acceptance status and the historical verification records.

The typed-cache/memory and Remember slices passed full native macOS verification,
including local PostgreSQL and the complete consumer/compiler/editor checks. See
[the roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-09-local-remember-acceptance)
for acceptance evidence. The [Redis adapter](redis.md) now implements connection ownership, ordinary cache operations and counters; full native acceptance passed. Redis tags and leases passed full native acceptance. Distributed Remember, rate limiting and pub/sub also passed full native acceptance. Namespace invalidation, [typed Redis data](redis-data.md) and [raw commands](redis-commands.md) also passed full native acceptance.

[Typed counters](../../blueprint/00-master-architecture-and-parity.md#milestone-09-typed-counter-acceptance)
also passed the full native gate and focused race, compiler, editor and arithmetic
fuzz checks. Redis counter behavior passed real-service races and full native adapter acceptance.
