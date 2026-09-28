# Credential changes and transactional revocation

Typed session/token revocation and checked password-proof issuance share the caller's model transaction and preserve guard/provider ownership.

Milestone 10 passed native acceptance. See the [master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-10-acceptance) for verification evidence and later integration boundaries.

## Register the credential guards once for invalidation

Bind sessions and tokens to the same `auth.Provider[M,K]` declaration used by
password login and recovery. Each binding contributes a typed revocation action:

```go
web, err := sessions.Revocation()
// Check err: the backend must support joining a transaction.
api, err := tokens.Revocation()
// Check err.
revocations, err := auth.NewRevocations(users, web, api)
// Check err. Pass revocations.Invalidate as the reset model's Invalidate callback.
```

The [consumer composition](../../tests/fixtures/consumer/recovering/credentials.go)
uses `auth.Revocations[Member, model.ID[Member]]`. The group accepts 1–32 targets,
requires the same provider declaration, rejects duplicates and sorts stable typed
`RevocationName` identifiers. All paths through a group acquire store locks in
the same order. Include every session/token guard used by that model; adding a
new guard requires adding its revocation contribution. This is explicit
registration, not discovery of arbitrary database rows or other applications.

`revocations.Invalidate(ctx, tx, member)` uses stored model identity and can revoke
disabled models. It performs all contributions in one savepoint on the supplied
transaction. A later failure rolls back earlier contributions, even if its caller
handles the error. Password reset propagates the error so its model change and
challenge consumption also roll back. Successful revocation is still provisional
until the outer transaction commits.

Ordinary credential changes can use the same group after locking and updating
the model through generated APIs. Hold the model lock first and propagate errors.
Domain authorization, current-password confirmation and password policy still
belong to their typed authentication/domain boundaries. Merely knowing a model ID
never authorizes changing its credentials.

The direct `sessions.RevokeAllIn(ctx, tx, modelReference)` and corresponding token
method are available for explicit orchestration. Their counts are provisional.
Ordinary `RevokeAll` retains its existing independent-transaction semantics and
must not be used inside a credential-changing transaction.

## Password verification cannot outlive a reset into a new credential

`auth.PasswordModel` now requires a `Lock(ctx, tx, priorModel)` callback. It uses
the prior model's typed primary key with a generated `ForUpdate().Find` query.
Password verification still performs its normal login lookup and optional rehash.
The resulting proof captures a check of that post-rehash snapshot.

When sessions or tokens receive this proof, their PostgreSQL adapter opens the
issuance transaction and invokes the check before taking credential-store locks.
The check locks the current model and compares identity, password hash,
eligibility and MFA policy. Credential insertion happens while that model lock
is held. The application provider does not hydrate a separate Actor, nor does
this change once-per-request guard model caching.

For a login that verified just before a password reset, exactly two orderings
are possible: issuance acquires the model lock first, so the later reset revokes
its new credential; or reset acquires it first, so issuance observes a changed
hash and rejects the old proof. Concurrent refresh/rotation cannot recreate a
deleted credential because they use the existing stable credential-subject locks.
An already-authorized in-flight request retains its request snapshot; revocation
applies to new authorization scopes rather than undoing completed application work.

```go
result, err := login.Authenticate(ctx, email, passwordInput)
// Check err and the required pending/full assurance mode.
issued, err := sessions.Issue(ctx, result.Proof(), session.IssueOptions{})
// Check err. The original proof carries the transaction check automatically.
```

Do not rebuild a password proof with `auth.NewProof(result.Subject().FoundryReference(),
...)`: that constructor is a trusted custom-adapter boundary and does not verify
credentials or preserve earlier verification. Pass `result.Proof()` directly.
If narrowing grants, use `proof.WithAccessScopes(scopes)`, which retains its
issuance check and cannot expand an existing grant.

Custom trusted verifiers may attach one immutable `WithIssuanceCheck` callback.
It must lock and revalidate its own authoritative state on the supplied transaction.
Callbacks cannot commit, perform detached work or retry. Adapters must implement
`CheckedBackend` to accept checked proofs; unsupported adapters fail before
creation. The runtime also rejects omitted/repeated checks and suppressed check
errors. The supplied PostgreSQL adapter enforces the transaction contract;
metadata checks cannot undo an independently misbehaving custom adapter's commit.

## Transaction ownership, schemas and lock order

The supplied session/token adapters implement `TransactionalBackend`. They join
only a transaction from their exact borrowed pool; another pool is rejected even
if its connection string points at the same server. `database.Tx.BelongsTo`
exposes this ownership check. Ordinary and connection-scoped transactions retain
ownership through savepoints; it is not a separate authorization capability.

A shared internal helper scopes the configured schema inside a savepoint. It
restores the previous search path on success; savepoint rollback restores it on
failure. It never changes the caller's schema permanently or commits independently.
Row operations remain generated ORM queries. Search-path inspection/control is
an explicit infrastructure SQL boundary.

Checked issuance and credential changes lock the application model before session
or token subjects. Recovery first holds its challenge subject, then the model,
then the deterministically ordered credential targets. Credential-store
contributions must not acquire model locks or call back into challenge operations.
The issuance adapter's schema must make the locked model available, just as the
recovery adapter must; callback code can use explicitly qualified model metadata
where appropriate.

Preserve the database's before/after-commit distinction. A callback failure before
commit rolls back; an after-commit failure reports the committed outcome even
when no successful result is returned. Late cancellation retains any operational
cause. No operation here automatically retries an uncertain outcome.

These additions cover session/token invalidation through recovery and MFA factor
changes. [Recovery model hooks](account-recovery.md#transaction-and-concurrency-behavior)
now maintain a typed persisted email revision: restoring an old address cannot
revive a link from its previous revision. This changes only the locked model row;
it does not acquire challenge locks in the reverse order. Recovery JSON/HTTP and
public request orchestration are written too. These extensions passed the complete milestone gate and auth parity review.

The new `Provider.RecheckPassword` shares the issuance validator and returns the
current locked model to factor management. It retains model/key types and the
original provider declaration; it does not convert a pending password result
into full authentication. See [MFA prerequisites](mfa.md). Tests passed the milestone gate.
