# Model-first authentication

Typed providers, guards, permissions and policies resolve concrete models once per request. HTTP adapters retain model and resource ownership through ordinary, signed and native handlers.

Milestone 10 passed native acceptance. See the [master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-10-acceptance) for verification evidence and later integration boundaries.

## One provider, multiple guards

Reuse generated model references and ordinary typed queries:

```go
users := auth.DefineProvider("users", (User{}).FoundryReference(),
    func(ctx context.Context, id model.ID[User]) (value.Optional[User], error) {
        return QueryUsers().Find(ctx, db, id)
    },
    func(ctx context.Context, user User) (bool, error) {
        return user.Status == Active, nil
    },
)
api := auth.DefineGuard("users.api", users, tokenStrategy)
web := auth.DefineGuard("users.web", users, sessionStrategy)
```

`User`, its status constants and strategy variables above stand for consumer
models/adapters. The [compiling consumer](../../tests/fixtures/consumer/authenticating/accounts.go)
uses existing generated models. The [session adapter](sessions.md) supplies
PostgreSQL-backed credentials; [tokens](tokens.md) and [browser login flows](browser-sessions.md) reuse the same provider. No static-token fixture is a production login implementation.

`Provider[M, K]` retains the concrete model and stored key type. It checks the
returned model's generated identity against the verified identity. Omitted models
and an eligibility check returning false reject authentication. Provider failures
remain errors, even if a callback also returned a model. Read getters never change
credential identity; an Actor or a second model hydration is unnecessary.

Reuse a provider declaration across guards. Registering different declarations
with the same provider name fails. Repeated guard/policy names fail too. A new
guard with the same name cannot impersonate a registered declaration.

## Strategies and credentials

`DefineStrategy[M, K](source, verify)` receives a `secret.String` from its named
source and returns `value.Optional[auth.Proof[M, K]]`. It runs only when that
source is present. The trusted adapter must verify credentials, expiration,
revocation and any cryptographic checks before calling
`auth.NewProof(reference, auth.Authenticated)`.

A proof freezes a stored reference. Its constructor does not authenticate a
password or token. Client-supplied identities and attribution are never proofs.
`PendingMFA` proofs cannot supply models to ordinary handlers or policies; they
produce `MFARequired`. [MFA enrollment and challenge completion](mfa.md) use
separate typed operations and never pass a pending model to these guards.

`NewCredentials` snapshots named secrets. Missing sources are omitted; an empty
present source is rejected. Limits are 32 sources, 16 KiB per secret and 64 KiB
combined. Routine formatting/JSON does not expose credential/proof contents.

## Request scopes and ownership

```go
registry, err := auth.NewRegistry(auth.DefaultConfig(),
    api.Registration(), web.Registration(), readOrder.Registration())
// Check err before use.
scope, err := registry.NewScope(ctx, credentials)
// Check err before use.
defer scope.Close()
ctx = scope.Context()
user, err := api.Require(ctx)
```

HTTP adapters own this setup and cleanup automatically. Manual scopes are for an
explicit authorization boundary, not a process-global user or a WebSocket
connection lifetime. A new scope verifies credentials and resolves current state.
Closing it cancels and waits for its callbacks and prevents reuse, including
through `context.WithoutCancel`.

Concurrent calls coalesce once per registered guard per scope, including failed
results. Different guards remain isolated. Their declaration identity includes a
fixed provider/model and the scope freezes credential inputs; raw ID collisions
cannot share cache entries. Policies run for every resource/check, using the
scope's current model snapshot. Revocation and account/permission changes are
observed by a new scope, not continuously during the current request.

The default registry permits 128 concurrent callbacks and gives each operation
five seconds. Configure both explicitly through `auth.Config`. A callback that
ignores cancellation retains its slot until actual exit; timeout never publishes
its late model. An initiating caller's cancellation is cached as a failure; other
waiters can cancel independently. No implicit verification retry occurs. Recursive
same-guard resolution fails instead of deadlocking.

