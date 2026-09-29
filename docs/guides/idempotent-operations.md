# Idempotent operations

T06 native acceptance passed. The [evidence](../evidence/typed-api-t06.json) records
commands, source hashes, process/crash coverage, fuzzing and repeated measurements.

Enable `settings.Features.Idempotency.Enabled`. The feature uses the configured
application/environment namespace and its named database, or the database default
when its name is omitted. `services.Idempotency()` resolves the owned store.
Its migrations join `app.Migrations()` and the existing explicit migration runner.
`000002_index_retained_outcomes` replaces the caller index with
`(namespace, scope_digest, expires_at)` using [nontransactional](migrations-and-seeding.md#concurrent-indexes-and-nontransactional-statements)
`CREATE/DROP INDEX CONCURRENTLY`, so it does not block writes to a large table.
The build waits for transactions older than itself, so the migration raises its
session `lock_timeout` to 300 seconds for these statements and then restores the
runner's value. It first runs `DROP INDEX CONCURRENTLY IF EXISTS
foundry_idempotency_retained`, so a leftover from an earlier failed run is
rebuilt, and drops the old caller index only `IF EXISTS`. If the build itself
fails (for example a lock timeout behind a long transaction), PostgreSQL can leave
an INVALID `foundry_idempotency_retained` and the runner records an uncertain
statement: drop the index with `DROP INDEX CONCURRENTLY IF EXISTS
<schema>.foundry_idempotency_retained`, reconcile that statement as
`migrate.StatementNotApplied`, then run `Up` again.
`Settings.WithDatabaseScopes` also scopes this feature for complete HTTP tests.
A store never migrates automatically. The application module starts its expiry
pruner (see [Atomicity and bounds](#atomicity-and-bounds)); a standalone
`idempotency.New` store prunes only after an explicit `Start`.

Adapt an existing typed endpoint after declaring its decoding, normalization,
validation and authorization:

```go
operation := endpoint.Idempotent(store, idempotency.Definition{
    ID: "orders.create", Version: 1,
})
route := operation.Handle(
    func(ctx context.Context, in OrderRequest) (idempotency.Scope, error) {
        actor, err := guard.Require(ctx)
        if err != nil { return idempotency.Scope{}, err }
        return idempotency.NewScope(actor.Tenant, actor.ID.String())
    },
    func(ctx context.Context, tx *database.Tx, in OrderRequest) (OrderReceipt, error) {
        order, err := QueryOrders().Create(ctx, tx, OrderDraft{}.SetName(in.Body.Name))
        if err != nil { return OrderReceipt{}, err }
        receipt := OrderReceipt{ID: order.ID}
        _, err = OrderCreated.Enqueue(ctx, tx, outbox, receipt)
        return receipt, err
    },
)
```

Names in this short example are application declarations. The complete
[independent consumer](../../tests/fixtures/consumer/idempotenthttp/application.go)
uses required authentication, a bound workspace, its current resource policy,
generated drafts, typed DTOs and the existing outbox. The authenticated adapter
passes the concrete actor to both callbacks. The model-binding adapter additionally
passes its concrete bound request. `Prepare` is the lower-level composition point
for resolving current resources and returning a transaction callback; it runs on
every request, even when the stored success will be replayed.

Use the supplied transaction for **all** business writes and durable dispatch.
The callback never owns outer commit. Captured pools, external provider calls and
process-local callbacks cannot join this guarantee. Long external work belongs in
an existing outbox job; return a typed accepted response after enqueueing it.

## Request and replay contract

Clients send one `Idempotency-Key` containing 16–256 ASCII letters, digits,
periods, colons, underscores or hyphens. A deployment may lower the maximum.
Use a random UUID or stronger random value. Length validation cannot prove entropy.
Create the key once per logical submission, and reuse it across network retries.

Scope comes from trusted tenant/caller identity. Never derive it from an arbitrary
client header. Use [webhook verification](webhooks.md) to authenticate provider signatures and
account/delivery identity before every claim or replay; the
[webhook test](../../tests/fixtures/consumer/idempotenthttp/publication_test.go)
uses the reusable production adapter. A signed URL by itself does not authenticate a
webhook body. Authentication, request policy and bound-resource policy run again
before a stored protected result can be returned. Their decisions are not cached.

The fingerprint covers prepared path, query and body values through the same
runtime codecs. Object order, whitespace and exact decimal spellings normalize;
array order and omitted/null/zero distinctions remain significant. Path/query/form
values use their existing typed URL codecs. Credentials, request IDs and trace
headers are excluded. Operation-relevant inputs must be declared in those typed
sources; use the core's explicit typed input codec for a non-HTTP adapter with
additional semantic inputs. Do not capture undeclared request state in a callback.

JSON/empty success responses on POST, PUT, PATCH and DELETE are supported.
Inputs may be JSON, URL-encoded forms or empty. Multipart/files/streams, credential
issuance and session mutation are outside this contract. Optional anonymous identity
has no implicit scope; use required authentication or a verified provider adapter.
The ordinary session adapter may authenticate, but mutation is disabled before
its mutation callback can run.

`WithHeaders` may prepare `Location`, `Content-Language` and `ETag` inside the
transaction. Values and duplicates are bounded/validated. Exact response bytes,
status and these application headers are persisted before commit. Compression and
incidental security/request headers are regenerated on delivery. Cookies,
credentials, framing and request/trace headers cannot be stored. Custom middleware
must explicitly call `PreservesIdempotentResponses()` to attest that it preserves
this contract; unknown wrappers reject Foundry assembly. Native wrappers outside
Foundry's assembly remain the caller's responsibility.

| Outcome | Behavior |
| --- | --- |
| Missing/invalid key | Declared 400; no callback or claim |
| Same retained scope/key/input | Original success representation; no second callback/outbox |
| Different meaningful input | Declared 409 mismatch; stored content stays private |
| Same scoped key still uncommitted after `DuplicateWait` | Declared 409 in progress, bounded `Retry-After`; same key on retry |
| Caller retention quota exhausted | Declared 429; no committed new claim |
| Local admission saturated (`fault.Overloaded`) | Declared 503 unavailable, bounded `Retry-After`; nothing started |
| Callback's own lock timeout | Declared 503 unavailable; everything rolled back; same key on retry |
| Business/encoding/storage failure before commit | Claim, business and outbox roll back together |
| Commit acknowledgment unknown | Reconcile on primary or declared 503; never rerun immediately |
| Known commit with later operational failure | Preserve/publish committed result and report the failure |
| Current authorization denies replay | Current safe 401/403; no protected stored response |
| Changed result contract accepts stored bytes | Original success representation (compatible evolution) |
| Stored hash/representation invalid or incompatible | Declared 503; no automatic re-execution |

The core `Operation[I,R].Run` returns `Result[R]` plus error. A result with
`Committed()==true` can accompany an operational error after commit or successful
reconciliation; report that error while preserving the outcome. `Value` retains R,
`Encoded` returns owned stored bytes, and diagnostics redact keys/scopes/results.
Raw request input is never persisted, only its domain-separated digest. Stored
response content still requires ordinary database access control and retention.

## Atomicity and bounds

The runner explicitly uses the primary at Read Committed. One transaction inserts
the unique claim, executes the callback and stores the complete result. A duplicate
uses `INSERT ... ON CONFLICT DO NOTHING`, then a **fresh statement** to load the
committed winner. There is no committed in-progress lease or live-transaction
takeover. Process death before commit rolls back; process death after commit leaves
the complete result. A failed socket write cannot undo that commit.

Defaults are 32 active operations per store instance, 1,000 retained records per
trusted caller across operations, 256 KiB semantic input/result envelopes, a
15-second transaction deadline, a 1-second duplicate wait and seven-day retention.
The database and HTTP kernel add their existing independent bounds. Local admission
queues in FIFO order for at most five seconds (and the request deadline), then
returns a retryable 503 without starting the operation.

`DuplicateWait` bounds only the claim's wait for an uncommitted winner of the same
scoped key. The runner restores the caller's `lock_timeout` before the callback
runs, so application lock waits are bounded by the transaction's statement timeout
and context instead; a lock timeout the callback configures itself rolls back and
returns 503, never 409 in progress. Nothing serializes one caller's operations with
each other, across keys or instances.

The caller quota counts **unexpired** committed outcomes after the new claim, using
a bounded indexed read of at most `MaxRetainedPerCaller` rows. It takes no lock:
concurrently executing new operations of one caller cannot see each other's
uncommitted claims, so retained outcomes can exceed the quota by at most that
caller's concurrently executing new operations (each instance admits at most
`MaxActive`). Existing replays do not need quota. Callbacks must honor cancellation;
Go cannot forcibly stop application code while safely releasing resources it still owns.

Infrastructure statements run inside the runner-owned transaction with
transaction-local settings rather than savepoint scopes: one statement captures
the caller's `search_path`/`lock_timeout` and applies the store schema and bounds,
one restores them before the callback, and completion selects the schema as the
transaction's last statement. Any failure rolls the whole transaction back.

The result envelope contains the exact body as bytes, so its storage budget includes
base64/envelope overhead in HTTP mode. Large-response workloads should choose
explicit limits and measure their shape. Input canonicalization uses the shared
bounded JSON parser, its decimal limit, depth 64 and 10,000-node ceiling.

Expiry establishes when a completed result may be removed. `store.Prune(ctx, limit)`
targets at most 1,000 completed, expired rows in this application namespace and
skips locked rows. It never removes a live uncommitted claim. `Config.PruneInterval`
(default ten minutes) schedules the store-owned pruner that `Start` and the
application module run: each sweep deletes up to `PruneBatch` (default 500) rows
per statement, at most 16 statements, using an ordinary admission slot. A failure
is logged once with a redacted diagnostic and retried at the next interval; it never
stops the application. `Close` stops the pruner and waits for a running sweep. Set
`PruneInterval` to zero to disable automatic pruning and schedule `Prune` yourself,
for example through the configured [housekeeping schedule](production-operations.md#housekeeping-schedule), which prunes on the scheduler
leader only while the store-owned pruner is disabled.
Expired records still replay until pruned, but no longer count toward the caller
quota. Repository tests use isolated schemas and namespaces.
After removal, the same key can execute again; this is one committed local outcome
per **retained scoped key**, not exactly-once network delivery or external effects.

Changing normalization or wire schemas requires an operation-version decision.
A changed input schema/fingerprint conflicts with an old key. A changed **result**
contract (encoding identity) does not make retained outcomes unavailable by itself:
replay decodes the exact stored representation with the current codec (and, over
HTTP, the current status and header policy) and returns it when that succeeds, so
compatible evolution such as an added optional field keeps working. An outcome the
current codec rejects remains unavailable until it expires and is pruned; Foundry
never treats it as expired or re-executes it early, because the committed callback
may already have written data or enqueued durable work. Use strict codecs (the
generated JSON descriptors validate required fields) so an incompatible outcome is
rejected rather than coerced. Changing operation version creates a new address and
can execute an old key again, so coordinate rollout and clients deliberately.
The core supports explicit versioned encoding for non-HTTP operations using
`JSONInput`, `JSONEncoding`, or a bounded adapter of existing codecs.

## Generated clients and verification

Manifest version 4 includes replay policy, required key bounds, allowed headers
and declared outcomes. OpenAPI exports the required header and `Retry-After`.
Generated TypeScript requests require a branded `IdempotencyKey`:

```ts
const key = idempotencyKey(crypto.randomUUID());
const request = { path: { workspace: "1" }, body: { name: "Order" }, idempotencyKey: key };
const receipt = await client.ordersCreate(request);
// Retry request unchanged if delivery is uncertain; do not generate another key.
```

`APIError` exposes the safe code/status and bounded retry delay. The client owns the
header and rejects conflicting raw-header overrides. Each generated operation also
exposes its declared `errorCode` union; JavaScript exceptions remain ordinary errors.

Acceptance sources cover independent app instances, real commits/outbox delivery,
current auth/resource policy, failures before response publication, lost commit
acknowledgments, process termination/restart, typed compiler/editor/client contracts,
bounded fuzzing and native costs. The cost harness reports fresh/replayed/contended
requests, SQL driver commands, scoped checkouts, outer transaction residence time,
stored bytes and allocations. Warm command/checkouts explain expected round trips;
these counts are not packet-level network measurements. Outer transaction residence
bounds the claim row's lock lifetime and includes waiting and callback work; it is
not an isolated PostgreSQL lock timer. The complete native gate passed. T07 still owns the
integrated T01–T06 audit, improvements and final verification.

Three native local PostgreSQL samples (300 iterations each, Apple M4 Max, warm
build cache) measured the following medians for one business INSERT and a 15-byte
core result. These are runner microbenchmarks. They ran while other builds shared
the machine, so compare the deterministic command counts and allocations rather
than wall time with the earlier samples (1.007/0.581/3.021 ms):

| Mode | Time per iteration | Allocated bytes | Allocations | SQL commands/request | Transaction residence/request |
| --- | --- | --- | --- | --- | --- |
| New operation | 1.325 ms | 99,954 | 1,601 | 13 (was 22) | 1,306 microseconds |
| Replay | 0.736 ms | 71,395 | 1,121 | 7 (was 12) | 721.1 microseconds |
| Contended pair | 4.335 ms | 172,527 | 2,737 | 10 (was 17) | 3,158 microseconds |

The contended iteration contains two requests and a deliberate one-millisecond
winner hold. Samples use fresh key namespaces so repeats never mislabel replays
as new work. The remaining savepoint pairs belong to the typed model writes for
the claim and completion; applications should measure their own DTO and callback sizes.
