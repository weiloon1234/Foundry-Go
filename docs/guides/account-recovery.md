# Typed account recovery

Typed password-reset and email-verification flows share single-use challenge storage, current-model transactions, email revisions and uniform public request handling. Actual email adapters arrive in milestone 16.

Milestone 10 passed native acceptance. See the [master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-10-acceptance) for verification evidence and later integration boundaries.

## Model-owned flows

The [independent consumer](../../tests/fixtures/consumer/recovering/recovery.go)
shows ordinary Go models, generated queries and typed callbacks. Its model's
stored password remains `password.Hash`; getters do not replace stored fields.

```go
// reset is *passwordreset.Reset[Member, model.ID[Member]].
issued, err := reset.Issue(ctx, member.FoundryReference())
// Check err. Deliver ONLY to issued.Subject().Email, the locked model snapshot.
// issued.Token().Secret() is the explicit secret transport boundary.

// Parse incoming data at the transport boundary; parsing does not authorize it.
link, err := passwordreset.ParseToken[Member](secret.New(submittedToken))
// Check err. newPassword is password.Plaintext, never a stored Hash or string.
updated, err := reset.Complete(ctx, link, newPassword)
// Check err. updated is Member. Completion does not sign the member in.
```

```go
// verification is *emailverification.Verification[Member, model.ID[Member]].
issued, err := verification.Issue(ctx, member.FoundryReference())
// Check err before delivering to the issued model's current stored email.
link, err := emailverification.ParseToken[Member](secret.New(submittedToken))
// Check err.
updated, err := verification.Complete(ctx, link)
// Check err. updated contains the complete verified model.
```

`passwordreset.Token[Member]` and `emailverification.Token[Member]` are different
Go types. Another model's token is also a different type. Phantom type fields
prevent an explicit Go conversion from silently crossing model or purpose.
An explicit parse of a string cannot bypass stored purpose, provider, environment
or model checks. Generic tokens now provide native JSON decoding and an automatic
`JSONContract`; a generated request retains both its model and purpose. The native
parser clears old values on failure and rejects malformed/null input. Parsing a
valid string still does not establish authority.

Ordinary formatting, structured logging and JSON redact tokens. Issued results
keep their model, expiry and token private until accessed explicitly. Persistence
stores only SHA-256 digests of random 32-byte credentials. Credentials use
canonical 43-character unpadded base64url encoding, shared with the existing
credential primitives; no additional dependency is installed.

## Typed HTTP completion

Declare the request body using the framework's actual input types:

```go
//foundry:dto
type ResetRequest struct {
    Token    passwordreset.Token[Member] `json:"token"`
    Password password.Plaintext          `json:"password"`
}

//foundry:dto
type VerificationRequest struct {
    Token emailverification.Token[Member] `json:"token"`
}
```

Generated fields, validation selectors and JSON metadata preserve these types.
An email-verification token or another model's token cannot be assigned to the
reset request. Multi-argument named generic contracts now use the same canonical
identity in generation and at runtime. Models declared in an executable's `main`
package retain their source identity through a contract alias; other imported
names must match exactly.

The [consumer completion routes](../../tests/fixtures/consumer/recovering/routes.go)
pass `input.Body.Token` and `input.Body.Password` directly to the typed operation.
They compose `http.CredentialRequests()` and `http.CSRF(...)`: submissions require
POST, TLS (or an explicitly trusted proxy), browser-origin evidence, and bounded
JSON with no undeclared query fields. Credential protection sets no-store,
no-cache and no-referrer headers before decoding. It shares the same transport
boundary used by secret response adapters. Apply ingress rate limiting as well.

The browser first opens an ordinary recovery page and explicitly submits its
JSON request. GET/prefetch/link-scanner requests never consume a credential.
Completion returns no model, cookie, access token or automatic login; a successful
reset atomically invalidates configured existing credentials. Replay and expired,
disabled, missing or wrong-purpose tokens receive the same unauthenticated code.
Invalid JSON receives the ordinary bounded input error and never reaches a write.

## Public link requests

`challenge.Requests[M,K,I,P]` owns the shared lookup, recipient quota, issuance and
delivery sequence. `P` fixes reset versus verification; `I` is the canonical
submitted identifier type. Construct it with:

- An existing reset/verification issuer and a typed `ratelimit.Limiter[I]`.
- `RequestCallbacks.Lookup(ctx, I)`, returning an optional model reference.
- `RequestCallbacks.Deliver(ctx, Issued[M,P])`, using only the committed subject's
  stored destination. The submitted identifier is absent from this callback.
- An application-owned logger and `auth.Config` callback limits.

The [consumer bindings](../../tests/fixtures/consumer/recovering/link_requests.go)
show the concrete types. `requests.Request(ctx, identifier)` returns no token,
model, existence flag or delivery outcome. Validate and canonicalize the input
before calling it; both quota and lookup use that exact value. Foundry rechecks
the current locked model during issuance, so a changed address is taken from
`issued.Subject()` when constructing delivery.

