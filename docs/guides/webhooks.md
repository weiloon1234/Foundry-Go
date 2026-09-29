# Inbound webhook verification

Foundry verifies provider signatures before typed decoding and every idempotent
replay. `webhook.Standard` supports Standard Webhooks HMAC-SHA256 (`whsec_`)
and Ed25519 public verification keys (`whpk_`). `webhook.Stripe` supports Stripe's
`Stripe-Signature` format and takes its event ID from the authenticated JSON body.
`webhook.HMAC` verifies a configurable single-header HMAC signature, with
`GitHubConfig`, `ShopifyConfig` and `SlackConfig` presets.
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

Verification work is bounded per request: duplicate signatures count once, at
most eight distinct supported signatures are considered, each HMAC key is
computed once and compared in constant time, verification stops at the first
match, and at most eight Ed25519 checks (each hashes the complete body) run
before failing closed. Standard Webhooks delivery IDs accept visible ASCII
except `.` (it delimits the signed content) and `,`, so provider IDs such as
`msg_…` values and UUIDs are accepted.

## Provider HMAC schemes

```go
config := webhook.GitHubConfig("github") // or ShopifyConfig / SlackConfig
verifier, err := webhook.New(config, RepositoryAccountID(7),
    []secret.String{githubSecret}, clock.System{})
```

`HMACScheme` names the `SignatureHeader`, an optional `Prefix` (for example
`sha256=`), the `Algorithm` (`SHA256`, `SHA512`, or `SHA1` for legacy providers
only) and the signature `Encoding` (`Hex` or `Base64`). The signed content is the
exact body or, with `TimestampHeader`, `PayloadPrefix + timestamp + Separator +
body`, and the Unix-seconds timestamp is checked against `Tolerance`.

These schemes never sign a delivery ID, so `Delivery.ID` is always `sha256-`
plus the digest of the verified signed content (the body, with the signed
timestamp when there is one). Provider headers such as `X-GitHub-Delivery` are
unauthenticated: set `DeliveryHeader` to expose one as
`Delivery.UnverifiedProviderID` for diagnostics, never for deduplication or
authorization. Without a signed timestamp (GitHub, Shopify) no freshness check
is possible, and the content-digest ID protects only against replays of the
identical body: durable idempotency rejects a captured request replayed with a
fresh delivery header, but a provider's legitimate resend of an identical body
is also treated as a duplicate. Prefer a scheme with a signed timestamp when the
provider offers one. Secrets are used verbatim as HMAC keys and must be at least
16 bytes. The scheme must be zero for the Standard and Stripe protocols.

| Preset | Signature header | Signed content | Delivery ID (unverified header) |
| --- | --- | --- | --- |
| `GitHubConfig` | `X-Hub-Signature-256: sha256=<hex>` | body | body digest (`X-GitHub-Delivery`) |
| `ShopifyConfig` | `X-Shopify-Hmac-Sha256: <base64>` | body | body digest (`X-Shopify-Webhook-Id`) |
| `SlackConfig` | `X-Slack-Signature: v0=<hex>` | `v0:{X-Slack-Request-Timestamp}:{body}` | signed-content digest (none) |