Models are Go struct values. Treat maps, slices, pointers and other reference-valued
fields returned from a scope as read-only; clone such fields before modifying them.
Callbacks must return owned models and be safe for concurrent requests. Foundry
does not invent a reflection-based deep copy or turn presentation getters into
stored values. Do not call `Close` from a callback belonging to that scope.

## Concrete policies and handlers

```go
readOrder := auth.DefinePolicy("orders.read",
    func(ctx context.Context, user User, order Order) (bool, error) {
        return order.BuyerID == user.ID, nil
    })
err := readOrder.Authorize(ctx, api, order)
```

The policy model must match the guard, and its resource type must match the
supplied value. Callers cannot substitute an unauthenticated user model.
`Allows` returns `(bool, error)`; `Authorize` converts false to `auth.Forbidden`.
Neither method caches decisions or restores authority from token claims.

```go
transport, err := http.NewAuthentication(registry, http.BearerCredential("api.bearer"))
// Check err before use; the source must match api.Source().
profile := http.DefineEndpoint(
    http.DefineRoute(http.RouteSpec{
        ID: "profile", Method: http.GET, Access: http.Guarded,
    }, http.StaticPath("/profile")),
    http.EmptyQuery(), http.EmptyBody(), http.EmptyResponse(204),
)
route := http.RequireAuthentication(profile, transport, api).Handle(
    func(ctx context.Context, user User, input ProfileInput) (http.NoContent, error) {
        // user is already resolved; policies/services can reuse api.Require(ctx).
        return http.NoContent{}, nil
    },
)
```

`ProfileInput` is an alias for the matching `http.Input` type, as in the consumer
fixture. Response DTOs remain declared separately; stored model fields never
become an automatic JSON response. Use explicit custom getters when mapping DTOs.

An unbound `Guarded` route cannot register a handler, including a raw handler.
Required authentication runs before path/query/body decoding. Use
`OptionalAuthentication` on a `Public` endpoint to receive `value.Optional[User]`:
only an absent credential is anonymous. Invalid, disabled, revoked or pending
credentials remain errors. An adapter validates the guard registration and source
at assembly. Route inspection includes the actual guard/provider names and
optional flag; it exposes no credential material.