Recipient quota is charged before lookup for present and absent accounts alike.
Denial acknowledges without lookup or replacing a live token. Once admitted, the
request returns immediately: lookup, issuance and delivery run in a bounded
background dispatch owned by the requester, so response latency no longer depends
on whether the account exists or how long delivery takes. Missing/ineligible
accounts, lookup/issuance/delivery errors, callback panic or abnormal exit, and
dispatch timeout all happen after the acknowledgement. Operational failures have
best-effort diagnostics containing purpose and a safe error summary, without the
submitted address or token. A failing logger cannot change an account-dependent
failure into a public error. This is not a constant-time or crash-durability claim.
Caller cancellation/deadline before admission still propagates; a dispatch keeps
the request's values (attribution, locale) but not its cancellation. Rate-authority
errors, dispatch capacity (`fault.Overloaded` after a short bounded wait) and a
closed requester are reported before any lookup, so they never depend on account
existence. Issuer error inspection
is bounded to 256 nodes and 64 wrapping levels. Incomplete inspection retains a
safe operational diagnostic and the same admitted acknowledgment; it never
suppresses a possible committed or unknown outcome as ordinary ineligibility.

Each dispatch runs under a bounded operation (`auth.Config.Timeout`) and holds
one of `auth.Config.MaxConcurrent` slots; uncooperative work keeps its slot until
it exits. Nothing is retried automatically. Register `requests.Close(ctx)` with
the application's shutdown hooks: it stops accepting requests and waits for
owned dispatches, canceling the rest when ctx ends. `requests.Wait(ctx)` waits
for the dispatches admitted so far, for tests and maintenance tools. Delivery
runs after issuance commits and releases its locks. A failed/lost
publication may leave an undisclosed active link; the next permitted request
replaces it. An email/outbox adapter must supply durable delivery separately.
Milestone 16 supplies the email drivers; this coordinator is usable with an
explicit transport now and does not claim an atomic database/email transaction.

The [public request routes](../../tests/fixtures/consumer/recovering/link_routes.go)
add typed email validation, CSRF, credential transport protection and shared IP
admission, then return the same empty acknowledgement. Independent recipient
families isolate reset and verification quotas. The fixture's lookup uses exact
stored email spelling; domains with case-insensitive matching must canonicalize
before both recipient quota and lookup.

## Bind domain behavior once

`passwordreset.Model[M,K]` requires:

- `Lock(ctx, tx, key)` using a generated `ForUpdate().Find` query.
- `Email(model)`, `EmailRevision(model)` and `Hash(model)` reading stored fields.
  The revision is a nonzero persisted `challenge.Revision[M]`, replaced in the
  model transaction whenever its email changes.
- `SetPassword(ctx, tx, model, nextHash)` using a normal generated update and
  returning the complete updated model, preserving identity and email.
- `ValidatePassword(ctx, plaintext)` for domain strength rules.
- `Invalidate(ctx, tx, updatedModel)` for credential invalidation in the same
  transaction. This callback is mandatory; a failure aborts the whole reset.

`emailverification.Model[M,K]` requires the same locked lookup, stored email and
email revision, plus `Verified(model)` and `MarkVerified(ctx, tx, model)`. A verified model cannot
receive another verification link. Marking must preserve identity/email and return
a verified full model without changing its email revision.

Both flows reuse `auth.Provider[M,K]` for current eligibility. Its new
`CheckModel` method validates an already locked model without another lookup.
This is a model check, not credential verification or an authenticated proof.
Recovery eligibility must permit unverified members when appropriate while still
rejecting disabled members. When the login provider requires a verified address,
set the flow's optional `Eligible(ctx, model)` callback on `passwordreset.Model`
or `emailverification.Model`: it replaces provider eligibility for that flow
(identity checks remain), so verification reaches not-yet-verified members while
still rejecting disabled ones. False rejects like a stale link.

`passwordreset.WithLockout(reset, throttle, func(m M) LoginKey)` returns a reset
that clears the login lockout of the reset account after each committed reset,
using the same login key as `PasswordLogin`: the account ceiling and the
resetting client's window. Clearing is best effort; it never undoes the committed
reset, and a lockout backend failure leaves the old windows to expire. Both flows
also offer `WithObserver` for `auth.EventPasswordReset` and `auth.EventVerified`.

Constructors perform no I/O. Password hashing uses the existing shared bounded
hasher. Reset validates the link and locks the model before doing expensive
hashing; the transaction holds that model lock during the bounded KDF. Request
limits should precede this operation. The challenge store defaults to 128
concurrent operations and a 15-second cooperative timeout; the hasher has its own
independent concurrency and timeout limits.

## Transaction and concurrency behavior

There is one active challenge per application/environment/provider/model/subject
and purpose. Issuing a replacement invalidates the previous link atomically.
Reset and verification links do not replace each other. Default lifetimes are
one hour for reset and 24 hours for verification. Explicit lifetimes must be
between one millisecond and seven days, at whole-microsecond precision.

