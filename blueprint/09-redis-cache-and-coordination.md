# 09 — Redis, cache, and coordination

**Status:** complete — typed cache, Redis adapters, tags/namespace invalidation, local/distributed Remember, counters, leases, rate limiting, pub/sub, hashes/sets and scoped commands/pipelines/scripts passed full native verification and source/parity/consumer review.

## Purpose and prerequisites

Prerequisite: [02](02-foundation-and-application-lifecycle.md). Supply shared cache and coordination capabilities for auth, jobs, scheduling and realtime.

Rust references: `src/redis`, `src/cache`, `src/support/lock.rs`; `tests/distributed_runtime_acceptance.rs`, `support_stores_acceptance.rs`; `docs/guides/caching-and-redis.md`.

## Contracts and boundaries

Use typed cache key/value codecs and separate interfaces for cache, lease/lock, rate-limit, and pub/sub capabilities. An adapter can implement multiple interfaces; do not force every consumer to depend on one giant Redis manager.

Typed cache usage:

```go
profile, found, err := profiles.Get(ctx, profileKey)
```

The [typed cache guide](../docs/guides/caching.md) and [consumer](../tests/fixtures/consumer/caching/profiles.go) describe the first implemented slice. The profile cache is bound to a declared value codec; a different payload type cannot be written through the same typed API. Dynamic serialized bytes stay inside adapter boundaries.

## Implementation slices

1. Memory and Redis adapters with context support, typed errors, namespaced keys and owned connection lifecycle.
2. Cache operations, explicit miss semantics, TTLs, atomic add/increment, memoization/stampede control and tagged invalidation.
3. Lease locks with unique owner tokens, atomic compare-and-renew/release and cancellation-aware heartbeat.
4. Atomic distributed rate limiting and pub/sub contracts reused by later kernels.

Namespace all keys by configured application/environment plus feature and semantic identity. Document Redis persistence/eviction expectations per feature; do not put durable queues in an evictable cache namespace by accident.

## Failure behavior

Memory behavior is a single-process test/development capability. It does not silently become a fallback for distributed locks, sessions or jobs when Redis fails. Consumers choose any safe cache-read fallback explicitly.

A lost lock lease means ownership is lost. Stale owners cannot release or renew another owner's lock. Use fencing where the protected downstream resource supports it; a lease alone cannot prevent an already-running stale process from writing elsewhere.

## Acceptance