Bearer sources reject repeated headers, malformed token grammar and oversized
values. Rejected bearer credentials return a `WWW-Authenticate: Bearer` challenge;
token syntax follows [RFC 6750](https://www.rfc-editor.org/rfc/rfc6750.html#section-2.1). Tokens in query strings are never used. `CookieCredential` reuses an
existing `Cookie[secret.String]` with `SecretCookie()` as its codec; parsing alone
does not implement a secure session or CSRF protection. An adapter snapshots all
its configured sources, so malformed configured inputs fail the request. Use
separate adapters sharing the same registry when routes select different input
sets. The [browser session adapter](browser-sessions.md) supplies the session/cookie/CSRF
policy as one typed integration.

HTTP maps auth failures into its shared `unauthenticated`, `forbidden` and
`mfa_required` contracts. Internal causes remain available to `errors.Is/As` but
never become response text. Ordinary domain HTTP errors are preserved. Typed ordinary, signed, model-bound and native/raw authentication adapters are
verified through independent consumer and transport tests.


## Typed permissions

`auth.Permission[M]` is a model-owned capability without a resource argument.
It reuses `Policy` registration, callback limits, error handling and per-check
evaluation. A `PermissionName` is distinct from an `AccessScopeName`: a token's
scope limits the credential, while a permission evaluates current domain state.

```go
var ViewAccount = auth.DefinePermission("accounts.view",
    func(ctx context.Context, user User) (bool, error) {
        return user.Status == Active, nil
    },
)
// Include ViewAccount.Registration() in the same auth.NewRegistry call.
err := ViewAccount.Authorize(ctx, api)
// Or inspect (allowed, err) from ViewAccount.Allows(ctx, api).
```

A callback can read typed role/permission relations when the domain requires
them. Foundry does not serialize the resulting decision into a permanent Actor
or token claim. Model fields are the current request snapshot; relation lookups
inside the callback evaluate whenever the permission is checked. The domain owns
its role schema and decides which stored state grants the declared capability.

Permissions and resource policies share one registry namespace. Duplicate names
fail, and a second declaration with the same name cannot impersonate the original.
`ValidateIn(registry)` permits exact registration checks during assembly without
running a callback. Ordinary `Authorize` still verifies the selected guard, and
an error from the authority never becomes an allow decision.

```go
endpoint = endpoint.WithPermissions(ViewAccount)
// Compose WithScopes(...) as well when this route requires token scopes.
```

The HTTP adapter accepts a nonempty list once, takes an owned copy, validates all
registrations and rejects duplicate requirements. Every permission must pass,
in declaration order, after guard/scope admission and before input decoding.
Denial stops later checks and the handler. Resource-specific policies remain
explicit in the handler or service. Route metadata includes typed
`RequiredPermissions` alongside `RequiredScopes`; inspection returns owned copies.
The [consumer](../../tests/fixtures/consumer/authenticating/permissions.go) uses
these actual declarations in its ordinary profile route.

## Automatic authenticated attribution

Typed required and optional HTTP adapters attach the successful guard's verified
stored identity and name to shared `attribution.Origin` before input decoding.
Request ID, trusted client IP and user agent remain unchanged. An anonymous
optional request stays anonymous. Invalid, disabled or pending credentials do not
reach a handler or receive authenticated provenance.

`Guard.Origin(ctx)` captures the same metadata from an explicit auth scope.
`Guard.WithAttribution(ctx)` derives a context carrying it. Both reuse the cached
guard result: no additional provider lookup, read getter or Actor hydration runs.
The identity comes from the verified result, not from later mutations to a returned
model. An inherited system/model origin cannot replace the selected guard.

```go
ctx, err = api.WithAttribution(ctx) // CLI/worker scope already explicitly verified.
// Check err. Existing audit.RecordFor and Topic.Enqueue read this attribution.
origin, err := api.Origin(ctx)     // Capture metadata for later processing.
```

[A matching consumer example](../../tests/fixtures/consumer/authenticating/attribution.go)
keeps model ownership on the guard and returns immutable origin metadata. Serialize
only the origin when carrying provenance across jobs/events; create a fresh
execution context and independently select/verify authority for that later work.
Neither a restored origin nor `context.WithoutCancel` revives a closed auth scope.
Credential secrets and the concrete model are absent from the origin.

Auditing and the existing transactional event outbox can capture this context;
ordinary in-process callbacks do not gain crash durability. Dedicated auth event
composition and final parity review remain milestone work.


## Authentication test helpers

Import `testkit/auth` as `authtest`. `authtest.Scope(t, registry, credentials...)`
creates an ordinary production scope and closes it through test cleanup.
`authtest.Require(t, scope, guard)` asserts authentication and returns the concrete
model. Both retain real verifier, eligibility and policy behavior; no model is
injected directly into the auth cache.

```go
scope := authtest.Scope(t, registry,
    auth.Credential{Name: "api.bearer", Secret: issued.Secret()},
)
user := authtest.Require(t, scope, api)
// Check resource policy independently; authentication is not authorization.
err := readOrder.Authorize(scope.Context(), api, order)
```

Use the declared strategy's actual credential source name. For expected rejection
or anonymous tests, call `api.Require(scope.Context())` or `api.Optional(...)`
directly and inspect the error. Empty scope inputs represent absent credentials.
Integration tests should issue real session/token credentials; a focused provider
or policy test can explicitly declare a test strategy. The helpers do not skip
MFA, manufacture password proofs or grant permissions. Test cleanup closes the
scope even when later assertions fail. The
[consumer example](../../tests/fixtures/consumer/authenticating/accounts_test.go)
uses the helpers alongside its existing guard and permission declarations. The tests exercise the production authentication path.


## Signed endpoints and bound resources

Configure scopes, permissions and ordinary endpoint options, then call `Signed`:

```go
secured := http.RequireAuthentication(endpoint, authentication, api).
    WithPermissions(ViewAccount)
signed := secured.Signed(signer)
bound := modelbinding.BindAuthenticated(signed, orders)
registration := bound.Handle(func(ctx context.Context, user User,
    in modelbinding.Input[OrderPath, http.NoQuery, http.NoBody, Order],
) (http.NoContent, error) {
    return http.NoContent{}, readOrder.Authorize(ctx, api, in.Model)
})
```

`SignedAuthenticatedEndpoint[P,Q,B,S,R]` keeps the handler subject separate from
the DTOs. `modelbinding.BindAuthenticated` accepts ordinary or signed typed auth
transports, retaining a separate type for the bound resource. With optional
authentication, `S` is `value.Optional[User]`; the handler cannot silently require
a User instead. The [independent consumer](../../tests/fixtures/consumer/authenticating/composed_routes.go)
uses actual generated User/Order models, their typed IDs and the existing policy.

Admission order is guard, token scope ceiling, authenticated attribution, required
permissions, URL signature (when present), decoded/validated input, then one bound
resource lookup. The handler still authorizes access to that resource. A valid
signature does not authenticate a user or grant ownership of an order. The signed
adapter reuses existing origin, route-purpose, expiry and key-rotation rules.
`PublicURLs` remains required; signed transport fields stay separate from domain
query DTOs. Signed URLs do not sign request bodies and are replayable until expiry.

Route inspection preserves signed/auth metadata and declared DTOs. Neither the
authenticated model nor the bound resource automatically becomes a public response.
Failed admission prevents path decoding and resource lookup. Failed or missing
resources never produce partially populated models for a handler.

## Authenticated native handlers

For native `net/http` integration, bind authentication to the typed route itself:

```go
secured := http.RequireRouteAuthentication(route, authentication, api).
    WithPermissions(ViewAccount)
registration := secured.HandleRaw(func(w stdhttp.ResponseWriter,
    r *stdhttp.Request, user User, path OrderPath,
) {
    // Native request/response handling; user and path remain concrete.
})
```

`OptionalRouteAuthentication` supplies `value.Optional[User]` and requires a Public
route declaration. Required authentication requires Guarded. Unbound guarded
routes remain rejected. Native adapters reuse the same credential, scope,
permission, attribution and cleanup implementation as DTO endpoints.

Call `secured.Signed(signer)` to combine native handlers with signed links.
`SignedAuthenticatedRoute[P,S].URL(ctx, origin, path, expires)` uses the existing
signer. Native handlers retain their response writer capabilities, URL, RequestURI,
headers and body; signing query parameters remain visible in the native request.
Raw payloads do not acquire an inferred client schema. Model ownership and typed
path parameters remain compiler checked.

The Foundry HTTP server establishes request ID/IP/user-agent metadata before the
router; a standalone Router's host owns that outer request boundary. Regardless
of inherited identity metadata, an absent optional guard clears model/system/guard
attribution and preserves only request metadata. This never turns metadata into
authentication. The composition examples and tests passed milestone 10 acceptance.

## Security events and operations

See [authentication operations](auth-operations.md) for typed MFA observations,
transactional outbox composition, bounded pruning and account retirement. The
[parity inventory](../../blueprint/10-model-first-authentication-and-authorization.md#source-parity-closure)
records implemented behaviors, explicit redesigns and later milestone integrations.
Milestone 10 acceptance includes these contracts.

## Cookie response caching

Cookie-authenticated routes, including optional/anonymous responses and early
authentication or origin failures, default to `Cache-Control: no-store`. The
shared authentication adapter and browser-session middleware apply the same
policy before downstream handling. Raw and typed handlers receive this default.
Explicit downstream cache headers remain an application-owned override; never
make personalized cookie responses publicly cacheable. Bearer-only routes keep
their existing response policy.
