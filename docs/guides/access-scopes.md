# Typed credential access scopes

Access scopes place a typed ceiling on a credential and compose with current-model policies. See [tokens](tokens.md) for persistence and refresh rotation.

Milestone 10 passed native acceptance. See the [master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-10-acceptance) for verification evidence and later integration boundaries.

## Declare scopes and immutable sets

```go
var OrderRead = auth.DefineAccessScope[User]("orders.read")
var ProfileRead = auth.DefineAccessScope[User]("profile.read")

grants, err := auth.NewAccessScopes(OrderRead, ProfileRead)
// Check err before issuing or accepting a credential.
required, err := auth.NewAccessScopes(OrderRead)
// Check err before registering a route.
```

Names follow Foundry's semantic identifier grammar: lowercase ASCII letters,
digits, dots, underscores and hyphens, at most 128 bytes. Wildcards have no special
meaning and are rejected. A set contains at most `auth.MaxAccessScopes` (128)
unique declarations. Duplicate/invalid declarations return ordinary errors.
Construction sorts and snapshots names. `Names()` returns a copy; `Contains` and
`ContainsAll` retain the model type. `AccessScopes[User]` cannot be substituted
for `AccessScopes[Admin]`. Its zero value grants nothing.

The [independent consumer](../../tests/fixtures/consumer/authenticating/access_scopes.go)
compiles these APIs against generated models. A static semantic name is the
persistence/contract identifier; a scope does not register a permission or policy.

## Verify a credential, then enforce its grant

An authoritative strategy constructs a scoped proof only after verifying its
credential and restoring the persisted grant:

```go
verified, err := auth.NewScopedProof(user.FoundryReference(), auth.Authenticated, grants)
```

This constructor does not verify a submitted model ID or scope list. Never turn
unverified request values into an authentication proof. `Proof.AccessScopes()`
returns the immutable grant and a boolean distinguishing a scoped proof from an
ordinary `NewProof`. An empty scoped proof still grants nothing. Neither an empty
scoped proof nor an ordinary unscoped proof satisfies a nonempty requirement.

In an existing authentication request scope:

```go
user, err := api.RequireScopes(ctx, required)
// Check err, then authorize the concrete resource with its current model policy.
err = ReadOrder.Authorize(ctx, api, order)
```

`RequireScopes` returns the concrete model and checks every required scope. Empty
requirements are configuration errors; missing grants return `auth.Forbidden`.
Absent/invalid credentials remain unauthenticated, and pending MFA remains
restricted. The check shares `Require`, `Optional` and policies' request cache:
one strategy result and one model lookup per guard, including concurrent checks.
A fresh request resolves the current model again. A retained closed request context
cannot reuse the grant.

Scope names do not become model permissions. A credential with `orders.read` still
needs the policy to authorize the selected order and account's current state.
Ordinary `Guard.Require` authenticates only; use an explicit scope requirement on
operations intended to enforce token capabilities.

## Require scopes at the HTTP boundary

```go
endpoint := http.RequireAuthentication(declaration, transport, api).WithScopes(required)
```

The scope check runs before request decoding and supplies the same concrete model
to the ordinary authenticated handler. `WithScopes` takes a nonempty immutable
set and may be attached once; a repeated attachment returns a validation error
rather than replacing an earlier requirement. The endpoint's inspection metadata
includes `Authentication.RequiredScopes`; returned metadata is a detached copy.
This metadata is also available for later client-contract generation.

Optional anonymous endpoints do not expose `WithScopes`. Session routes normally
use their model policies: an unscoped session proof cannot satisfy this token
requirement. Likewise, session issuance rejects a scoped proof so it cannot discard
credential restrictions while converting the proof to an unscoped browser session.
The trusted password/MFA login flow will issue its own verified session proof.

## Delivery boundary

The [token runtime and PostgreSQL adapter](tokens.md) retain refresh generations
to detect reuse and commit family revocation. Typed storage, HTTP delivery and
account-security flows passed milestone 10 acceptance. Current policies cannot
expand a credential's immutable scope ceiling.
