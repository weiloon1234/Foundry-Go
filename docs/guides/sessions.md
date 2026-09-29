# Typed persistent sessions

`auth/session` provides model-owned session credentials and a PostgreSQL backend.
It uses the existing model-first guards and providers. The
[consumer example](../../tests/fixtures/consumer/authenticating/sessions.go)
compiles independently of the framework. Focused race tests and full native
regression with local PostgreSQL/Redis passed. [Browser integration](browser-sessions.md)
also passed focused races and full native regression. Password and
MFA verification remain separate work; this guide describes credential persistence.

## Construct once, register explicitly

```go
backend, err := sessionpg.New(db, sessionpg.DefaultConfig())
// Check err before continuing. db is an existing *database.DB.
store, err := session.NewStore(backend, session.DefaultConfig(namespace))
// Check err; namespace is a keyspace.Namespace for this application/environment.
web, err := session.New(store, "users.web", users, "users.session")
// Check err; users is the existing auth.Provider for the generated User model.
guard := web.Guard()
registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
```

Construction performs no I/O. The pool remains owned by its application provider.
Apply `sessionpg.Migrations()` explicitly with the ordinary
[migration runner](migrations-and-seeding.md) in the same schema selected by
`sessionpg.Config.Schema`. The default is `public`. Do not run these migrations
in a request or automatically synchronize schemas at startup. Application session
code needs no raw SQL. The backend reads request credentials, lists, enforces
capacity and prunes with explicit schema-qualified statements, and uses
Foundry-generated model queries for other mutations. Migrations
`000002_session_device_confirmation` and `000003_session_impersonation` add
nullable columns with `NOT VALID` checks (metadata-only, no table scan under an
exclusive lock); `000004_validate_session_state` validates those checks in its
own transaction, which does not block reads or writes.

The namespace, guard name, provider name and stored model namespace form the
credential address. Guards for one model can share a provider and store while
remaining isolated. A session binding owns its guard declaration; use that same
declaration for registration and resolution.

## Issue after verified authentication

```go
issued, err := web.Issue(ctx, verifiedProof, session.IssueOptions{Remember: true})
// Check err before passing issued.Secret() to an approved transport boundary.
info := issued.Info()
```

`verifiedProof` has the concrete `auth.Proof[User, model.ID[User]]` type for this
binding. Its producer must actually verify credentials. A submitted user ID or an
arbitrary model must never be treated as a proof. See [password verification](password-hashing.md)
and [MFA completion](mfa.md) for the delivered proof-producing flows.

Each credential contains 32 cryptographically random bytes, encoded as canonical
base64url. Only its SHA-256 digest is persisted. Routine formatting and JSON never
reveal `Issued` secrets; `Secret()` is an explicit transport operation returning
`secret.String`. Session secrets do not belong in URLs, application logs or DTOs.
The public `session.ID[User]` identifies a session but cannot authenticate it.

Resolve models through `web.Guard()` and ordinary auth scopes. The provider loads
and checks the current model once per guard per scope. Deleted or disabled models
reject authentication. An already resolved request retains its snapshot; the next
scope observes revocation and current provider/policy state.

The per-request read is one statement joining the session and its subject, with no
transaction or row lock, so parallel requests from one user never wait for each
other or for listing, pruning and revocation. Expiry is checked against the clock
sampled after the read; a revocation that commits concurrently applies from the
next scope.

Issuance records display-only device metadata (`Info().Device()`): the trusted
client IP and the user agent, truncated to 512 bytes on a rune boundary, from the
request attribution. Device data never authenticates or authorizes.

## Expiry, rotation and remember me

`session.Config` owns regular, remembered and pending-MFA lifetimes. Each has idle
and absolute deadlines. Sliding activity can extend idle expiry, never the
original absolute deadline. Activity is written only when the stored last-seen
time is older than `Lifetime.TouchInterval()`, max(1 minute, idle/20) but never
more than half the idle lifetime (so a 60-second idle window still slides), with one
conditional update that cannot revive a removed, rotated or expired session; idle
expiry may therefore arrive up to that interval earlier than the idle lifetime
after the last request. Changing configuration affects newly issued sessions;
revoke existing sessions when an immediate policy change is required.

The defaults are two hours idle/one day absolute for regular sessions, seven days
idle/30 days absolute for remembered sessions, and a fixed five minutes for
pending MFA. Pending sessions cannot request remember-me or sliding expiry and
cannot resolve an ordinary authenticated model. Remember-me selects server-side
policy here; [browser integration](browser-sessions.md) applies the matching cookie expiry.

