# Typed login lockout

Typed failed-attempt policies use atomic Redis or an explicit local authority. Password and MFA throttles remain separate from HTTP request-rate limits.

Milestone 10 passed native acceptance. See the [master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-10-acceptance) for verification evidence and later integration boundaries.

## Configure the account's login policy

```go
var PasswordAttempts = lockout.DefineLogin(
    "accounts.password",
    keyspace.StringKeys[Email](),
    lockout.DefaultLimits(),
)

// Reuse the application's existing Redis client and namespace.
store, err := lockout.NewStore(redisClient, lockout.DefaultConfig(namespace))
// Check err before binding.
throttle, err := PasswordAttempts.Bind(store)
// Check err and keep the protected view returned by WithLockout.
login, err = login.WithLockout(throttle)
```

The throttle key must match the login's concrete input key type. Use one canonical
key for lockout and model lookup, including tenant/provider identity where needed.
For case-insensitive email lookup, normalize through the same domain constructor
before both operations; do not normalize only inside a database lookup callback.
A family name scopes one provider and credential stage. Reusing a declaration
shares its state; a distinct declaration with an already-bound name is rejected.
Missing and existing accounts use the same key construction and policy.

## Client-aware limits

`PasswordLogin.WithLockout` requires a `lockout.DefineLogin` declaration and
rejects a single-key `lockout.Define` throttle with `fault.Invalid`. Keyed on the
account alone, anyone who knows an email address could keep that account locked
out forever. `lockout.Limits` therefore counts these windows per attempt:

| Window | Key | Default |
| --- | --- | --- |
| `PerClient` | account and trusted client IP | 5 failures / 15 min, lock 15 min |
| `Account` | account, any client | 50 failures / 15 min, lock 15 min |
| `Address` | client IP, any account | off; opt in with `value.Set(lockout.DefaultAddressPolicy())` (100 failures / 15 min, lock 15 min) |

An attempt is denied when any window is locked, with the longest retry delay.
The verifier runs once; a failure counts in every window. A success clears the
pair and account windows but never the address ceiling, so an attacker's own
valid account cannot reset its spraying budget. Each enabled ceiling must be at
least the per-client threshold. IPv6 clients are grouped per /64 network, like
`http.RateLimitByIP`.

The account ceiling bounds distributed guessing. Its tradeoff: an attacker who
spreads failures across many addresses can still lock one targeted account for
`LockFor`. Lower the risk with request rate limits and by keeping the ceiling well
above what one user produces.

The address ceiling is off by default because every user behind one shared
address (carrier-grade NAT, a corporate proxy or VPN) shares it: once it locks,
all of them are refused, including correct passwords. With misconfigured trusted
proxies every request appears to come from the proxy, and one ceiling would lock
out the whole site. Enable it only when client addresses are trusted and rarely
shared, and prefer `http.RateLimitByIP` for credential spraying.

The client IP is the trusted request address that the Foundry HTTP server records
in request attribution, after its [trusted proxy](http-trusted-proxy.md) rules.
Behind a proxy, configure those rules, or every attempt appears to come from the
proxy, which merges every user's pair window for an account. Attempts without
request attribution (CLI, workers) share one `unknown` client per account and skip
the address ceiling. Logical keys use distinct
prefixes and fixed-size digests under the family name, so they cannot collide.

The [consumer binding](../../tests/fixtures/consumer/passwords/lockout.go) uses the
existing password provider/query and one `WithLockout` call. Applications do not
coordinate counters, expiry, clearing or late-result checks themselves. Plain
`PasswordLogin` remains available for explicitly controlled integration paths;
consume the protected return value when enabling this feature.

## Admission and completion

Defaults permit five completed failures in a 15-minute window, then lock for 15
minutes. The window begins at its first admitted attempt. Durations are whole
milliseconds between 1 ms and 24 hours; thresholds are bounded at 10,000 failures.
Denials do not extend a lock. When a window or lock expires, the next admitted
attempt starts a fresh generation.

