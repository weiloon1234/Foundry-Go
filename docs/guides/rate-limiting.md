# Typed rate limiting

Foundry owns atomic, fixed-window admission; applications declare the resource key
and quota. The same typed limiter works in HTTP, CLI and domain services. Redis is
the shared authority; `ratelimit/memory` is an explicit local authority for tests or
single-process use. Neither becomes a fallback for the other.

## Declare and bind

The [independent consumer](../../tests/fixtures/consumer/limiting/members.go)
contains compiling examples. A declaration retains its exact key type:

```go
var MemberRequests = ratelimit.Define(
    "member-requests",
    keyspace.TextKeys[model.ID[Member]](),
    ratelimit.PerMinute(60),
)

store, err := ratelimit.NewStore(client, ratelimit.DefaultConfig(namespace))
if err != nil { return err }
limiter, err := MemberRequests.Bind(store)
if err != nil { return err }
decision, err := limiter.Take(ctx, member.ID, 5)
if err != nil { return err }
if !decision.Allowed {
    // RetryAfter is relative to the authority's decision time.
    return handleBusy(decision.RetryAfter)
}
```

Here `client` is the application's existing `*redis.Client`, `namespace` is a
`keyspace.Namespace`, and `Member`/`handleBusy` belong to the application. `Allow`
is the one-unit convenience operation. A different model's ID does not compile.
`TakeWith` resolves a concrete key inside the same bounded operation as its codec
and backend call; the HTTP adapter uses it to own key extraction safely.

Reuse one declaration value within a store. Rebinding a copy is valid; a different
declaration with the same name is rejected even when its Go key type matches.
`Limit()` returns a value copy, so changing it cannot modify a bound policy.

`Limit{Requests: ..., Window: ...}` permits positive uint32 capacities and windows
from 1 millisecond through 24 hours, in whole milliseconds. `PerSecond`, `PerMinute`
and `PerHour` construct ordinary limits. Cost must be positive and no greater than
capacity; an impossible cost is an input error rather than an endless denial.

## Admission and policy changes

Windows align to Unix-epoch multiples of their duration. A five-unit quota admits
a cost of three, rejects another three without consuming anything, then admits a
cost of two. Denials leave both usage and expiry unchanged. Arithmetic is bounded
and checked before addition; maximum uint32 capacity cannot wrap into fresh quota.

`Decision` contains `Allowed`, `Limit`, `Remaining`, `ResetAfter`, and `RetryAfter`.
`RetryAfter` is zero for admitted work; for denied work it equals `ResetAfter`.
Durations describe the authority's decision time and are not computed from the
client clock. Network latency can make them conservatively stale. Any operation
error returns a zero decision, including a response received after cancellation.

Fixed windows can admit bursts around a boundary; this is not a sliding window or
a concurrency limit. Admission consumes capacity before the protected work runs.
There is no refund when that work later fails, and no automatic retry.

All clients using the same application/environment, declaration name and encoded
resource share one bucket. Live buckets reject different capacities or windows
with `fault.Conflict`; a new policy can take effect after the previous bucket
expires. During rolling policy changes, use coordinated deployment or an explicit
new declaration name if intentionally granting fresh quota. Renaming, losing, or
resetting authority state resets its quota; it is not an idempotency guarantee.

## HTTP middleware

```go
middleware := foundryhttp.RateLimit(limiter,
    func(r *http.Request) (model.ID[Member], error) {
        return currentMemberID(r.Context())
    },
)
```

The resolver is a typed domain boundary; model-first authentication will provide
its model context in milestone 10. This API requires no Actor abstraction. Apply
the middleware through existing route/scope `WithMiddleware` or global
`ApplyMiddleware`. Route middleware can inspect `MatchedRoute` and native path
values. The handler receives its original request and response writer after
admission; resolver deadlines do not cancel the downstream handler context.

For IP limits, declare `keyspace.TextKeys[netip.Addr]()` and use
`foundryhttp.RateLimitByIP(limiter)`. Foundry first uses existing `ClientIP`
attribution, then the direct `PeerIP`. IPv4-mapped addresses are unmapped and zones
removed. An invalid peer with no attribution returns 400. Install `TrustedProxy`
before the IP limiter when forwarded attribution is needed; the limiter never
reinterprets untrusted forwarded headers itself.