```go
rotated, err := web.Rotate(ctx, currentSecret)
```

Rotation atomically replaces the secret and invalidates the old one. It retains
the session ID, subject, assurance, creation time and absolute expiry. Concurrent
rotations have at most one winner. Rotation does not complete an MFA challenge or
raise assurance. Session fixation protection at login and privilege changes also
uses the [browser authentication integration](browser-sessions.md). Server-side idle and
absolute limits and credential renewal follow the
[OWASP session guidance](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).

The PostgreSQL adapter samples its injectable server clock after taking the
subject lock for mutations, and after the read for request authentication.
Instances sharing a store need synchronized clocks. Use
`testkit.Clock` in deterministic tests. A backward clock reading does not move
stored activity backward; clock synchronization remains an operational concern.

## List and revoke

```go
sessions, err := web.List(ctx, user.FoundryReference())
// Each element is session.Info[User, model.ID[User]].
removed, err := web.RevokeID(ctx, user.FoundryReference(), selectedSessionID)
removed, err = web.Revoke(ctx, currentSecret)
count, err := web.RevokeAll(ctx, user.FoundryReference())
```

These operations require caller authorization through an ordinary policy. A
reference or listed ID is not permission. `RevokeID` requires both the matching
subject and guard; it cannot revoke another subject's session even when the caller
supplies a valid session ID. Repeated revocation returns false. Revoking the raw
secret also works for expired entries.

Listings contain no secrets or hashes, are ordered by creation time then ID, and
are read in one statement without taking the subject lock.
`MaxPerSubject` (default 32) caps a subject's live full sessions per address.
`Limit` selects `auth.EvictOldest` (the session default), which revokes the oldest
sessions so a new login succeeds, or `auth.RejectNew`, which fails issuance with
`auth.CredentialLimit` (HTTP 409 `conflict`). Pending-MFA and impersonation
sessions never count toward that cap: each class has its own
`MaxPendingPerSubject` (default 8) and a new one always replaces the oldest of its
class, so `MaxPerSubject + 2*MaxPendingPerSubject` must not exceed
`session.MaxSessions`. Expired rows are removed
during issuance under the same lock and never count. Listings are bounded by the
hard limit and never silently truncate a subject.

## The current session

On an authenticated route, `web.Current(ctx)` returns the `Info` of the session
that authenticated the request without a second lookup; compare `ID()` values to
mark "this device" in `List` results. `RevokeCurrent(ctx)` revokes it (API
logout). `RevokeOthers(ctx)` revokes every other session of the subject in this
guard under the subject lock ("log out other devices") and returns the count.
`CurrentProof(ctx)` returns a proof for the current model, for example to mint an
API token without constructing an unrestricted proof from a submitted identity.

Password confirmation for sensitive actions uses the session itself. After
re-verifying the password with `login.Confirm(ctx, user, password)`, call
`web.ConfirmCurrent(ctx)` to record `ConfirmedAt`. Then
`web.RequireConfirmed(ctx, 15*time.Minute)` returns `auth.ConfirmationRequired`
(HTTP 403) unless that session confirmed within the window, checked against the
backend's clock rather than the scope's cached metadata.

`web.WithObserver(observer)` returns a binding sharing the same guard that reports
`auth.EventLogin` after full issuance or MFA completion, `auth.EventLogout` for
`Logout` and `RevokeCurrent`, and `auth.EventOtherDevicesLoggedOut` (with
`Count`). Observers run after the backend returned and cannot undo the change; see
[authentication events](authentication.md#lifecycle-events).

`RevokeAll` serializes with issuance for that subject/address. A new session
committed after revocation is a new credential; this operation does not disable
future logins. Disable the account or change its credential policy separately.

## Impersonation

`session.NewImpersonation(actors, targets, maximum)` lets an authenticated actor
of one session binding act as a model of another, or the same, binding (for
example support staff acting as a customer). `maximum` is at most 24 hours.

```go
support, err := session.NewImpersonation(web, web, time.Hour)
// Authorize the actor first, for example with a policy on the target model.
issued, err := support.Start(ctx, customer.FoundryReference(), 30*time.Minute)
```

`Start` issues a session for the target, marked with the actor's identity, guard
and own session. It is fully authenticated, never remembered, and expires after
the requested duration. It counts against `MaxPendingPerSubject` apart from the
target's own sessions, so impersonation never evicts or is rejected by them.
Nested impersonation (`auth.ImpersonationForbidden`), impersonating yourself and
missing or ineligible targets are refused. The actor's own session is kept.

In the impersonated request, `Info().Impersonated()` and `Info().Impersonator()`
expose the marker, and `support.Actor(ctx)` loads the original actor through its
provider. An impersonation session acts as its subject but never mints another
credential or changes the subject's credentials. The following refuse it with
`auth.ImpersonationForbidden` (HTTP 403):

- `web.CurrentProof(ctx)` and `auth.CurrentProof`, so no token or remembered
  session without the impersonation marker can be minted for the subject;
- `web.RevokeOthers(ctx)` (logging the subject out elsewhere);
- `ConfirmCurrent`, so no password confirmation can pass;
- `web.RequireNotImpersonating(ctx)`: call it before changing the password,
  email or MFA factors or deleting the account.

An impersonation session ends when the actor's own session ends: Lookup and
`List` require the recorded actor session to still exist and be live, so the
actor's logout, `RevokeAll` (including a password reset joining
`RevokeAllIn`) or expiry ends every impersonation it started. Actors and
targets must therefore share one session store; `NewImpersonation` rejects
bindings on different stores.

