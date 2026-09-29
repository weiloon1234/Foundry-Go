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
generations. Request authentication, listing, capacity enforcement, revocation
of many families and pruning use explicit schema-qualified SQL: bearer
verification is one joined read with no transaction or row lock, so parallel
requests never serialize on the subject. Other mutations use the typed ORM under
the subject lock. Subject keys share the existing session identity serialization
without changing persisted session addresses. Sessions and tokens have distinct
tables and public hash types. Migration `000002_token_device_refresh_history`
adds nullable device columns with `NOT VALID` checks (metadata-only, no scan under
an exclusive lock) and the consumed-refresh table; `000003_validate_token_state`
validates the checks in its own transaction without blocking reads or writes.

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

`MaxPerSubject` (default 32) caps a subject's live personal/renewable families per
guard. `Limit` selects `auth.RejectNew` (the token default), which fails issuance
with `auth.CredentialLimit` (HTTP 409 `conflict`), or `auth.EvictOldest`, which
revokes the oldest families so the new one fits. Challenge tokens never count
toward that cap: they have their own `MaxPendingPerSubject` (default 8) and a new
challenge always replaces the oldest. Expired families never count.

Issuance records display-only device metadata (`Info().Device()`): the trusted
client IP and the user agent, truncated to 512 bytes on a rune boundary, from the
request attribution. Refresh keeps the family's device. Device data never
authenticates or authorizes.

`Config.Prefix` (empty by default; lowercase letters, digits and underscores, at
most 16 bytes) is prepended to new access and refresh secrets, for example
`acme_pat_`, so secret scanners can recognize leaked tokens. Only the random part
is hashed: tokens issued before a prefix was configured keep working, and changing
the prefix never invalidates existing tokens.

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
The previous access token keeps authenticating until its own access expiry or
`AccessGrace` after the refresh, whichever comes first (default 30 seconds; zero
disables it; at most the renewable access lifetime), so requests already in flight
with it do not fail. Older access tokens stop authenticating. Only the current and
previous generations are kept as rows; older refresh digests move to a compact
consumed set in the same transaction. Replaying any consumed refresh token revokes
the entire family, including its current successor. That revocation commits
before the public refresh call reports unauthenticated.

Refresh rotates credential storage; it does not itself hydrate the subject model.
Every ordinary guard authorization still resolves current provider eligibility, so
a disabled/deleted subject cannot use a refreshed credential to authenticate.

Clients must serialize refresh. Concurrent requests have at most one success, and
reuse detection then invalidates that successor. The access grace does not apply
to refresh tokens, and no automatic retry is provided. A failed or uncertain commit
never returns partial credentials; a client may need to authenticate again. A
refresh whose commit completed is returned even when the operation deadline passes
immediately afterwards, because the old refresh token is already consumed.
Database commit and network delivery are separate, so a lost response can leave a
credential that was never delivered.

History is bounded by a persisted rotation limit (default/hard maximum 4,096).
Reaching it requires authentication again and removes the exhausted family.
Expiry and revocation remove all retained generations and consumed digests. The rotation/reuse behavior
follows the principle in [RFC 9700](https://www.rfc-editor.org/rfc/rfc9700.html#section-4.14.2);
these personal tokens are not a complete OAuth authorization server.

## Lifetimes and maintenance

Defaults: personal tokens 30 days; renewable access 15 minutes, refresh idle seven
days, absolute family lifetime 30 days; pending MFA five minutes. Configuration
requires bounded microsecond-precision durations. Existing families preserve their
issued policy when configuration changes. The binding's current scope ceiling
always applies: removing a scope from the declaration withdraws it from existing
tokens (verification, `List` and `Refresh` report the intersection) without
making them unusable. The stored grant is never widened.

Guard lookup is read-only. `Touch(ctx, accessSecret)` explicitly records activity
without extending access, refresh-idle or absolute expiry. Only a successful
refresh renews idle expiry, always capped by the original absolute deadline.
Instances sharing persistence require synchronized clocks; tests inject one clock.

`List(ctx, user.FoundryReference())` returns typed metadata for current live families,
including families whose access token expired while refresh remains live, in one
statement without taking the subject lock. Authorize inspection of another subject
with an ordinary model policy. `RevokeID` requires both its model-owned family ID
and the matching subject; `RevokeAll` serializes with issuance and deletes in one
statement. `Revoke` accepts the current access secret, or the previous
generation's access secret while it is still valid within `AccessGrace` (a logout
right after a refresh), and removes its family; `Logout` does the same and reports
`auth.EventLogout`. A committed revocation always reports its count; it never
turns into an error.

## The current token

On an authenticated route, `api.Current(ctx)` returns the `Info` of the token that
authenticated the request, including its effective scopes, without a second
lookup. Compare `ID()` values to mark "this device" in `List` results.
`RevokeCurrent(ctx)` revokes it (API logout), and `RevokeOthers(ctx)` revokes every
other family of the subject in this guard under the subject lock ("log out other
devices"). `CurrentProof(ctx)` returns a proof that retains the request's scope
grants, so a token minted from it can only narrow them, unlike `auth.NewProof`.

`api.WithObserver(observer)` returns a binding sharing the same guard that reports
`auth.EventLogin` after full issuance or MFA completion, `auth.EventLogout` and
`auth.EventOtherDevicesLoggedOut` (with `Count`). Observers run after the backend
returned, in process, and cannot undo the change; see
[authentication events](authentication.md#lifecycle-events).

## Pruning

`Prune` removes at most `limit` expired families per call (up to 1,024), with their
generations and consumed digests, in one set-based statement. Candidates come from
the family absolute-expiry and generation expiry indexes and consider only current
generations, so an expired historical refresh record cannot starve cleanup or
prune a live successor. The expiry predicate is rechecked on each locked family;
families locked by a concurrent refresh or revocation are skipped. Small subject
lock rows remain after family deletion to avoid races with new issuance.

`PruneExpired(ctx, batch, maxBatches)` prunes in batches until a short batch or
`maxBatches`, honoring cancellation, and reports the removed families. Call it
from a scheduler handler:

```go
cleanup, err := calendar.Hourly("auth.tokens.prune", func(ctx context.Context, _ schedule.Invocation) error {
	_, err := api.PruneExpired(ctx, token.MaxPruneFamilies, 16)
	return err
})
```

Configured applications can instead return `application.PruneTokens(guard)` in
`FeatureDeclarations.Pruning` to let the [housekeeping schedule](production-operations.md#housekeeping-schedule) prune each guard.

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