Issuance locks a stable challenge-subject row, then locks the application model
and captures its current binding. Password-reset binding includes the exact
stored email, email revision and password hash. Verification binding includes
the exact stored email and revision. Both use `challenge.BindRevision`, which
rejects an uninitialized revision. No implicit trimming or case conversion runs
here: domain setters own canonical storage. Consumption compares the binding
while holding the model lock. A different email, revision or password hash rejects
a reset link; password rehashing also changes that hash.

The [consumer model hooks](../../tests/fixtures/consumer/recovering/hooks.go)
initialize a fresh revision on creation and replace it whenever Email changes.
Changing the address also clears EmailVerified. Assigning an earlier revision in
a normal draft cannot restore it. The final Saved check rejects an email change
without a fresh unverified state, including one introduced by a later mutator.
Restoring an old email therefore retains a different revision and rejects its
old links. Unchanged email writes preserve the revision; transaction rollback
restores both the email and its revision. A password reset leaves email revision
unchanged, so a live verification link for that address stays independent.

`challenge.Revision[M]` reuses the existing UUID implementation/codec with a
separate model-owned marker. It is not a model ID, bearer secret or DTO field.
Persist it in an explicit migration and backfill existing rows before enabling
recovery. The flow requires its getter and fails closed on zero. Hook-aware writes
perform this maintenance automatically after the model's hook is declared once.
Explicit set-based writes bypass model hooks and must maintain email revision and
verification state themselves; raw writes and database restores have the same
responsibility. No background observer can detect a bypassed historical change.

Consumption first uses the hash as a locator, then acquires the stable subject
lock and rereads the current token. It checks expiry, locks/checks the model,
applies the typed mutation and removes the token in one database transaction.
It rechecks expiry after the action, since acquiring the model lock or hashing
may take time. An action that finishes after expiry rolls back.

Concurrent uses serialize: only one can complete. Before-commit errors, callback
panic, `runtime.Goexit`, cancellation and failed model hooks roll back the model
change and consumption together. A still-live link remains available after rollback.
All model mutations use the ordinary generated lifecycle/transaction pipeline.
No writes or callbacks are automatically retried. An uncertain commit returns an
error and no model or secret; the caller must not blindly replay side effects.
An after-commit callback failure also returns no model, but its database error
retains `Outcome() == database.Committed`: the password change and consumption
already succeeded. A later cancellation preserves that error cause. Ordinary
in-process after-commit delivery is not durable and cannot undo a committed reset.

Callbacks must use the supplied transaction for writes, run synchronously and
honor its context. Lock order is challenge subject, then application model.
Do not call nested challenge operations, acquire another pool connection, send
email or perform external I/O while holding these locks. Provider eligibility
callbacks used by recovery must follow the same ownership rules. Use a
transactional outbox when durable external effects are needed.

Pass `auth.Revocations[M,K].Invalidate` to the password-reset callback to invalidate
registered session/token guards in the same transaction. The group is built from
each binding's `Revocation()` contribution; direct `RevokeAllIn` methods are also
available. Ordinary `RevokeAll` still opens another transaction and does not
satisfy this boundary. [Credential changes](credential-changes.md) explains the
implementation and its issuance/reset race protection. The consumer now contains
real PostgreSQL session/token integration tests as well as the callback spies;
none of these new tests have run. Reset preserves MFA requirements and returns
no proof, session or access token.

## PostgreSQL setup and maintenance

Reuse the application's existing database pool. Construct
`challenge/postgres.New(db, config)`, then `challenge.NewStore(backend, config)`
and bind the typed flows. The adapter's schema must contain both its tables and
the domain models used by callbacks, unless those models explicitly select a
schema themselves. Configuration/schema selection mirrors existing session and
token adapters and starts no database server.

Register `challenge/postgres.Migrations()` with the existing migration runner.
They own `foundry_challenge_subjects` and `foundry_challenges`, constraints and
indexes. Migrations run explicitly; construction and startup do not synchronize
schemas automatically. Ordinary data operations use generated model queries;
only schema control and migration definitions use infrastructure SQL.

`Revoke(ctx, modelReference)` explicitly revokes that purpose's current link.
`Prune(ctx, limit)` removes at most 128 expired links per operation. Pruning uses
a declared projection, deterministic subject-lock ordering, and rereads the
candidate's unique ID after locking so it cannot delete a replacement link.
Stable subject rows remain after consumption/pruning and need coordinated
maintenance if they are ever removed.

Missing, consumed, expired, replaced and current-state-mismatched credentials
return `auth.Unauthenticated`; operational failures remain errors. Public request
routes must use uniform responses, request/recipient throttling and stored-address
only delivery. The current `Issue` result is in-process delivery data and does
not claim crash-durable email publication. Email adapters arrive in milestone 16.

## Acceptance evidence

Native PostgreSQL, JSON/HTTP, requester ownership/quota/diagnostics, historical
email revisions, rollback, compiler and real-gopls tests passed. Generation is
current for framework stores and consumer DTOs. See the [master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-10-acceptance)
for complete evidence. Email transport and durable job delivery retain their
separate milestone ownership.