The backend checks both admission and completion. A callback that verified the
right password still cannot publish an authenticated result if another attempt
locked the account while it was running. Completing after window expiry or an
explicit reset returns `lockout.Expired`, with no result; retry the attempt.
Success clears only the failures present when that attempt started. Later
failures remain, even when their completion races a correct password.

Lockout counts completed credential failures. Operational callback errors,
cancellation, panic and Goexit grant no authority and do not count as bad
credentials. Already-admitted attempts can run concurrently. Lockout is therefore
not a hard cap on in-flight hashing: combine the existing `http.RateLimitByIP`
(or another typed request limiter) before decoding/verification, plus the shared
hasher's capacity bound. Password successes and account resets never clear those
request quotas. The consumer test covers this composition.

For another credential stage, such as MFA, the public typed operation is:

```go
verified, err := throttle.Run(ctx, key, func(op context.Context) (bool, error) {
    return verifyFactor(op, submittedCode)
})
// Only a true result with no error may grant authority or issue credentials.
```

Do not issue credentials inside that callback: a final lock/expiry check remains.
A successful password producing a pending-MFA proof verifies only the password
stage. Use separate password and MFA declarations so it cannot clear MFA failures.
MFA enrollment, factor persistence and completion remain in milestone 10's next
implementation work; this API does not claim those capabilities are complete.

## Authority, failures and resource ownership

The Redis adapter uses the existing Foundry client and one bounded metadata value
per key. Each operation validates metadata and live policy before its final atomic
write. Failure count and lock expiry change together. Redis server time owns the
window; returned retry delays are relative to the decision time. A recorded clock
moving backward, corrupt metadata or a conflicting live policy fails closed.
Deploy consistent policy across instances; allow old windows to expire before
changing their policy, or perform an authorized explicit reset under the old one.

Network failures can leave a mutation's outcome unknown. Foundry never retries
that mutation, returns an authenticated result, or switches to local state.
`lockout.Unavailable` classifies backend errors. The HTTP adapter uses its shared
503 response for these errors, 429 plus a rounded-up `Retry-After` for a confirmed
`lockout.Locked` rejection, and 401 for an expired attempt. Ordinary context
cancellation/deadlines use the shared 503 `unavailable` response. Internal details and
submitted keys do not appear in those response bodies.

`lockout/memory` is an explicitly chosen single-process adapter with a bounded
entry count and injected clock. It never evicts a live window to admit another
account. The borrowing store owns bounded callback admission: a burst queues for
at most min(`Timeout`, 5s) and then fails with `fault.Overloaded` (HTTP 503). It
waits for actual callback exit, including after cancellation. Backend lifecycle remains with the
application; construction does no I/O and starts no background work.

Redis lockout state has Redis's persistence and eviction behavior. An eviction,
flush or nonpersistent restart can lose windows; this feature is not a durable
account suspension record. Configure the authority accordingly. Window generations
prevent an in-flight result from mistaking replacement state for its original
window, but cannot recreate deleted security history. The framework never flushes
Redis as part of lockout or testing.

## Recovery and observation

`throttle.Reset(ctx, key)` is an explicit administrative or account-recovery
operation. Authorize its caller. It clears that factor's current window and
invalidates outstanding attempts, without changing other provider/stage/request
quotas. For a `DefineLogin` throttle it clears the account ceiling and the pair
window of the current request's client; other clients' pair windows expire on
their own. Ordinary successful login uses the revision check instead of blind
clearing. `passwordreset.WithLockout(reset, throttle, key)` performs this reset
after each committed password reset (see [account recovery](account-recovery.md)).

`WithLockedObserver(func(context.Context, lockout.Notice[Key]) error)` attaches a
typed domain-event or notification callback to the confirmed transition that starts
a lock. `Notice.Key()` exposes the concrete key deliberately; routine formatting
and JSON omit it. Repeated denials do not emit repeated notices; a client-aware
attempt emits at most one notice even when several windows lock together. Observer failures
cannot undo committed lockout and remain causes of its rejection. Delivery is
in-process: a lost Redis response or process crash can lose the observation. It
does not claim outbox durability or exactly-once external notifications.
