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
in a request or automatically synchronize schemas at startup. The backend uses
Foundry-generated model queries; application session code needs no raw SQL.

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

## Expiry, rotation and remember me

`session.Config` owns regular, remembered and pending-MFA lifetimes. Each has idle
and absolute deadlines. Sliding activity can extend idle expiry, never the
original absolute deadline. Changing configuration affects newly issued sessions;
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
subject lock. Instances sharing a store need synchronized clocks. Use
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

Listings contain no secrets or hashes and are ordered by creation time then ID.
The default active-session limit is 32 per subject/address, configurable up to
`session.MaxSessions`. Issuance rejects the limit instead of silently logging out
another device. Expired rows are removed during issuance under the same lock.
Listings are bounded by the hard limit and never silently truncate a subject.

`RevokeAll` serializes with issuance for that subject/address. A new session
committed after revocation is a new credential; this operation does not disable
future logins. Disable the account or change its credential policy separately.

## Failure and maintenance

Database errors remain errors, never anonymous authentication or cache misses.
Mutations do not retry automatically. A lost acknowledgement can leave a committed
creation, rotation or revocation with an unknown caller-visible outcome. Errors
return no partial credential. Losing an issued secret can leave an orphan session
until expiry or revocation; no recovery API reveals stored secrets.

The store bounds simultaneous operations and applies a timeout. A backend callback
that ignores cancellation retains its slot until it actually exits. Its late
credential is discarded. Custom adapters must honor the same atomicity, scope,
expiry and resource contracts as the PostgreSQL adapter.

Call `web.Prune(ctx, limit)` in a maintenance task to remove a bounded page of
expired sessions for this address. It rechecks candidates under their subject
locks, so activity that wins the lock first cannot be erased using a stale expiry
snapshot. Register periodic maintenance through Foundry's [scheduler](scheduler.md).

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
