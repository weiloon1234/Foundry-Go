# 10 — Model-first authentication and authorization

## Purpose and prerequisites

Prerequisites: [07](07-model-lifecycle-events-and-audit.md), [08](08-http-validation-and-responses.md), [09](09-redis-cache-and-coordination.md). Replace string-identity Actor plumbing with typed, model-first application APIs.

Rust references: `src/auth`, `src/http/middleware.rs`; `tests/auth_acceptance.rs`, `token_acceptance.rs`, `http_authorization_acceptance.rs`. Usage reference: Starter's `src/domain/actor_hydrators.rs`, `src/providers/app_service_provider.rs`, and profile handlers.

## Current implementation

**Complete — verified and consumer experience reviewed.** All six slices,
signed/model-bound/native transport composition, typed security events and
retirement/maintenance behavior passed acceptance. See the
[master evidence](00-master-architecture-and-parity.md#milestone-10-acceptance).
Historical slice notes below describe their state when written; this status and
the master roadmap own the delivered state. Later integrations remain listed in
the source-parity closure.

## Session persistence slice

The implementation now contains `auth/session` and `auth/session/postgres`, with
passing native PostgreSQL, race, consumer, compiler and editor checks. Full
regression passed in 490.3 seconds with unchanged sources, 717 compiler cases,
269 editor probes and four generation targets. The cookie/CSRF portion of slice 2
also passed the subsequent acceptance recorded below. See the
[session guide](../docs/guides/sessions.md) and
[consumer](../tests/fixtures/consumer/authenticating/sessions.go).

PostgreSQL is the first canonical session authority. Foundry owns migrations,
internal generated models and transactions; applications reuse their existing
model providers. The adapter borrows an existing pool, selects an explicit schema
and performs no I/O at construction. Redis session storage is not implied by
milestone 09's Redis support; future adapters must prove the same contract.

Opaque random credentials persist only as hashes. Session metadata retains model
and key types. Issuance, lookup/touch, rotation, revocation and bounded maintenance
share stable subject locks. Rotation preserves the absolute expiry and invalidates
the old secret. Targeted revocation requires a model-owned session ID plus the
matching subject. Pending MFA cannot remember or slide. Failed/uncertain writes
never retry or return a credential. Small subject lock rows remain after pruning;
removing them requires coordinated maintenance, not an ordinary session deletion.

## HTTP session and CSRF slice

Implemented and accepted: focused native races and full regression passed in
503.3 seconds, with 721 compiler cases, 271 editor probes, five field notices and
four current generation targets. All 2,155 verification inputs stayed unchanged. The
[browser guide](../docs/guides/browser-sessions.md) and
[consumer](../tests/fixtures/consumer/authenticating/browser.go) show the concrete
`http.BrowserSessions[M,K]` API. Login, rotation and logout use typed handlers and
stage cookies until response preparation succeeds. The existing endpoint decoder,
auth scope, cookie formatter and response writer remain their sources of truth.

CSRF deliberately uses Go's built-in cross-origin protection with strict evidence
and scheme-aware origin fallback. It protects public login routes as well as
cookie-authenticated routes; CORS trust is separate. Missing browser-origin evidence
on unsafe requests fails closed. Rust's cookie/header CSRF wire protocol is not
copied. Native/bearer clients use separate routes/transport. Password verification,
MFA challenge completion and remaining auth composition are still required.

## Token scope slice

Implemented typed `auth.AccessScope[M]` and immutable `AccessScopes[M]`, scoped
proofs, `Guard.RequireScopes`, and HTTP `AuthenticatedEndpoint.WithScopes`.
Focused consumer, compiler, editor and runtime checks passed. Full regression
will run at milestone 10 completion, after the remaining implementation.
See the [scope guide](../docs/guides/access-scopes.md). The same request resolution
retains both the concrete model and verified grants; scope checks do not add a
second hydration or bypass current model policies. Unscoped proofs do not satisfy
scope requirements, and sessions reject scoped proofs rather than discarding them.
The token runtime and PostgreSQL adapter have now been written, using generated
family/generation models, current-generation lookup, atomic refresh/reuse
revocation and bounded maintenance. Tests and consumer/compiler/editor fixtures
are being written without per-slice execution. See the
[token implementation guide](../docs/guides/tokens.md). Slice 3 is not accepted:
typed HTTP delivery is now written, and its milestone-level verification remains
required. `http.TokenResponse[M,K]` preserves model/key ownership while delivering
generated wire DTOs over secure POST with no-store headers. `RefreshTokenBody`
keeps incoming credentials redacted and uses the existing bounded decoder. The
independent consumer exercises login, scoped access, refresh/replay, current-model
eligibility and revocation; these new tests have not been executed.

## Password hashing implementation

The approved Go `x/crypto` Argon2id dependency is installed. `auth/password` now
contains distinct redacted Plaintext/Hash types, bounded canonical PHC parsing,
context-owned hashing/constant-time verification and explicit rehash policy.
Generated login DTOs discover the plaintext wire contract; hashes have no public
JSON contract. The [hashing guide](../docs/guides/password-hashing.md) and independent
consumer document this implementation. Tests are written but have not run.
Typed model hashes and login orchestration are now written as well. Generated
`password.Hash` fields use sensitive codecs, automatic field documentation and
typed drafts/predicates; audit and cursor/identity boundaries honor that metadata.
`auth.PasswordLogin` reuses the provider and one login lookup, conditionally
rehashes through typed domain behavior, rejects lost replacements, and emits an
explicit pending/full proof from current MFA policy. These tests remain unrun.
Reset/verification, MFA factor persistence/completion, credential-change
revocation and attribution/composition remain required before milestone acceptance.

## Login lockout implementation

`auth/lockout` now supplies typed declarations, bounded orchestration, atomic Redis
and explicit local adapters. Password login adds it through `WithLockout` using
the same typed key as lookup; existing HTTP request limits bound attempts before
expensive verification. The shared error pipeline maps confirmed locks to 429
with Retry-After and protection-store errors to 503. Documentation and consumer,
concurrency, corruption, server-clock, transport, compiler and editor coverage are
written; **these tests have not run**. See the
[lockout guide](../docs/guides/login-lockout.md).

This intentionally replaces Rust's separate increment/set-lock operations. One
atomic update owns count and expiry. Successful attempts cannot clear failures
recorded after they began, or publish proof after a concurrent lock. Generations
invalidate attempts across expiry/reset. Denials do not extend locks. Typed
observations run after a confirmed threshold transition and do not claim durable
external delivery. Redis outages never fall back to process-local state.

## Recovery implementation

`auth/challenge` now owns typed model/purpose tokens, current-state bindings and
one-time persistence. Its PostgreSQL adapter uses generated internal models and
explicit migrations. `auth/passwordreset` and `auth/emailverification` reuse the
provider, lock the current model once and run normal generated mutations in the
same transaction as consumption. Pre-commit failures leave a still-live link usable; expiry
is rechecked after the action. Replacement, pruning and concurrent use share
stable subject locks. The new public `Provider.CheckModel` checks an already
loaded model without another lookup or any credential proof.

See the [recovery guide](../docs/guides/account-recovery.md) and
[consumer](../tests/fixtures/consumer/recovering/recovery.go). Native generation
completed for the internal tables and consumer model. Runtime, local PostgreSQL,
compiler and editor tests are **written, not run**. The next full gate remains at
milestone completion. This does not mark slice 4 or milestone 10 accepted.

Password reset requires transactional invalidation. The framework session/token
integration and checked issuance described below are now written with acceptance
pending. Existing standalone RevokeAll methods still cannot satisfy this contract;
joined RevokeAllIn methods and a typed group do. Recovery HTTP/DTO composition,
durable email dispatch, MFA, attribution and final consumer/parity review remain
in their owning work.

## Credential-change integration

Typed `auth.Revocation[M,K]` contributions and `auth.Revocations[M,K]` now group
registered session/token guards in deterministic order. Their `Invalidate` method
joins the password-reset transaction; a later target failure rolls back the group
and the reset. PostgreSQL adapters expose `RevokeAllIn`, retain the same bounded
row-deletion logic as ordinary revocation, check exact pool ownership through
`Tx.BelongsTo`, and restore the caller's schema setting through a shared savepoint
helper. This adds ownership propagation to database transactions/savepoints and
requires foundation regression as part of the milestone gate.

PasswordModel now requires a generated model-lock callback. Password proofs carry
an immutable issuance check of current identity/hash/eligibility/MFA policy. Session
and token adapters run it inside the creation transaction before credential locks.
A login verified before a reset therefore cannot issue a usable credential after
that reset. Proof.WithAccessScopes preserves the check while narrowing grants.
Backends that cannot enforce checked creation or join revocation fail explicitly.

The [guide](../docs/guides/credential-changes.md) and
[consumer](../tests/fixtures/consumer/recovering/credentials.go) document this
implementation. Real PostgreSQL consumer tests now cover actual session/token
revocation, rollback, reset/issuance/refresh races, schema restoration and foreign
pool rejection. Runtime/capability/ownership/compiler/editor tests are also written.
**None of these new tests have run.** No full gate has resumed; remaining milestone
10 code/tests/docs come first. MFA and remaining auth/HTTP/parity work stay open.

## MFA prerequisites — written, unverified

`auth/mfa` now has distinct redacted TOTP secrets, six-digit code inputs and
high-entropy recovery-code inputs, plus generated JSON contract discovery.
Provisioning uses the same digits/period as private TOTP verification. Private
matching tracks the highest accepted step and recovery consumption returns
owned state. Transactional persistence and management are now written below;
authenticated pending-credential completion remains open.
Rust reference: `src/auth/mfa/mod.rs` (TOTP, recovery, replay and factor changes).

Shared `encryption` now provides bounded versioned AES-256-GCM envelopes, purpose
and owning-record authentication, retained-key rotation and explicit disclosure.
It uses only standard Go crypto, with no new dependency. Its per-key local nonce
budget is not a distributed/restart usage guarantee. Milestone 20 will reuse it.

`Provider.RecheckPassword` returns the current locked concrete model using the
same validator as issuance. It requires the original provider declaration and
checks identity/hash/eligibility/MFA policy without a second provider lookup.
Results remain request-local; pending MFA does not become full authority.

[Consumer declarations](../tests/fixtures/consumer/multifactor/primitives.go)
now generate two DTO descriptors. Primitive/security/consumer/compiler/editor
checks are written, not executed. [MFA](../docs/guides/mfa.md) and
[encryption](../docs/guides/encryption.md) state the actual boundaries. Remaining
work includes transactional pending-credential completion, HTTP/event composition,
operational/orphan integration and final milestone regression. No milestone completion or acceptance is claimed.

## MFA persistence and management — written, unverified

The real `auth/mfa/postgres` adapter now uses generated `internal/mfastore.Factor`
queries and explicit `foundry.mfa` migrations. Rows hold encrypted secrets,
enrollment generations, pending expiry, confirmed time, monotonic replay steps
and bounded recovery hashes. One row belongs to a model/provider identity across
its guards. All operations lock the domain model before the factor; creation
without an existing factor is serialized by that model lock.

`Factors.Enroll`, `Confirm`, `Disable`, `RegenerateRecovery`, `Reencrypt` and
bounded pending-only `Prune` are implemented. Mandatory model callbacks own the
stored flag, normal generated updates, account label, disable policy and
transactional invalidation. `Provider.RecheckPassword` supplies the current locked
model. A separate `lockout.Throttle[K]` finishes its final decision before any
protected writes, so late rejection cannot consume a factor or publish a change.
Redis lockout and PostgreSQL remain separate authorities; counter changes are not
claimed to roll back with a later database failure.

Confirmation commits factor state, model flag and registered session/token
revocations together. Pending replacement changes the enrollment ID and secret.
A confirmed factor requires verification before disabling/replacing. Recovery
rotation invalidates all old hashes. Key rotation preserves factor identity and
consumes the authorizing code. Backend errors return no results; known committed
after-commit errors retain their database outcome. Callback ownership rejects
omitted, reordered, repeated, foreign-transaction and suppressed failures.

The independent consumer now includes a generated Account model and real
PostgreSQL tests for two credential guards, rollback, expiry, replay, concurrency,
late lockout, required-factor policy, pruning, rotation and after-commit failure.
Compiler/editor and runtime adapter tests are written too. **None has run yet.**
Necessary native generation completed; full acceptance remains deferred until
all milestone 10 code/tests/docs are finished. Pending-to-authenticated credential
completion is still required; management returns no full authentication proof.

## MFA completion and HTTP delivery — written, unverified

`auth.SecondFactor[M,K]` binds an unverified factor action to the original provider.
`Factors.Verifier` joins the factor adapter to the credential creation transaction;
`Sessions.CompleteMFA` and `Tokens.CompleteMFA` consume a live pending credential
from the same guard and return a fresh authenticated one. The current model is
locked once, with eligibility checked through the same provider declaration.
Factor state, pending deletion and credential creation roll back together.
Ordinary guards still reject pending credentials, and no full proof is returned.
The credential deadline is checked again before commit. Exact pool ownership and
nested transaction identity prevent accidental independent transactions.

`BrowserSessions.CompleteMFA` uses existing CSRF/cookie staging. Explicit
`MFAEnrollmentResponse` and `MFARecoveryResponse` share secure POST/no-store
response preparation with `TokenResponse`. Generated MFA token bodies retain
separate challenge/refresh and TOTP/recovery types. `EnrollmentID[M]` has native
JSON parsing and an automatically discovered model-owned UUID contract.

The [MFA guide](../docs/guides/mfa.md) and
[consumer routes](../tests/fixtures/consumer/multifactor/routes.go) document the
actual API. Required native generation succeeded after fixing an import-name
collision in the session PostgreSQL adapter. New PostgreSQL, transport, compiler
and editor cases are written, **not executed**. This is implementation progress,
not milestone acceptance. Recovery transport, security events/attribution,
operational cleanup and remaining parity/permissions/test-helper work precede
the full milestone regression. Earlier sections describe their delivery point.

## Recovery requests and HTTP completion — written, unverified

`challenge.Token[M,P]` now has bounded native JSON input and a discovered typed
contract. Generated reset/verification requests retain model and purpose; native
output remains redacted. Named generic schema identities match runtime names,
including an executable's model arguments through a source-preserving alias.
`http.CredentialRequests` shares the existing secret-response POST/TLS/no-store
boundary and adds no-referrer protection. Consumer completion uses typed bodies,
CSRF, no token query fallback, and an empty success response without login.

`challenge.Requests[M,K,I,P]` owns recipient admission, optional reference lookup,
current-model issuance and post-commit typed delivery. It borrows the issuer,
limiter and logger and bounds callbacks with the shared credential gate. Accepted
requests reveal no account/delivery outcome; errors after admission are diagnosed
safely and acknowledged while the caller context remains live. Recipient quota
denial does no lookup; pre-lookup authority/capacity errors remain errors. No
constant-time, automatic retry or durable email claim is made. The consumer uses
explicit IP limits, email validation and a stored-address test transport.

Required native generation succeeded. Runtime, real PostgreSQL, HTTP, compiler,
editor and generator tests are written but **not executed**. The
[recovery guide](../docs/guides/account-recovery.md) and its linked consumer files
own the public contract. Historical email-change invalidation, auth security
events/attribution, operational cleanup and remaining permission/test-helper/parity
review still precede full milestone acceptance; email drivers belong to 16.


## Recovery history, permissions and attribution — written, unverified

Recovery mappings now require `EmailRevision(M) challenge.Revision[M]` alongside
stored Email. Revisions reuse the existing typed UUID implementation and codec;
`BindRevision` rejects zero. The consumer initializes/rotates its revision through
ordinary model hooks, resets verification on address changes, and checks the final
stored changes. Restoring a previous address cannot restore its old link binding.
No challenge lock is acquired from that model write; original lock order remains.
Set-based/raw writes explicitly own equivalent revision maintenance. Password
reset and verification callbacks must preserve the email revision themselves.

`Guard.Origin`/`WithAttribution` capture the verified identity from the existing
scope cache. HTTP required/optional adapters attach it before decoding, preserving
request metadata and leaving anonymous requests anonymous. Audit and event outbox
use the existing attribution context. It carries no reusable execution authority.

`Permission[M]` reuses `Policy[M,struct{}]`; registration, bounded evaluation,
errors and model resolution are shared. HTTP `WithPermissions` validates exact
registration, snapshots requirements, applies all checks after guard/scopes and
before decoding, and exposes owned typed metadata. Domain role relations can be
queried in the callback; no stale role snapshot is added to credentials.

Required native consumer generation succeeded (one refreshed model file).
Behavioral/PostgreSQL/compiler/editor cases are written and **not executed**.
This is implementation progress. Security-event/outbox composition, operational
cleanup policy, signed/model-bound/raw transport composition and the final Rust parity inventory remain before gate 20.

## Public contract and responsibility split

- A typed `Guard[M]` selects an authentication strategy and a provider for model `M`.
- A provider resolves and validates the current model identity. Session and token guards may share the same provider/model.
- Typed policies receive the concrete subject and resource. Roles/permissions use declared semantic identifiers.
- An authenticated endpoint adapter supplies `M` to the handler after resolving it once per request. Its cache key includes guard/provider/model identity, never only the raw ID.
- Persisted credentials contain only the identity/credential information they need. Claims and token scopes are not treated as permanently authoritative model permissions.

Planned model-first handler shape:

```go
func Profile(ctx context.Context, user *User, request ProfileRequest) (ProfileResponse, error)
```

The adapter's guard model type must match the handler subject type at compile time. No ordinary consumer `Actor`, string ID conversion, cast, or second hydration call is required.

## Implementation slices

1. Typed guards/providers, model resolution, request caching, optional authentication and policy registration.
2. Sessions with rotation, expiry, logout/revocation, cookie settings and CSRF integration; never put session secrets in URLs.
3. Hashed personal access tokens, expiration, scopes, refresh rotation and replay/reuse handling. Refresh operations must be atomic under concurrent requests.
4. Password hashing/verification and rehash policy, password reset, email verification, lockout and rate limiting.
5. MFA pending/full states, TOTP/recovery factors, credential revocation integration and current authorization refresh.
6. Identity attribution adapter for audit/events/jobs, and typed auth test helpers that still exercise authorization.

Credential stores use framework migrations. Custom authenticatable models need only provider behavior and typed key mapping; they do not need to reimplement sessions, hashing, tokens or middleware infrastructure.

## Failure and isolation behavior

Missing/deleted/disabled models reject authentication. Permission changes are observed from the authoritative provider/policy state on the next authorization scope; old credential claims cannot restore removed permissions. Keep user/admin identity namespaces separate even when IDs collide.

HTTP model caching lasts one request. WebSocket integration revalidates on subscription and privileged message handling, and consumes revocation signals to disconnect affected sessions. Jobs carry attribution and explicitly selected execution authority, not a serialized user model or bearer token.

Return stable unauthenticated/forbidden/MFA-required error contracts without disclosing credential existence. Login/reset throttling and token comparisons use appropriate constant-time primitives; redact secrets from logs and errors.

## Acceptance

Compile-test guard/handler and policy/resource mismatches. Test one model behind two guards, one provider lookup per request, cross-guard identity collisions, revoked sessions/tokens, disabled users, permission changes, refresh races, CSRF, reset token single use, MFA state restrictions, recovery factor reuse, and authorization-preserving test helpers. Apply the [common gate](README.md#common-completion-gate).


Typed auth test helpers are now written in `testkit/auth`: test-owned production
scopes and concrete-model assertions. They preserve verifier/provider/eligibility,
MFA and policy checks and register scope cleanup. The independent consumer uses
them; unit/consumer/compiler test sources are unverified until the milestone gate.

## Authenticated transport composition — written, unverified

A private `authenticationBinding[M]` now shares validation, immutable requirements,
metadata and middleware between typed endpoints and native routes. Existing public
guard/scope/permission APIs delegate to it. Optional absence discards inherited
model/system/guard provenance while retaining trusted request metadata.

Required/optional endpoints expose `Signed`, retaining concrete or Optional
subjects. `AuthenticatedTransport[P,Q,B,S,R]` is the typed handler capability used
by `modelbinding.BindAuthenticated`; the bound resource remains a separate M.
The ordinary and authenticated model-binding adapters share resolver/validation
logic. Signatures reuse the existing SignedEndpoint and signedRegistration paths.

`RequireRouteAuthentication`/`OptionalRouteAuthentication` supply typed subjects
to native HandleRaw callbacks. Scope and permission requirements use the same
private binding. Signed variants preserve native writer capabilities and original
URL/RequestURI/body/headers; raw payload metadata remains explicit. Guarded routes
without a typed auth binding still fail registration. All declaration constructors
perform no I/O.

Written runtime/consumer tests cover guard/permission/signature failures before
path decoding and resource lookup, optional subjects, resource policy denial,
partial resolver errors, expiry, scope cleanup, native capabilities and metadata
ownership. Nine compiler rejection cases and three editor probes are written.
**None have run.** No generation or dependencies were needed for this slice.
Security-event/outbox composition, operational cleanup policy and final Rust auth
parity inventory still precede the complete milestone 10 verification/fix cycle.

## Source parity closure

Read against Rust `src/auth/{mod,session,token,token_store,password_reset,email_verification,lockout}.rs`
and `src/auth/mfa/mod.rs`, plus HTTP authorization/credential adapters. Source
review is not test acceptance. The independent consumer packages `authenticating`,
`passwords`, `recovering` and `multifactor` exercise the new public experience.

| Rust capability | Go implementation / deliberate redesign |
|---|---|
| Authenticatable registry, Actor hydrator, Current/OptionalActor, Auth model | Explicit typed provider/strategy/guard registry and required/optional request scope; one resolved model per guard, multiple guards per provider; no Actor payload or second hydration |
| Guard access, permission sets, policies, role/claim snapshots | `Permission[M]`, `Policy[M,R]`, typed scope ceilings and exact registration; roles remain domain relations queried by policy, not permanently trusted credential snapshots |
| Sessions, remember, validate/touch, destroy/all, cookie responses | PostgreSQL hashed sessions, bounded expiry/sliding/rotation, typed revocation and HTTP BrowserSessions; standard Go cross-origin CSRF with explicit ingress trust |
| Personal/renewable tokens, refresh, pending MFA, prune | Typed hashed token families, immutable grants, atomic refresh/reuse invalidation, scopes, metadata, HTTP response/refresh DTOs, bounded pruning |
| Sync abilities into every issued token | Deliberate redesign: current permissions are evaluated through policies; immutable token ceilings never expand silently. Revoke/reissue when changing a ceiling; a permission edit needs no duplicated token permission snapshot |
| Password/reset/email verification helpers | Argon2id typed hashes, bounded hashing/verification, current-model login and checked issuance; typed single-use flows own mutation/invalidation transaction and email revisions; public uniform-ack requests/throttles |
| Login lockout/event | Typed separate password/MFA throttles, atomic Redis and explicit local authority; `WithLockedObserver` confirmed-transition notice |
| TOTP enrollment/confirm/verify/disable/recovery and MFA events | Encrypted factor state, replay prevention, recovery hashes, pending-to-full transactional credential completion; `WithObserver` typed changes and rejections replacing Actor-bearing events |
| MFA guard/role requirement | Domain `RequiresMFA` plus stored factor flag and `CanDisable`; pending credentials cannot access ordinary guarded endpoints |
| Audit/event identity | Verified `Guard.Origin`/`WithAttribution`; automatic HTTP attachment, existing audit/outbox capture; affected model distinct from initiator |
| Signed/raw/typed routes and model binding | Typed authenticated transport interfaces and wrappers reuse existing signature/resolver/response logic; native writer capabilities preserved |
| Test helpers | `testkit/auth` production scopes and typed assertions, no model-cache or authorization bypass |
| Credential maintenance | Bounded existing prune APIs and explicit `RetireIn`; stable lock rows retained, no unsafe online deletion |

Integration ownership stays explicit: WebSocket credentials, fresh authorization
and disconnect signals are milestones 14–15; job authority and outbox delivery are
12; scheduler registration is 13; actual email transports/templates are 16;
client generation is 21. Those contracts are not claimed implemented by auth.
No additional authentication subfeature is intentionally left open for milestone
10. Compilation, behavioral/race/PostgreSQL/Redis, generated-code, invalid-contract
and real-gopls acceptance and fixes are complete.

The closure also found a recovery adapter protocol gap: suppressed duplicate or
failed callbacks could be hidden by a custom backend. The runtime now isolates
callbacks and retains their first failure, refusing output even if a backend
suppresses it. New tests cover duplicate, error, panic and abnormal exit. As with
all adapter contracts, the runtime cannot undo a malicious adapter's independent
commit; supplied adapters own and enforce rollback.
