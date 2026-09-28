# Idempotent operations

T06 native acceptance passed. The [evidence](../evidence/typed-api-t06.json) records
commands, source hashes, process/crash coverage, fuzzing and repeated measurements.

Enable `settings.Features.Idempotency.Enabled`. The feature uses the configured
application/environment namespace and its named database, or the database default
when its name is omitted. `services.Idempotency()` resolves the owned store.
Its migrations join `app.Migrations()` and the existing explicit migration runner.
`Settings.WithDatabaseScopes` also scopes this feature for complete HTTP tests.
A store never migrates or prunes automatically.

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
| Duplicate lock wait expires | Declared 409 in progress, bounded `Retry-After`; same key on retry |
| Admission quota/capacity exhausted | Declared 429; no committed new claim |
| Business/encoding/storage failure before commit | Claim, business and outbox roll back together |
| Commit acknowledgment unknown | Reconcile on primary or declared 503; never rerun immediately |
| Known commit with later operational failure | Preserve/publish committed result and report the failure |
| Current authorization denies replay | Current safe 401/403; no protected stored response |
| Stored schema/hash/representation invalid | Declared 503; no automatic re-execution |

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
The database and HTTP kernel add their existing independent bounds. Admission is
nonblocking locally; caller quotas serialize new operations through a bounded
transaction-scoped advisory lock across instances. Hash collisions can reduce
concurrency but never merge operation addresses. Existing replays do not need quota.
Callbacks must honor cancellation; Go cannot forcibly stop application code while
safely releasing resources it still owns.

The result envelope contains the exact body as bytes, so its storage budget includes
base64/envelope overhead in HTTP mode. Large-response workloads should choose
explicit limits and measure their shape. Input canonicalization uses the shared
bounded JSON parser, its decimal limit, depth 64 and 10,000-node ceiling.

Expiry establishes when explicit maintenance may remove a completed result.
`store.Prune(ctx, limit)` targets at most 1,000 completed, expired rows in this
application namespace and skips locked rows. It never removes a live uncommitted
claim. Expired records continue to replay and count toward caller quota until
pruned. No background pruning is enabled. Repository tests retain all data.
After removal, the same key can execute again; this is one committed local outcome
per **retained scoped key**, not exactly-once network delivery or external effects.

Changing normalization or wire schemas requires an operation-version decision.
A changed input schema/fingerprint conflicts with an old key; an incompatible
stored result is unavailable. Changing operation version creates a new address and
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
bounds claim-lock lifetime and includes waiting and callback work; it is not an
isolated PostgreSQL lock timer. The complete native gate passed. T07 still owns the
integrated T01–T06 audit, improvements and final verification.

Three native local PostgreSQL samples measured the following medians for one
business INSERT and a 15-byte core result. These are runner microbenchmarks:

| Mode | Time per iteration | Allocated bytes | Allocations | SQL commands/request | Transaction residence/request |
| --- | --- | --- | --- | --- | --- |
| New operation | 1.007 ms | 112,502 | 1,822 | 22 | 933.5 microseconds |
| Replay | 0.581 ms | 79,199 | 1,260 | 12 | 509.6 microseconds |
| Contended pair | 3.021 ms | 193,436 | 3,098 | 17 | 2,282 microseconds |

The contended iteration contains two requests and a deliberate one-millisecond
winner hold. Samples use fresh key namespaces so repeats never mislabel replays
as new work. Schema/savepoint management contributes to command and allocation
cost; applications should measure their own DTO and callback sizes.