Outbound signed delivery lives in `webhook/outbound`; see
[sending webhooks](#sending-webhooks).

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

## Sending webhooks

`webhook/outbound` sends signed webhooks to registered endpoints through typed
jobs and the transactional outbox, and keeps a delivery log.

```go
var InvoicePaid = outbound.DefineEvent("invoice.paid", InvoicePaidJSON())

service, err := outbound.New(outbound.Dependencies{DB: db, Keys: keyring,
    Client: restrictedClient, Clock: clock.System{}}, outbound.DefaultConfig())
job := outbound.DefineDeliveryJob("webhooks.deliver", jobs.DefaultPolicy("webhooks"))
declaration, err := job.Declare(service) // register with the job registry
sink, err := job.FailureSink(service)    // register with every worker running it
queue, err := job.ToOutbox(service, producer)

endpoint, signingSecret, err := service.CreateEndpoint(ctx, outbound.EndpointOptions{
    URL: customerURL, Events: []outbound.EventType{"invoice.paid"},
})
// Inside the business transaction:
id, err := outbound.Send(ctx, tx, queue, endpoint, InvoicePaid, payload)
ids, err := outbound.Publish(ctx, tx, queue, InvoicePaid, payload) // fan-out
```

Apply `outbound.Migrations()` in the same schema as the job outbox;
`outbound.Module` owns the service in foundation assembly and drains it on
shutdown after workers stop. Events are
typed by their generated JSON contract; the body is the Standard Webhooks
envelope `{"type":…,"timestamp":…,"data":…}` encoded once, bounded by
`MaxPayloadBytes` (256 KiB by default, at most 1 MiB), and stored with the
delivery so every attempt and replay sends identical bytes.

**Endpoint registry.** Endpoints (at most `MaxEndpoints`) store a URL, the
event types `Publish` fans out to, and an active flag. The service requires an
`httpclient.Client` with a restricted destination policy
(`RestrictsDestinations`); each URL is checked against that policy when it is
registered and again for every send, so customer URLs cannot reach internal
services. `Send` targets one active endpoint; `Publish` targets every active
subscribed endpoint, each with its own delivery. Both only write the delivery
log and enqueue the job in the caller's transaction: a rollback sends nothing.

**Signing and secrets.** `CreateEndpoint` returns the first secret
(`whsec_` + base64 of 32 random bytes) once; only its encryption envelope is
stored, bound to the endpoint and secret record with the application's
`encryption.Keyring`. Each attempt sends `webhook-id` (`msg_` + the delivery ID,
stable across retries and replays), `webhook-timestamp` and `webhook-signature`
with one `v1,<base64 HMAC-SHA256>` of `id.timestamp.body` per signing secret,
verifiable by `webhook.Standard` above. `RotateSecret(ctx, endpoint)` adds a new
current secret; previous secrets keep signing alongside it for
`Config.SecretGrace` (24 hours by default). `RotateSecretWithGrace(ctx, endpoint,
grace)` takes an explicit overlap, and zero revokes previous secrets at once. At
most `MaxSigningSecrets` sign at a time.

**Delivery and retries.** The job sends one attempt with no client retries and
records it. A 2xx status succeeds; the response body is drained up to 16 KiB and
never stored, so an accepted delivery with a large response is not resent.
Transport failures, timeouts, 408, 429, 5xx and failures before sending
(`store_failed`, `signing_unavailable`) are retried with the job policy's
backoff; each attempt is logged and the delivery stays `pending`. Other
statuses, a denied destination and an inactive endpoint fail without retry. The
job runtime alone decides when retries end (attempts, retry deadline, exception
limit, timeouts): register `job.FailureSink(service)` with every worker that runs
the job (`jobs.WithFailureSink` or `jobs.RegisterFailureSink`) so terminal
failures are recorded as `failed`, keeping the last attempt's category.
Without the sink such deliveries stay `pending`; sinks run at most once, so an
operator can still find them with `webhooks deliveries --state pending`. A
duplicate job never resends a succeeded delivery; receivers still deduplicate by
`webhook-id`, because a retry can follow an unknown outcome.

**Delivery log, replay and retention.** `foundry_webhook_deliveries` records the
event, state (`pending`, `succeeded`, `failed`), attempts, last HTTP status and a
stable failure category (`status_retryable`, `status_rejected`,
`transport_failed`, `timeout`, `destination_denied`, `endpoint_inactive`,
`signing_unavailable`, `job_<reason>`, …) — never response bodies, error text or
secrets. It also stores each payload so retries and replays send identical
bytes; keep payloads free of secrets and prune finished rows.
`service.Deliveries` inspects the log without payloads. `queue.Replay(ctx, id)`
and `queue.ReplayFailed(ctx, endpoint, limit)` return deliveries to `pending`
with the same webhook-id and enqueue them; `ReplayFailed` skips inactive
endpoints (naming one explicitly is a conflict) and counts committed replays
only. `service.PruneDeliveries(ctx, olderThan, limit)` deletes up to
`MaxPruneBatch` succeeded or failed deliveries, with their payloads, last
updated more than `olderThan` (at least one hour) ago; pending ones are never
pruned. Run it from an ordinary scheduled task. The `webhooks` command
(`command.Declaration`) exposes `webhooks deliveries [--endpoint id] [--state
failed] [--limit 50] [--format json]`, `webhooks replay --delivery id | --failed
[--endpoint id] [--limit 100]` and `webhooks prune --older-than 720h [--limit
1000]`, which repeats batches until fewer than the limit remain.
