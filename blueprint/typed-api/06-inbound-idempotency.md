# T06 — Inbound idempotent operations

Prerequisites: T01–T05. Status belongs to the
[master](../00-master-architecture-and-parity.md#typed-api-delivery).

## Scope and ownership

Add a reusable typed operation runner with PostgreSQL persistence and an HTTP
adapter. The first supported operation makes database writes and enqueues existing
outbox work in one concrete database transaction. The HTTP surface returns a bounded
JSON success or declared empty success. Streams, downloads, credential issuance,
cookie/session mutation, long-running external provider calls and arbitrary raw
handlers are outside its replay contract and must reject incompatible assembly.

This feature is not cache `Remember`, a distributed lease around an ordinary pool
handler, or a second outbox. A lock without durable outcome storage cannot handle a
crash after the business commit. A process-local after-commit callback cannot bridge
that gap either. Reuse Foundry's database transactions, generated persistence,
contract codecs, clocks, safe errors, migrations and existing outbox destinations.

The runner's callback receives `*database.Tx`, concrete input and context, and
returns the endpoint's concrete result plus ordinary error. The runner owns outer
commit/rollback; callback code uses the supplied transaction for business writes
and existing outbox enqueue. Do not silently wrap a callback that owns an independent
transaction or infer transactions from context. Go cannot prevent a callback from
using a separately captured pool; document and test the supported transaction-bound
path without advertising a guarantee for arbitrary external side effects.

## Identity, fingerprint and limits

Persist a unique address containing application namespace, operation ID and version,
tenant/caller scope and idempotency key. Scope is derived from trusted authenticated
identity; the caller cannot choose another actor's scope through the header. A
verified webhook delivery uses its configured provider/account/operation scope and
authenticated delivery ID. Provider signature verification precedes every claim or
replay; adapters can supply a trusted identity without weakening the ordinary flow.

Use typed operation/scope/key values with bounded, validated input. Never log raw
keys, credentials, full payloads or stored results. The store persists only the
metadata needed for deduplication/replay and a bounded result, with a documented
retention policy. Prefer a domain-separated cryptographic digest of the key for
indexing; key secrecy still depends on adequate key entropy and storage access.

Fingerprint the validated, prepared semantic input using the existing typed wire
contract plus operation/fingerprint version. Include path/query/body values and
explicitly declared operation-relevant inputs; exclude incidental request IDs,
credentials and trace headers. Define canonical object-key ordering, numeric
representation and collection order. Preserve omitted versus null versus zero.
Equivalent object order/whitespace produces the same fingerprint; different
meaningful input produces a mismatch. Do not implement a competing JSON parser.
Normalization changes or wire-schema changes require a fingerprint/operation
version decision and a documented rollout; they must not silently alter old keys.

Bounds cover key length, input/result bytes, transaction duration, duplicate wait,
active operations and per-caller admission. Reuse rate limiting where configured.
An attacker must not create unlimited rows or hold unbounded waiting goroutines.

## Atomic protocol

1. Authenticate/verify, derive trusted scope, decode/prepare/validate and run current
   request authorization. Resource authorization needed for replay must also run
   before returning a stored result; never cache permission decisions.
2. On the primary connection begin a bounded transaction. Atomically insert the
   address and fingerprint under a unique constraint, or resolve the conflicting
   record. Claim, business writes, outbox enqueue and completed response are all
   in this same transaction.
3. The winning callback executes once for that committed outcome. Prepare and
   validate the full response using the existing response descriptor before commit;
   save its representation, allowed application headers, status and schema version.
   A serialization/storage error rolls back business effects and the claim.
4. Commit the completed record and business/outbox writes together. Only then
   publish HTTP success. A failed client socket write does not roll back a commit.
5. A concurrent duplicate waits only within the configured DB/request bound. Once
   the winner commits, read its outcome in an appropriate fresh statement/snapshot,
   compare fingerprints and replay or reject. If waiting times out, return the
   documented in-progress response; it must not execute a second callback.

PostgreSQL's [Read Committed behavior](https://www.postgresql.org/docs/18/transaction-iso.html#XACT-READ-COMMITTED)
allows unique conflicts with concurrent rows not visible in the original statement
snapshot. Design insert/conflict/reload as an explicit protocol and prove it with
independent connections. A read-then-insert check is not an atomic claim.

No committed in-progress lease is needed for this initial transaction-bound mode:
process death before commit rolls back both claim and writes, while process death
after commit leaves the complete result. Do not introduce takeover of a live SQL
transaction after a wall-clock lease expires. Long-running asynchronous acceptance
returns a typed accepted result after enqueueing the existing outbox in this same
transaction; it does not hold the transaction open during delivery.

## Public outcomes and replay

| Situation | Required behavior |
| --- | --- |
| Missing/malformed required key | Safe declared 400; no claim or business callback |
| Completed, same scope/key/fingerprint | Replay original success status and application representation; no callback or second outbox row |
| Completed, same scope/key, different fingerprint | Declared 409 payload mismatch; no execution or stored payload disclosure |
| Concurrent claim exceeds duplicate wait | Declared 409 in progress with bounded retry guidance; original ownership remains intact |
| Business error, validation/encoding failure or known rollback | Roll back claim and business writes; return ordinary declared failure; a retry may execute again |
| Commit outcome unknown | Never immediately rerun callback; resolve on the primary with the same address, or return a safe retryable unavailable result |
| Commit known successful, later callback/cleanup failed | Keep the completed outcome and report the operational failure; do not delete the claim or repeat committed business work |
| Re-authentication/policy now denies access | Current safe auth/policy failure; never return the old protected result |
| Stored schema/version cannot be replayed safely | Safe unavailable/conflict outcome with operational visibility; never re-execute automatically |

Initial policy persists successful results only; domain failures are not cached.
Add replay policy/status/header metadata to the existing endpoint manifest and SDK
so generated clients expose the key and declared outcomes. Retries reuse a key only
for the same logical submission; no automatic fresh key per network retry. Store
application representation before compression and regenerate incidental transport/
security headers. Never replay `Set-Cookie`, authorization credentials or stale
request/trace IDs. Disable response mutation that would violate stored body/status
semantics or explicitly compose it before outcome capture.

Retention guarantees deduplication only while the completed record is retained.
Persist expiry at completion. Expiry/pruning or explicit operation-version changes
can permit a later execution; document this clearly. Any production pruning must
target only completed expired operation records with bounded work and concurrency
protection. It cannot remove an active transaction's claim. Test teardown still
retains all schema/data under repository policy.

## Acceptance

- Real independent HTTP clients submit the same key concurrently across two app
  instances: one committed business effect and outbox enqueue, consistent replay.
- Same key across callers/tenants/operations is isolated; changed input within the
  same scope conflicts. JSON order normalizes; omitted/null/value remain distinct.
- Failure injection before claim, during business writes, response preparation,
  commit acknowledgment and after commit/before socket write proves outcome rules.
  Include process termination for pre/post-commit recovery, not only goroutine mocks.
- No double execution during lock timeout, cancellation or unknown commit. A fresh
  process can replay a stored result through the real primary DB.
- Unauthorized replay, signature failure, version mismatch, oversized results and
  forbidden response modes are rejected without exposing private stored content.
- Native race/fuzz tests cover key/fingerprint parsing, persisted corruption and
  bounds; compiler/gopls/strict TS prove typed transaction/input/result contracts.
- Measure new, replayed and contended requests, DB round trips/lock duration, response
  storage and allocations. Document the guarantee as one committed local outcome
  per retained scoped key, not exactly-once network delivery or external effects.

Complete the common gate after the entire implementation/test/docs batch. T07 then
re-audits all new ownership, transaction and replay paths together.

## Concrete delivered implementation

The typed core is `idempotency.Define(store, definition, input, output)` and
`Operation[I,R].Run`; HTTP uses `endpoint.Idempotent(store, definition).Handle`
with the existing required-authentication and model-binding adapters. The configured
application exposes `Features.Idempotency` and `Services.Idempotency()`. Its
store/migrations join the shared persistence target list. Manifest format 4 owns
required key and replay metadata for OpenAPI and TypeScript. See the
[concrete consumer guide](../../docs/guides/idempotent-operations.md).

Implementation, tests and docs were completed before compilation. Native
acceptance, cost measurements and the complete gate passed; see the
[T06 evidence](../../docs/evidence/typed-api-t06.json). T07 remains outstanding.
