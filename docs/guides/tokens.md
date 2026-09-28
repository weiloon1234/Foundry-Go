# Typed personal and renewable tokens

PostgreSQL-backed hashed personal and renewable tokens retain model-owned IDs, bounded lifetimes and immutable scope ceilings. Refresh rotation detects reuse and invalidates the family.

Milestone 10 passed native acceptance. See the [master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-10-acceptance) for verification evidence and later integration boundaries.

## Framework-owned persistence

`auth/token` binds one stored model/provider/guard and its declared scope ceiling.
`auth/token/postgres` borrows an existing database pool. Apply its `Migrations()`
explicitly in the same schema as its `Config.Schema`; construction does not connect
or migrate. Register the binding's `Guard()` once alongside other auth declarations.
The [consumer example](../../tests/fixtures/consumer/authenticating/tokens.go)
shows independent use verified by the milestone gate.

```go
backend, err := tokenpg.New(db, tokenpg.DefaultConfig())
// Check err.
store, err := token.NewStore(backend, token.DefaultConfig(namespace))
// Check err.
api, err := token.New(store, "users.api", users, "users.bearer", declaredScopes)
// Check err and register api.Guard().Registration().
```

Generated internal models own subject locks, token families and retained token
generations. Operational queries use the ordinary typed ORM. Only explicit
migration DDL and schema selection require SQL infrastructure statements. Subject
keys share the existing session identity serialization without changing persisted
session addresses. Sessions and tokens have distinct tables and public hash types.

## Issue only after trusted verification

```go
issued, err := api.Issue(ctx, verifiedProof, token.IssueOptions[User]{
    Name: "My phone", Scopes: grantedScopes, Refresh: true,
})
```

Proofs retain both model and stored key types. `NewProof` is a trusted adapter
boundary, not password verification. A submitted ID must never become a proof.
Issued scopes must fit the binding's declaration and, for scoped input proofs, the
input grant. An undeclared or expanded scope fails before persistence. Current
model eligibility and policies remain authoritative at each new auth scope.

Without `Refresh`, issuance creates a fixed-expiry personal token. Fully
authenticated issuance with `Refresh` creates a renewable pair. Pending MFA can
only issue a short, fixed-expiry challenge with no scopes or refresh capability.
Ordinary authenticated handlers reject it.

`Issued.Info()` exposes immutable metadata. `AccessSecret()` and optional
`RefreshSecret()` are explicit sensitive transport boundaries. Routine formatting
and JSON never disclose either raw secret. Do not put credentials in URLs. HTTP
delivery uses the framework descriptor below; applications should not mass-serialize
stored models or credential records into response DTOs.

## Typed HTTP delivery

```go
response := foundryhttp.TokenResponse[User, model.ID[User]](200, clock.System{})
refresh := foundryhttp.DefineEndpoint(
    foundryhttp.DefineRoute(foundryhttp.RouteSpec{
        ID: "auth.refresh", Method: foundryhttp.POST, Access: foundryhttp.Public,
    }, foundryhttp.StaticPath("/auth/refresh")),
    foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenBody(), response,
)
registration := refresh.Handle(func(ctx context.Context,
    input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshTokenRequest],
) (token.Issued[User, model.ID[User]], error) {
    return api.Refresh(ctx, input.Body.RefreshToken.Secret())
})
```

The [independent consumer](../../tests/fixtures/consumer/authenticating/token_routes.go)
shows login, refresh, scoped bearer authentication and concrete handler models.
These examples and tests passed the milestone gate. Handlers return
`token.Issued[User, ID]`; a different model or key cannot satisfy the response type.
Foundry performs explicit JSON disclosure only through `TokenResponse`, preserving
ordinary redacted serialization of both issuance and incoming refresh credentials.

The success shape is:

```json
{
  "tokens": {
    "access_token": "<opaque credential>",
    "refresh_token": "<opaque credential>",
    "expires_in": 900,
    "token_type": "Bearer"
  },
  "mfa_required": false
}
```

Personal and MFA-pending credentials omit `refresh_token`. Pending credentials set
`mfa_required: true` and still cannot satisfy ordinary authenticated handlers.
`expires_in` contains remaining whole seconds at preparation, rounded down; a
subsecond remainder yields zero. The backend expiry is authoritative. The injected
HTTP and backend clocks must agree. Future-issued, missing or expired values fail
before a success body is committed. Storage commit and HTTP delivery remain separate.