Test miss versus error, codec failures, expiry with a controlled clock, namespace isolation, concurrent atomic operations, stampede control, tagged invalidation, lease expiry/reacquisition, stale release/renewal, connection loss and shutdown. Run contract tests against memory and real Redis for their shared guarantees, and Redis-only tests for distributed behavior. Use the documented local Redis service; do not start replacement servers. Apply the [common gate](README.md#common-completion-gate).

## First slice: concrete Go boundary

The cache package owns Namespace, Name, EntryKey, TTL, key/value codecs, reusable
Declaration[K,V], the application Store and bound Cache[K,V]. The cache/memory
adapter implements the focused byte Backend, atomic Add and bounded LRU/expiry.
A store borrows its backend; the adapter owner controls closure. Redis connection
lifecycle and its provider registration remain with the next adapter slice.

Get returns (V, bool, error), preserving misses separately from zero/nil payloads
and failures. Put/Add require an explicit positive For duration or Forever. Cache
family declarations reject duplicate ownership before accessing stored values.
Key/value types remain ordinary Go generics visible to gopls. Consumers map getters
into cache snapshots explicitly; cache serialization does not export HTTP contracts.

Memory limits bound entry count and key/payload bytes. They do not certify total
process RSS or constrain arbitrary custom-codec allocation. Explicit failure,
owned buffers, exact injected-clock expiry and cancellation form the shared
acceptance contract the Redis adapter must also satisfy where applicable.

## First slice acceptance

Native macOS verification passed in 376.9 seconds with local PostgreSQL and real
gopls. The gate covered root and consumer tests, all 648 compiler-rejection cases,
233 consumer editor scenarios plus five field-documentation scenarios, vet,
formatting, three generation-freshness targets and documentation checks. All 1859
verification source fingerprints matched. Focused cache/consumer/editor races and
64,509 key-fuzz executions also passed. No dependency was added.

Consumer review confirms model-owned keys, concrete snapshots, explicit getter use
and borrowed adapter ownership. This accepts the typed-cache/memory slice only;
Redis and the remaining cache/coordination slices are still required.

## Local Remember slice

The typed Remember API coalesces misses per Store with bounded active fills and
followers, independently decoded snapshots, owned callback lifetimes and context
cycle detection. Canceled followers release only their wait; failed owners publish
failure without retry and release their fill. Concurrent Put/Forget remain ordinary
cache operations and can race with a fill, as documented in the
[guide](../docs/guides/caching.md#load-on-a-miss-with-remember). Distributed coalescing
remains with the Redis/lease work. Focused cache/consumer/editor races and both new compiler-rejection cases passed.
Full native verification passed in 377.8 seconds with 1864 unchanged source
fingerprints, local PostgreSQL, all 650 compiler cases, 234 editor scenarios plus
five field-documentation scenarios, vet/formatting, freshness and docs. No dependency
was added. The consumer review confirms typed domain loading with explicit getter
selection; the framework owns coalescing, storage and callback lifetimes.

## Typed atomic counters

CounterDeclaration[K] binds exact signed int64 values to model-owned or other typed
keys. It reuses value-cache declaration ownership and operation boundaries; a
focused CounterBackend capability supplies atomic increments. The memory adapter
shares its existing storage mutex, expiry, byte limits and eviction implementation.
Missing values begin at zero with an explicit initial TTL; later increments retain
the existing expiry. Canonical integer serialization and overflow handling share
one private implementation. Corruption/overflow/capacity errors preserve live data.

The [consumer](../tests/fixtures/consumer/caching/counters.go) demonstrates activity
counts without application key formatting or read/write arithmetic. Focused tests
cover type rejection, exact values, contention, expiry, corruption and failures;
arbitrary-precision arithmetic supplies the fuzz oracle. Focused cache/consumer/editor
races, three new compiler cases, two real editor probes and 619,028 fuzz executions
passed. Full native verification passed in 376.9s with 1877 matching source inputs,
local PostgreSQL, all 653 compiler cases, 236 consumer editor scenarios plus five
field-documentation scenarios, root/consumer tests, vet/formatting, all three
generation-freshness targets and docs. No dependency was added. Redis, tags, leases, distributed coalescing, rate limiting and pub/sub
remain required before milestone09 can be complete.

## Typed tag invalidation

TagDeclaration[K] and bound Tags[K] retain key ownership through For/Invalidate.
Cache.WithTags returns the existing typed cache API with a canonical tag set;
Counter.WithTags requires the additional tagged counter capability. Declaration
ownership, key codecs, deadlines, callbacks, CRUD and Remember remain shared.

TaggedKey separates a stable data address from a version-specific fill identity.
The backend stores a fingerprint beside data and atomically checks snapshots for
reads/writes/cleanup. Missing metadata receives fresh random versions, preventing
resurrection after eviction. Stale writers return Conflict before replacing newer
data. Stable addresses preserve Forever without accumulating one key per version.
The memory adapter uses its existing lock, entry budget and eviction implementation;
selected metadata plus the payload must fit together before a write succeeds.

The [tag consumer](../tests/fixtures/consumer/caching/tags.go) refreshes a getter-based
snapshot after explicit domain invalidation. Tests cover canonical sets, unrelated
views, immutable snapshots, metadata loss/eviction, concurrent tagged counters,
stale Remember owners/followers, persistent key-count stability, corruption, bounds
and atomic batch failure. Focused cache/consumer/editor races, three new compiler
rejections and two real editor probes passed. Full native verification passed in
541.2 seconds with 1891 matching source inputs, required local PostgreSQL, all 656
compiler cases, 238 consumer editor scenarios plus five field-documentation
scenarios, root/consumer tests, vet/formatting, three freshness targets and docs.
go-redis v9.22.0 was installed with explicit user approval. Its adapter and
real-service acceptance remain required.

## Redis connection and cache adapter

The public redis package owns explicit endpoint/TLS/credential configuration, pure
Prepare, one-attempt Start, bounded operations/connections and draining Close.
Module integrates that ownership with application lifecycle. Its Client implements
the existing cache.Backend and CounterBackend without exporting vendor APIs.
One bounded server script validates type/length, preserves atomic Add and exact
int64 increments, and applies explicit millisecond TTLs. Lost responses never cause
an implicit command retry. The [Redis guide](../docs/guides/redis.md) owns the concrete
consumer and provider semantics.

Memory and real Redis run shared byte ownership, miss, atomicity, counter precision,
corruption and cancellation contracts. Redis-only tests cover expiry, wrong
credentials, payload bounds and startup failure; owned protocol fixtures cover
capacity, lost replies, deadline handling and shutdown. The independent consumer
uses its existing typed profile/getter API with the Redis provider.

Full native verification passed in 493.4s with 1905 matching source inputs, required local PostgreSQL and Redis, all 656 compiler cases, 239 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Focused Redis/cache/consumer/editor races and shared memory/Redis contracts also passed.

Redis tag snapshots, leases, distributed coalescing, rate limiting and pub/sub remain required.

## Redis tag snapshots and conditional operations

The existing redis.Client now implements TaggedBackend and TaggedCounterBackend,
reusing operation ownership, payload bounds, TTL conversion and reply handling.
Memory and Redis share canonical tag-key validation; integer wire checks/errors
are shared between plain Redis counters and tagged hash counters.

Missing metadata receives a fresh versioned envelope; one MSET publishes validated
metadata batches. Tagged payload hashes keep stable addresses and exactly two fields.
Conditional Lua operations reject changed snapshots before accessing/mutating data;
obsolete payloads are absent without decoding them. Hash counters preserve int64
precision and expiry. Redis 7+ permission checks guard data-plus-expiry writes before
mutation; transport/server uncertainty remains explicit. No new dependency is added.

Shared memory/Redis contracts cover invalid batches/cancellation, owned bytes, tag
set isolation, stale operations, metadata loss, exact/corrupt counters, batch failure
and concurrent initialization. Native Redis tests cover two-client Remember races,
expiry, stable addresses/field counts, metadata envelopes, bounded/corrupt payloads,
permission failures and unsupported script capability. Permission failures use a
script-local fixture without changing Redis users or shared server configuration.
The independent consumer binds model-owned tags to the same Redis Store and retains
its getter-specific profile API.

Full native verification passed in 327.6s with 1915 matching source inputs, required local PostgreSQL and Redis, all 656 compiler cases, 239 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Shared memory/Redis tag races, Redis failure/cross-client tests, consumer races and three actual-gopls probes passed. No dependency was added.

Leases, distributed coalescing, rate limiting and pub/sub remain milestone09 work.

## Typed lease ownership

The public lease package supplies reusable Declaration[K], bound Leases[K],
Manager, Guard and a focused Backend. The keyspace package now owns shared namespace
syntax and typed key codecs; a private encoder preserves existing cache addresses
and separates lease addresses. Cache compatibility aliases keep existing consumer
code valid without a cache/lease dependency cycle.

Acquisition distinguishes confirmed contention from failure. Finite validity
subtracts command elapsed time and an explicit drift margin; late renewal cannot
revive expired ownership. With owns heartbeat, lost-ownership cancellation,
callback exit and compare-release. Manual guards expose renewal and explicit
release. Bounded managers own waiting, scopes and cleanup, and provider dependencies
keep their borrowed adapters alive until draining completes. Memory never evicts
live owners; Redis uses bounded atomic owner checks with no mutation retries.

The [lease guide](../docs/guides/leases.md) owns public usage, failure behavior,
resource limits and the limits of Redis lease authority. The consumer uses concrete
Member IDs, with six compile-fail cases and three real editor scenarios. Shared
memory/Redis and focused race tests passed, including late replies, uncertainty,
heartbeat/callback failures, stale owners, capacity and shutdown ordering.
Full native verification passed on 1945 matching source inputs with local PostgreSQL and Redis required, all 662 compiler cases, 242 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. The initial full run took 416.7s. After a regression test exposed release hiding an earlier manager cancellation, the corrected final gate took 12.2s: affected lease/Redis/consumer packages reran and 108 unchanged packages reused Go's valid test cache. Fresh focused lease/cache/Redis races also passed on the correction; consumer races, six new compiler rejections and three actual gopls probes passed. No dependency was added.

Distributed Remember, rate limits and pub/sub remain required before milestone 09 completion.

## Distributed Remember

NewCoordinatedStore configures coordination before binding declarations and borrows
both capabilities from the same lease-manager authority. Existing typed Remember
and tag APIs reuse the local fill registry, independent decoding, callback isolation,
bounds and post-election reads. Framework-owned WithProof supplies heartbeat and
cleanup; publication validates the exact fill binding and compares live ownership
atomically with cache writes. Tagged publication checks its snapshot in the same
command. Shared Lua payload/expiry checks and lease metadata parsing remain SSOT.

The [guide](../docs/guides/distributed-cache.md) owns usage, lifecycle, resource
bounds and failure guarantees. Native two-client tests cover coalescing, stale
owners, tag invalidation, timeout, callback failure, shutdown, cycles, proof binding
and uncertain write/release acknowledgements. Consumer races, three new compiler
cases and one actual editor scenario pass.

Full native verification passed in 414.1s with 1960 matching source inputs, required local PostgreSQL and Redis, all 665 compiler cases, 243 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Fresh focused lease/cache/Redis races, two-client failure tests, consumer races, three new compiler rejections and one actual gopls constructor probe also passed. No dependency was added.
Rate limiting and pub/sub remain required before milestone 09 completion.

## Typed rate limiting

The public ratelimit package supplies immutable Declaration[K], Limiter[K], Limit,
Decision and a focused Backend. Stores borrow adapters and bound entire key
resolver/codec/backend operations. Explicit memory and Redis authorities implement
fixed windows, admitted-cost accounting, live policy conflicts and no retry or
fallback. Redis owns its clock, bounded metadata and atomic absolute expiry.

HTTP RateLimit and RateLimitByIP reuse existing middleware, attribution and error
contracts. The [guide](../docs/guides/rate-limiting.md) owns public examples,
resource/failure semantics, Redis prerequisites and intentional Rust redesigns:
denials consume nothing, server time controls windows, errors return 503 and no
Actor is required. The [consumer](../tests/fixtures/consumer/limiting/members.go)
proves model-owned domain and HTTP quotas. Five new compiler rejections and two
actual gopls probes pass.

Full native verification passed in 465.6s with 1984 matching source inputs, required local PostgreSQL and Redis, all 670 compiler cases, 245 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Fresh focused rate-limit/Redis/HTTP races, consumer races, five new compiler rejections, two actual gopls probes and 2,861,333 timestamp fuzz executions also passed. No dependency was added.
Pub/sub remains required before milestone 09 completion.

## Typed pub/sub

The public pubsub package supplies versioned Declaration[K,V], Topic[K,V], typed
Subscription[V], a borrowed-adapter Broker, and provider ownership. Payloads reuse
Foundry value.JSON validation. A shared bounded ring implements explicit terminal
loss instead of silent eviction. Exact adapter channel batches retain namespace
and schema identity. Memory is explicit; Redis owns dedicated connections,
acknowledged establishment, reader/heartbeat loops and draining cancellation.

The [guide](../docs/guides/pubsub.md) owns public examples and limitations; the
[consumer](../tests/fixtures/consumer/messaging/members.go) publishes getter-selected
DTO values under model-owned keys.

Full native verification passed in 430.4s with 2012 matching source inputs, required local PostgreSQL and Redis, all 675 compiler cases, 247 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Fresh focused native races, consumer checks and 1,519,877 queue fuzz executions also passed. No dependency was added.
The remaining parity work below is required before milestone 09 completion.

## Parity review: remaining implemented Rust capabilities

The implementation-slice list was not a complete public API inventory. A source
review on 2026-09-16 found the following additional work before milestone 09 can
be complete. These are implemented Rust behaviors, not historical proposals, and
remain in this milestone rather than being silently deferred.

| Rust capability and evidence | Current Go status | Required closure |
| --- | --- | --- |
| `CacheManager::get/put/forget/remember`, tag views and tag flush (`src/cache/mod.rs`, `src/cache/tagged.rs`) | Typed cache/counters/tag invalidation and coordinated Remember accepted | Retain coverage while adding namespace invalidation |
| `CacheManager::flush`, `RedisCacheStore::flush` (`src/cache/mod.rs:160`, `src/cache/redis_store.rs:79`) | Accepted through automatic reserved snapshots and full native verification | Invalidate only the selected cache namespace, including plain/tagged/counter entries and stale in-flight fills; preserve control metadata and unrelated applications/features |
| `RedisConnection::get/get_optional/set/set_ex/del/incr/publish` (`src/redis/mod.rs:630`) | Covered through focused typed cache/counter/pubsub APIs | Preserve focused adapters and avoid requiring vendor-level calls for normal work |
| `RedisConnection::del_many/exists/expire` (`src/redis/mod.rs:687`) | Typed cache/hash/set and scoped general raw-key entry APIs accepted | Add typed, bounded operations with explicit result/cancellation semantics and consistent tag behavior |
| `RedisConnection::hget/hset/sadd/srem/smembers` (`src/redis/mod.rs:739`) | Typed hash/set facilities accepted with full native verification | Preserve concrete key, field and value types; bound collections and payloads; document expiry and atomicity |
| `RedisCommandBuilder`, `RedisCommand`, `RedisPipeline`, `RedisScript` (`src/redis/mod.rs:272–439`); execute methods at 590–612 | Scoped commands/pipelines/scripts accepted with full native verification | Provide explicit typed key and reply contracts with bounded batches, callback ownership and no implicit mutation retry; ordinary supported operations remain typed helpers |
| Manager namespace/channel construction and explicit foreign namespace selection (`src/redis/mod.rs:528–552`) | Focused feature addresses include application/environment and reject cross-feature key types | Preserve these guarantees in the new Redis data/command boundary; never infer another application's namespace or inspect its data |

The Rust cache flush increments a namespace generation; it does not issue a global
Redis flush. The Go design must reuse its current invalidation machinery and handle
metadata loss without resurrecting old entries. Acceptance uses only dedicated
owned namespaces and exact test resources, with no global flush, database wipe,
shared-server scan or service reconfiguration.

Raw command/script behavior is an explicit adapter escape hatch. It must not become
the normal hash/set/cache API or claim that arbitrary trusted script source is
statically verified. Pipeline transaction semantics must distinguish atomic server
execution from rollback guarantees; connection loss must not replay a batch whose
outcome is unknown. Public examples, compiler tests, consumer probes and native
service tests accompany each closure slice.

Pub/sub passed its full native gate. The gaps above still prevent claiming
milestone 09 complete. Milestone 10 starts after this closure work and its
consumer review, following the agreed one-milestone-at-a-time cadence.

## Cache namespace invalidation

`Store.Invalidate(ctx)` now rotates one reserved namespace tag through the shared
invalidation boundary. Native stores apply that snapshot automatically to ordinary
values, explicit tag views, counters and Remember fills. Stable data addresses,
fresh versions after metadata loss and existing atomic checks protect Forever data
and reject obsolete writers. New generations receive independent local/distributed
fill ownership; rotation leaves application tags and feature control metadata intact.

The public [guide](../docs/guides/caching.md#invalidate-a-complete-cache-namespace)
records callback ownership, read overlap, retention, adapter capabilities, memory
metadata costs and the Redis 7+ typed-store requirement. The reserved stamp does not
consume application tag/declaration capacity. Basic custom adapters retain their
ordinary cache API but cannot claim namespace invalidation. Typed counters and
coordinated stores validate the required corresponding tagged capabilities.

Shared memory/Redis contracts, actual cross-client stale-fill tests, lost-acknowledgement
checks, full application-tag bounds, reserved-key separation, bounded Forever memory,
callback isolation and independent consumer/getter checks cover this behavior.

Full native verification passed in 350.4s with 2018 matching source inputs, required local PostgreSQL and Redis, all 248 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. The existing 675-case compiler suite reused cached results; all 20 affected cache and cross-feature compiler rejection cases were additionally rerun uncached and passed. Fresh complete milestone 09 races and independent consumer races passed. No dependency was added.
Batch/existence/expiry, hash/set and scoped command/pipeline/script parity remain.

## Typed cache entry operations

Cache and Counter provide `Exists(ctx,key)`, `Expire(ctx,key,ttl)` and
`ForgetMany(ctx,keys...)`, with model-owned keys and ordinary typed results.
Entry operations never decode/copy payloads. Expiry preserves the current value;
batch input is bounded before deduplication and all keys encode before metadata I/O.
One snapshot protects the entire batch. Memory holds one lock; Redis checks all
metadata/envelopes before one DEL. Errors preserve live entries unless an unknown
remote acknowledgement hides an applied batch; no mutation is retried.

Shared envelope/expiry helpers and the existing callback/context boundary are the
source of truth for ordinary and tagged behavior. Tests cover complete batches
under contention, maximum 256 keys with 64 application tags plus namespace metadata,
expiry transitions, counter TTL retention, later corruption, old snapshots, lost
acknowledgements, malformed replies, callback cancellation/failure and typed DTOs.
The [consumer](../tests/fixtures/consumer/caching/entries.go) and
[guide](../docs/guides/caching.md#existence-expiry-and-batch-removal) own the concrete
API examples. Full native verification passed in 436.6s with 2039 matching source inputs, required local PostgreSQL and Redis, 680 compiler rejection cases, 251 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, formatting/vet, generation freshness and documentation checks. Focused complete milestone 09 races and consumer races passed.

Rust RedisKey also addresses hashes/sets and low-level commands. Their corresponding
entry operations still need the general typed Redis data boundary. This cache slice
does not close hash/set or scoped command/pipeline/script parity. Milestone09 stays
open until those remaining source capabilities are implemented and reviewed.

## Typed Redis data structures

`redis/data` now provides versioned hash/set declarations, model-owned resource
keys, typed hash fields/values and typed set members. The existing Redis client
implements focused entry, hash and set interfaces; the package graph stays acyclic.
[Consumer examples](../tests/fixtures/consumer/redisdata/members.go) and the
[guide](../docs/guides/redis-data.md) define the concrete APIs, bounds and failure
semantics. Foundry's existing canonical JSON and TTL primitives remain authoritative.

Native structures preserve existing expiry; new keys persist. Explicit expiry is
a separate operation. Type/cardinality checks precede one mutation. Deleting a
bounded batch validates every selected key before one DEL. Set enumeration returns
owned typed slices in canonical order, with explicit server-allocation limits for
externally corrupted data. Store callbacks retain their slots until actual exit.

Focused native behavior/races, ten compiler rejections and five real-gopls scenarios passed. Full native verification passed in 444.1s with 2067 matching source inputs, required local PostgreSQL and Redis, 690 compiler rejection cases, 256 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, generation freshness and documentation checks. This slice does not complete the
scoped command/pipeline/script boundary; milestone 09 remains open.

## Scoped Redis command boundary

`redis/raw` supplies model-owned key declarations, immutable key-required commands,
context-aware typed decoders, trusted scripts and one-shot heterogeneous pipelines.
The [consumer](../tests/fixtures/consumer/rediscommands/commands.go) and
[guide](../docs/guides/redis-commands.md) own concrete APIs and failure semantics.
The existing client supplies focused adapter methods with no retry or vendor leakage.

Typed data handles expose an explicit AdapterKey method so advanced commands reuse
their declaration instead of duplicating codecs or namespace construction. Raw keys
also supply typed existence/expiry/deletion for general Redis types. Shared TTL
conversion and expiry commands remain authoritative.

The API intentionally uses opaque hashed addresses instead of retained logical
suffixes, immutable reusable commands, and one-shot pipelines. Pipeline Len/IsEmpty
describe the unclaimed queue and Mode records ordinary versus transactional execution.
Tag declarations and their original typed keys replace Rust's stored string tag
name lists; `TagDeclaration.Name` remains inspectable, while opaque references
resolve codecs only inside context-owned operations. Manual pooled-connection state
changes are rejected; dedicated lifecycle/subscription APIs or key-bound scripts
own those operations. Arbitrary raw arguments and Lua remain
trusted code, not a statically proved namespace sandbox.

Native tests reproduce Rust's leaderboard, typed script, ignored SET + INCRBY
transaction and XGROUP prefix-key examples. They also cover general-key expiry,
whole-batch result publication, nil versus error, two pipeline modes with runtime
errors, actual applied/lost acknowledgements without retry, mixed namespaces,
immutable buffers and reply trees, maximum limits and callback ownership. Twelve
new compiler rejections and six actual-gopls scenarios passed. Full native acceptance
and the final source/parity review passed; milestone 09 is complete.

## Milestone 09 completion

Full native verification passed in 463.5s with 2095 matching source inputs, required
local PostgreSQL and Redis, 702 compiler rejection cases, 262 consumer editor
scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting,
generation freshness and documentation checks. The full eleven-package milestone
09 race suite passed. Earlier slices retain their detailed acceptance records above.

The final source review covered Rust Redis manager/connection helpers, key/channel
construction, cache and tagged cache methods, memory/Redis adapters, lock acquisition/
renewal/release/heartbeat and command/pipeline/script examples. Concrete consumer
fixtures demonstrate each feature through public Go imports. Go representation and
ownership differences are documented above; no remaining milestone 09 runtime
capability gap was identified.

Milestone 10 may now begin. Authentication, jobs, scheduling and WebSocket domain
integration remain in their owning milestones. This completion does not claim
Redis durability, cluster/Sentinel support, or complete-framework release readiness.
