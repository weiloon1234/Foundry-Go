# Inbound webhook verification

Foundry verifies provider signatures before typed decoding and every idempotent
replay. `webhook.Standard` supports Standard Webhooks HMAC-SHA256 (`whsec_`)
and Ed25519 public verification keys (`whpk_`). `webhook.Stripe` supports Stripe's
`Stripe-Signature` format and takes its event ID from the authenticated JSON body.
The same `whsec_` prefix has different key semantics in these protocols; select
the provider's actual protocol rather than guessing from its key prefix.

```go
verifier, err := webhook.New(
    webhook.DefaultConfig("billing", webhook.Standard),
    BillingAccountID(42),
    []secret.String{currentKey, previousKey},
    clock.System{},
)
// Handle err. Attach before any body-transforming middleware:
endpoint = endpoint.WithMiddleware(http.VerifyWebhook(verifier))
```

Account IDs retain their concrete Go type. The configured account/provider are
trusted routing declarations: use separate endpoint keys for separate accounts.
Never choose an account or verification key from an unsigned header. Rotations
accept up to eight configured keys; construct a new verifier with a new snapshot
and retire old keys deliberately. Keys/signature bytes are never diagnostic text.

The default freshness tolerance is five minutes in either direction and can be
configured between one second and one hour. Body limits default to 1 MiB and
cannot exceed 16 MiB; signature/header counts and sizes are bounded. Original
bytes are verified and restored unchanged for downstream typed decoding. The
HTTP adapter requires POST and rejects content encodings; verification must run
before decoding, normalization or decompression. Invalid/repeated signature
headers, ambiguous IDs and duplicate Stripe JSON keys fail closed. Stripe JSON
is additionally bounded to 64 levels and 10,000 nodes. Repeated timestamps are
rejected even when the first value is empty.

The HTTP adapter returns the shared error envelope: 401 for signature/freshness
failure, 413 for body limits, 400 for unreadable bodies, 408 for cancellation or
deadline expiry, and 500 for internal verification failure. Verifier formatting
redacts both pointers and copied values.

`verifier.FromContext(ctx)` returns a `webhook.Delivery[BillingAccountID]` from
that exact verifier. Its `Account`, `Provider`, `ID` and `Timestamp` methods expose
verified metadata without turning it into a global service container. Metadata
is not current resource authorization; apply ordinary account/resource policy.

## Durable duplicate handling

The HTTP adapter replaces untrusted `Idempotency-Key` with a stable digest of the
verified provider and delivery ID. Scope the existing idempotency operation using
both provider and typed account. A valid redelivery is authenticated again and
then can replay the existing committed result. Freshness does not replace durable
deduplication, and no process-local webhook replay store is introduced.

The [compiling PostgreSQL consumer](../../tests/fixtures/consumer/idempotenthttp/publication_test.go)
uses the real middleware, checks expiry/invalid signatures on replay, and proves
that changing an unsigned key cannot duplicate a database effect. Reuse the
[transactional outbox](outbox.md) for required background effects.

Non-HTTP adapters can call `Verify(ctx, headers, originalBody)` directly. The
returned delivery proves only those bytes; do not deserialize different bytes or
reuse a previous delivery as authorization for a new request.

Protocol references: [Standard Webhooks](https://github.com/standard-webhooks/standard-webhooks/blob/main/spec/standard-webhooks.md),
[Stripe signature verification](https://docs.stripe.com/webhooks/signature).
