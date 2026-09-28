# Authentication events and maintenance

Typed security observations compose with existing events and outbox storage. Maintenance remains bounded, and account retirement joins the domain transaction.

Milestone 10 passed native acceptance. See the [master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-10-acceptance) for verification evidence and later integration boundaries.

## Security events

`mfa.Factors.WithObserver` returns an immutable factor service with one typed
`Observer[M,K]`. `Changed` receives the actual transaction and a `Notice[M,K]`:
`Subject()` is the affected model reference and `Action()` identifies enrollment,
disable, successful login completion, recovery replacement, reencryption or retirement.
Neither notice nor payload needs a complete authenticatable model or its secrets.
Use normal generated model observers for password/email/eligibility changes.

Map notices to a small domain event DTO and call `events.Topic.Enqueue` with that
transaction. Foundry owns JSON capture, attribution, outbox storage and rollback.
The [consumer example](../../tests/fixtures/consumer/multifactor/security.go)
shows the complete composition. Apply the existing `outbox.Migrations()` explicitly
in the same schema. An observer error rolls back factor, model, credential and
outbox changes. An enqueue ID is not proof of commit. Delivery belongs to milestone 12.

Use `Topic.AfterCommit` for process-local listeners that should run only after
commit. Listener failure reports a committed database outcome and does not roll
back the change; blindly retrying would be wrong. These callbacks have no crash
durability. The supplied tests exercise failed enrollment, failed pending-to-full
completion, outbox rollback, after-commit order and separate initiator attribution.

`Rejected` observes a valid factor mismatch or a confirmed lockout immediately.
It deliberately has no transaction parameter: the failed authentication transaction
will roll back. An error/panic/abnormal exit in this observer cannot authorize the
request. Malformed input, missing/inactive factors, failed model resolution and
infrastructure faults do not fabricate a factor-rejection event. This observation
is not durable; use the existing event bus for optional process-local diagnostics.
It contains no submitted code, password, factor material or credential.
`lockout.Throttle.WithLockedObserver` separately observes confirmed threshold
transitions, not every already-locked denial.

`attribution.FromContext` identifies the initiating request/model/system. The
notice's affected subject is a separate field. A recovery link or pending MFA
credential is not a fully authenticated actor. Restoring event/audit attribution
never restores authentication or authorizes a worker.

## Bounded maintenance

Run maintenance against explicitly configured stores and their existing pool.
Each call is bounded and cancellable; repeat on later scheduled runs when a full
batch is returned. A zero count can mean competing workers hold the eligible rows;
it is not a globally consistent proof that nothing remains. Scheduler registration
belongs to milestone 13; no background timer is installed by these constructors.

| API | Batch limit | Eligible state |
|---|---|---|
| `Sessions.Prune` | `session.MaxPageSize` (1024) | Expired sessions within this guard/address |
| `Tokens.Prune` | `token.MaxPruneFamilies` (16) | Expired families and their bounded generation history |
| Reset/verification `Prune` | `challenge.MaxPrune` (128) | Expired links for this provider/purpose |
| `Factors.Prune` | `mfa.MaxPrune` (128) | Expired **unconfirmed** enrollments only |

Small stable subject-lock rows intentionally survive pruning. Deleting one while
another transaction still relies on it can split serialization and allow a stale
credential mutation. There is no live automatic subject-row garbage collector.
Reclaiming them is a coordinated operational procedure after all writers for that
namespace have stopped and retained credentials/challenges have been accounted for.
Do not delete confirmed factors merely because a lookup currently returns no model.

## Account retirement and key reuse

An authorized administrative domain transaction calls `Factors.RetireIn` before
hard-deleting its model. This locks the existing model before factor state, clears
the enabled flag, invalidates configured session/token guards, removes pending or
confirmed factors and emits `Retired` in the same transaction. Disabled models can
be retired; absent or mismatched models fail. All changes roll back with the outer
transaction, including generated domain deletion and outbox entries.

Unlike self-service `Disable`, retirement does not ask the departing account for a
password or factor and does not apply its `CanDisable` rule. Authorize the admin
before entering this operation. Never expose it as a public self-service route.
The [consumer transaction](../../tests/fixtures/consumer/multifactor/security.go)
uses ordinary typed queries. No infrastructure SQL is needed in the application.

Domain keys should not be recycled. If a natural key is deliberately reused, first
retire its factor and credentials; initialize a fresh email revision for the new
model. Recovery links from the old revision then remain invalid. Already orphaned
factors from out-of-band deletes require an audited maintenance procedure while
creation/issuance for that identity is stopped. Retirement cannot infer a deleted
model's policies or safely race a replacement account into existence.