`support.Resume(ctx)` ends the impersonation and returns to the actor session
recorded at `Start` with a fresh secret (use it when both share one cookie). That
session is rotated in place: its ID, creation time, absolute expiry and remember
policy are unchanged, so repeated impersonation can never extend it.
`support.Stop(ctx)` only ends the impersonation, for actors that keep their own
credential. If the actor's session was revoked, rotated or expired meanwhile, or
the actor is no longer eligible, `Resume` still ends the impersonation and fails
with `auth.Unauthenticated` caused by `session.ActorSessionEnded`.
`support.WithObserver(observer)` reports `auth.EventImpersonationStarted` and
`auth.EventImpersonationStopped` with both `Subject` and `Impersonator`; events a
session binding emits from an impersonation session (such as `EventLogout`) also
carry `Impersonator`. Record them through an audit or outbox observer.

Resume requires `session.ResumptionBackend` (provided by PostgreSQL). Its
`ResumeActor` runs the actor eligibility check, then locks and revalidates the
exact live actor session and replaces its secret in one transaction. Concurrent
resumes have at most one winner; rotation, revocation and expiry cannot be
bypassed between listing and rotation. Custom adapters must implement this atomic
capability; unsupported adapters fail before ending the impersonation.

## Failure and maintenance

Database errors remain errors, never anonymous authentication or cache misses.
Mutations do not retry automatically. A lost acknowledgement can leave a committed
creation, rotation or revocation with an unknown caller-visible outcome. Errors
return no partial credential. Losing an issued secret can leave an orphan session
until expiry or revocation; no recovery API reveals stored secrets.

The store bounds simultaneous operations and applies a timeout. A burst queues for
at most min(`Timeout`, 5s) and then fails with `fault.Overloaded` (HTTP 503). A
backend callback that ignores cancellation retains its slot until it actually
exits. A callback that completed is reported as a success even if the deadline
passed immediately afterwards, so a committed credential is never silently lost.
Custom adapters must honor the same atomicity, scope, expiry and resource
contracts as the PostgreSQL adapter.

Call `web.Prune(ctx, limit)` in a maintenance task to remove a bounded page of
expired sessions for this address in one set-based, index-backed statement. Rows
locked by concurrent work are skipped and expiry is rechecked on the row actually
deleted, so a session touched meanwhile survives. `web.PruneExpired(ctx, batch,
maxBatches)` prunes in batches until a short batch and reports the removed count.
Call it from a [scheduler](scheduler.md) handler; the auth packages do not import
the scheduler:

```go
cleanup, err := calendar.Hourly("auth.sessions.prune", func(ctx context.Context, _ schedule.Invocation) error {
	_, err := web.PruneExpired(ctx, 1024, 16)
	return err
})
```

Configured applications can instead return `application.PruneSessions(guard)` in
`FeatureDeclarations.Pruning` to let the [housekeeping schedule](production-operations.md#housekeeping-schedule) prune each guard.

The small subject rows remain after session pruning because they provide stable
serialization with concurrent issuance and revocation. They do not authenticate
anyone. Removing those rows requires a coordinated maintenance design; ordinary
session pruning does not remove them. All persistence operations own their database
transaction, so a successful issuance never escapes an uncommitted caller
transaction. Session storage must be writable even during authentication when
sliding activity is enabled.


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