Every admitted or denied response gets `X-RateLimit-Limit`,
`X-RateLimit-Remaining`, and `X-RateLimit-Reset`. Reset is seconds until the window
ends, rounded up, matching Rust Foundry's duration convention. These X headers are
Foundry compatibility metadata, not a claim of standardized rate-limit headers.
Confirmed denial returns the existing JSON `rate_limited` error (429) plus
`Retry-After` in rounded-up whole seconds, as defined by
[RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html#section-10.2.3).

Backend failure, local operation saturation, policy conflict, canceled admission,
and callback panic/Goexit return the existing `unavailable` response (503). Explicit
key-resolver errors use Foundry's normal error mapping, so an authentication error
can remain 401. Errors never execute the downstream handler or imply quota denial.

Reusing a declaration across routes intentionally shares quota. Use distinct names
for independent route quotas. Middleware IDs are bounded deterministic hashes of
exact declaration names, so all valid family names fit the HTTP identity grammar;
installing the same declaration twice in one chain is detected as a duplicate.

## Redis authority

Use the existing [Redis lifecycle](redis.md) and approved go-redis adapter. This
capability requires **Redis 7 or newer** on the supported standalone connection.
One EVAL uses [Redis TIME](https://redis.io/docs/latest/commands/time/) for the
authority clock, reads bounded metadata and atomically returns admission. Redis 7
uses [script effects replication](https://redis.io/docs/latest/develop/programmability/eval-intro/#script-replication),
so server time can drive writes without relying on synchronized application clocks.

One stable address stores a bounded, versioned string containing policy, window
end and admitted usage. Type, canonical integers, range and absolute expiry are
checked before mutation. `SET ... PXAT` writes usage and expiry together; there is
no separate increment/expiry step that can leave persistent unbounded counters.
`PEXPIRETIME` verifies the stored expiry matches metadata. Wrong types, oversized
values and malformed state fail without replacement. Backward clock movement
outside a live bucket fails rather than reopening an earlier bucket.

The client's existing operation/connection limits, cancellation and draining
shutdown apply. If a write succeeds but its acknowledgement is lost, the caller
gets an error and consumption remains unknown. Foundry does not replay the command
or compensate with a second mutation. Infrastructure eviction, restart without
preserved state, or failover can lose buckets; use an appropriate non-evicting
Redis authority and persistence policy for security-sensitive quotas. This adapter
does not promise durable exactly-once admission or cluster/failover consensus.

## Resource ownership and local tests

A Store borrows its backend, performs no startup I/O and needs no background
manager. Stop callers before closing the adapter. Defaults bound 256 declarations,
1,024 logical key bytes, 128 active operations, and a five-second operation deadline.
Namespaces and keys reuse `keyspace`; rate addresses are distinct from cache and
lease addresses. Encoded keys are hashed only after byte/UTF-8/control checks.

The active slot includes resolver, codec and backend execution. Panic and Goexit
are caught without exposing panic payloads. A callback that ignores cancellation
retains its slot until it actually exits: the timeout is cooperative and does not
guarantee forced return from arbitrary Go code. Saturation returns an error;
capacity is never reclaimed while its callback is still running.

For deterministic tests, use `memory.New(maxEntries, clock)` and the same Store
and declarations. The injected clock must return promptly. Deadlines still use
real context time. The memory authority rejects backward clock readings, performs
constant-time hot-bucket checks, reclaims expired buckets when capacity is needed,
and never evicts a live bucket to admit another key. Closing releases its state.

Shared memory/real-Redis tests cover weighted decisions, exact maximum counts,
concurrency, policy conflicts, cancellation and independent resources. Additional
tests cover window boundaries, namespaces, corrupt state, callback ownership,
uncertain acknowledgements, HTTP behavior, wrong Go types and real gopls discovery.
Full native verification passed in 465.6s with 1984 matching source inputs, required local PostgreSQL and Redis, all 670 compiler cases, 245 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Fresh focused rate-limit/Redis/HTTP races, consumer races, five new compiler rejections, two actual gopls probes and 2,861,333 timestamp fuzz executions also passed. No dependency was added.
See the [acceptance record](../../blueprint/00-master-architecture-and-parity.md#milestone-09-typed-rate-limit-acceptance). Milestone 09 still requires pub/sub before milestone 10 begins.