This descriptor requires POST and HTTPS. Behind TLS termination, configure the
existing `TrustedProxy` middleware with explicit peer networks and origin headers;
untrusted forwarding headers never satisfy HTTPS. Responses reaching this boundary,
including its failures, use `Cache-Control: no-store` and `Pragma: no-cache`.
The normal bounded JSON/handler/error pipeline owns parsing, cancellation and writes.
No alternative response writer or framework-managed public login route is introduced.

`RefreshTokenBody` accepts a JSON `refresh_token`, rejecting unknown, duplicate,
missing, null or malformed values. Its typed value redacts formatting and JSON;
`.Secret()` supplies `Tokens.Refresh` without application string conversion. Use
`EmptyQuery` so query-string credentials are rejected. Bearer authentication uses
`Authorization` only and never falls back to a URL token. This is native token
transport; browser sessions use their separate cookie/CSRF adapter.

Generated internal DTO declarations are the runtime and manifest source of truth.
Response metadata describes the wire payload, not the private fields of Issued.
The request type aliases expose the same descriptor without a copied schema.

## Refresh and reuse

```go
next, err := api.Refresh(ctx, refreshSecret)
```

Each successful refresh retains the family's ID, original absolute deadline and
grant. It consumes the old generation and creates both new hashes atomically.
Old access tokens stop authenticating. Consumed refresh hashes remain available
to detect reuse; replay revokes the entire family, including its current successor.
That revocation commits before the public refresh call reports unauthenticated.

Refresh rotates credential storage; it does not itself hydrate the subject model.
Every ordinary guard authorization still resolves current provider eligibility, so
a disabled/deleted subject cannot use a refreshed credential to authenticate.

Clients must serialize refresh. Concurrent requests have at most one success, and
reuse detection then invalidates that successor. No grace period or automatic
retry is provided. A failed or uncertain commit never returns partial credentials;
a client may need to authenticate again. Database commit and network delivery are
separate, so a lost response can leave a credential that was never delivered.

History is bounded by a persisted rotation limit (default/hard maximum 4,096).
Reaching it requires authentication again and removes the exhausted family.
Expiry and revocation remove all retained generations. The rotation/reuse behavior
follows the principle in [RFC 9700](https://www.rfc-editor.org/rfc/rfc9700.html#section-4.14.2);
these personal tokens are not a complete OAuth authorization server.

## Lifetimes and maintenance

Defaults: personal tokens 30 days; renewable access 15 minutes, refresh idle seven
days, absolute family lifetime 30 days; pending MFA five minutes. Configuration
requires bounded microsecond-precision durations. Existing families preserve their
issued policy when configuration changes. Changing the declared scope ceiling
causes credentials carrying removed scopes to fail verification.

Guard lookup is read-only. `Touch(ctx, accessSecret)` explicitly records activity
without extending access, refresh-idle or absolute expiry. Only a successful
refresh renews idle expiry, always capped by the original absolute deadline.
Instances sharing persistence require synchronized clocks; tests inject one clock.

`List(ctx, user.FoundryReference())` returns typed metadata for current live families,
including families whose access token expired while refresh remains live. Authorize
inspection of another subject with an ordinary model policy. `RevokeID` requires
both its model-owned family ID and the matching subject; `RevokeAll` serializes
with issuance. `Revoke` accepts the current access secret and removes its family.

`Prune` removes at most 16 expired families per call, including bounded generation
history. Candidate selection uses current generations, so an expired historical
refresh record cannot starve cleanup or prune a live successor. Candidates are
rechecked under stable subject locks in deterministic order. Small subject lock
rows remain after family deletion to avoid races with new issuance.

These behaviors passed the milestone tests and consumer review.


## Credential changes and checked issuance

The [credential-change integration](credential-changes.md) is verified. `Revocation()` contributes this guard to a typed group; `RevokeAllIn`
joins a caller-owned model transaction without committing. Checked password proofs
lock and revalidate their current model before issuance in the same transaction.
The complete milestone gate covers this integration.

## MFA completion

`CompleteMFA` passed the milestone gate. Pass a
model-bound verifier from `Factors.Verifier`; it consumes a live pending credential
and factor in the same transaction that creates the authenticated replacement.
The original guard/provider and current model eligibility remain authoritative.
See [MFA completion and HTTP delivery](mfa.md#complete-a-pending-login) for the
actual API, failure behavior and transport composition.
