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

Each key's fixed windows start at a deterministic per-key phase,
`key.WindowOffset(window)`, derived from its address hash: windows begin at
`offset + n*window` rather than at Unix-epoch multiples, so the buckets of many
keys do not all reset (and invite a synchronized burst) at the same instant. Every
authority sharing a key computes the same phase. A five-unit quota admits a cost
of three, rejects another three without consuming anything, then admits a cost of
two. Denials consume nothing; they change a bucket only by persisting a policy
conversion (below). Arithmetic is bounded and
checked before addition; maximum uint32 capacity cannot wrap into fresh quota.

`Decision` contains `Allowed`, `Limit`, `Remaining`, `ResetAfter`, and `RetryAfter`.
`RetryAfter` is zero for admitted work; for denied work it equals `ResetAfter`.
Durations describe the authority's decision time and are not computed from the
client clock. Network latency can make them conservatively stale. Any operation
error returns a zero decision, including a response received after cancellation.

Fixed windows can admit bursts around a boundary; this is not a sliding window or
a concurrency limit. Admission consumes capacity before the protected work runs.
There is no refund when that work later fails, and no automatic retry.

All clients using the same application/environment, declaration name and encoded
resource share one bucket. A request whose capacity or window differs from a live
bucket converts that bucket to the requested policy immediately: usage already
admitted in the current window still counts (capped at the new capacity) and a new
window starts on the new policy's phase. A denied request persists the
conversion when the new window ends later than the live bucket, so carried usage
is never released early: tightening 10 per minute to 10 per hour after ten
requests keeps the key limited for the rest of the hour instead of reopening when
the old minute ends. A denial that would end sooner leaves the live bucket (and
its full usage) in place. During a rolling policy change, instances alternating between two
policies therefore keep counting usage instead of failing or granting fresh quota.
Use an explicit new declaration name when intentionally granting fresh quota.
Renaming, clearing, losing or resetting authority state resets its quota; it is
not an idempotency guarantee.

## Inspect, clear and attempt

```go
decision, err := limiter.Peek(ctx, member.ID, 5)       // non-consuming Decision
remaining, err := limiter.Remaining(ctx, member.ID)    // uint32
wait, err := limiter.AvailableIn(ctx, member.ID, 5)    // time.Duration, 0 if it fits now
cleared, err := limiter.Clear(ctx, member.ID)          // bool
decision, err = limiter.Attempt(ctx, member.ID, 1, func(ctx context.Context) error {
    return sendVerification(ctx, member)
})
```

`Peek` reports the decision a cost would receive now without consuming capacity
or changing expiry: `Remaining` is the capacity left now, `Allowed` reports whether
the cost fits, and `RetryAfter` equals `ResetAfter` when it does not. It applies the
same policy conversion and clock rules as `Take` but writes nothing. A peek is an
observation, not a reservation; a later `Take` can still be denied. A missing or
expired bucket reports full capacity and the reset of the window that would open.

`Clear` removes the key's bucket, including unreadable state at its address, so the
next request starts a fresh window. It grants new quota: use it deliberately (for
example after a successful verification), never as error recovery. `Attempt` takes
the cost and runs the callback only when admitted, returning the callback's error;
a denial or admission error never runs it. The callback runs in the caller's
goroutine after the bounded admission operation has finished, and consumed capacity
is not refunded when it fails.

Inspection is the optional `ratelimit.InspectBackend` capability. Memory and Redis
implement it; a custom `Backend` without it keeps working for `Take`/`Allow`, and
`Peek`/`Remaining`/`AvailableIn`/`Clear` return `fault.Invalid`. Adapters validate
inspection output with `Decision.ValidatePeek`.

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
removed. IPv4 is keyed per address and IPv6 per /64 network; configure other
prefixes with `foundryhttp.RateLimitByIPWith` (see [HTTP middleware](http-middleware.md)).
An invalid peer with no attribution returns 400. Install `TrustedProxy`
before the IP limiter when forwarded attribution is needed; assembly rejects the
reverse order, and the limiter never reinterprets untrusted forwarded headers itself.
Handlers that call a limiter directly return `foundryhttp.RateLimitExceeded(decision)`
to send the same 429 headers.

Every admitted or denied response gets `X-RateLimit-Limit`,
`X-RateLimit-Remaining`, and `X-RateLimit-Reset`. Reset is seconds until the window
ends, rounded up, matching Rust Foundry's duration convention. These X headers are
Foundry compatibility metadata, not a claim of standardized rate-limit headers.
Confirmed denial returns the existing JSON `rate_limited` error (429) plus
`Retry-After` in rounded-up whole seconds, as defined by
[RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html#section-10.2.3).

Backend failure, local operation saturation, canceled admission and callback
panic/Goexit return the existing `unavailable` response (503). Explicit
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
phase, window end and admitted usage. Type, canonical integers, range, phase
alignment and absolute expiry are checked before mutation. `SET ... PXAT` writes
usage and expiry together; there is no separate increment/expiry step that can
leave persistent unbounded counters. `PEXPIRETIME` verifies the stored expiry
matches metadata. Wrong types, oversized values and malformed state fail without
replacement; `Clear` is the explicit repair path for such an address. Buckets
written by earlier releases (version 1, epoch-aligned) remain valid: they keep
their usage and expiry and are rewritten in the current format on the next
admission. A backward server clock step inside a live bucket is clamped to that
bucket's start: it neither fails nor reopens earlier quota. Peek runs the same
script without writing.

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
A burst beyond the active-operation bound waits in FIFO order for a slot, within
the operation deadline (at most five seconds), then fails with retryable
`fault.Overloaded`, which HTTP maps to 503 with `Retry-After`.
Namespaces and keys reuse `keyspace`; rate addresses are distinct from cache and
lease addresses. Encoded keys are hashed only after byte/UTF-8/control checks.

The active slot includes resolver, codec and backend execution. Panic and Goexit
are caught without exposing panic payloads. A callback that ignores cancellation
retains its slot until it actually exits: the timeout is cooperative and does not
guarantee forced return from arbitrary Go code. Saturation that outlasts the
admission wait returns an error; capacity is never reclaimed while its callback is
still running.

For deterministic tests, use `memory.New(maxEntries, clock)` and the same Store
and declarations. The injected clock must return promptly. Deadlines still use
real context time. The memory authority clamps a backward clock reading to the
latest reading it has observed (so it can neither reopen quota nor fail), performs
constant-time hot-bucket checks, and keeps buckets in an expiry-ordered heap: when
a new key needs capacity it reclaims expired buckets in end order in amortized
logarithmic time instead of scanning the table. It never evicts a live bucket to
admit another key; a table full of live buckets returns `fault.Conflict`. Closing
releases its state.

Shared memory/real-Redis tests cover weighted decisions, exact maximum counts,
concurrency, policy conflicts, cancellation and independent resources. Additional
tests cover window boundaries, namespaces, corrupt state, callback ownership,
uncertain acknowledgements, HTTP behavior, wrong Go types and real gopls discovery.
Full native verification passed in 465.6s with 1984 matching source inputs, required local PostgreSQL and Redis, all 670 compiler cases, 245 consumer editor scenarios plus five field-documentation scenarios, root/consumer tests, vet/formatting, three generation-freshness targets and documentation checks. Fresh focused rate-limit/Redis/HTTP races, consumer races, five new compiler rejections, two actual gopls probes and 2,861,333 timestamp fuzz executions also passed. No dependency was added.
See the [acceptance record](../../blueprint/00-master-architecture-and-parity.md#milestone-09-typed-rate-limit-acceptance). Milestone 09 still requires pub/sub before milestone 10 begins.
