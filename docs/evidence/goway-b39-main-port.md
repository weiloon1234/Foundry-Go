# B39 main port: credential contention

Main anchors below refer to the reviewer's `10030ad4` snapshot, not current branch
line numbers. Port the behavior without migrating main's generated query API.

| Main file | Change |
| --- | --- |
| `auth/token/postgres/backend.go:125` | Replace primary-key-only conflict handling with `query.OnConflict[tokenstore.Subject]().DoNothing()`. |
| `auth/session/postgres/backend.go:115` | Use `query.OnConflict[sessionstore.Subject]().DoNothing()`. |
| `auth/challenge/postgres/subject.go:30` | Use `query.OnConflict[challengestore.Subject]().DoNothing()`. |
| `internal/credential/gate.go` | Replace unconditional outer `Internal` classification with the shared classifier in this branch's `internal/credential/failure.go`, including the shallow custom-`As` fix. |

All three subject tables have a primary key plus `UNIQUE (key, scope)`; the schema
owners are respectively `auth/token/postgres/migrations.go:26`,
`auth/session/postgres/migrations.go:23`, and
`auth/challenge/postgres/migrations.go:19`. Keep these historical schemas unchanged.
Targetless `DO NOTHING` covers either conflicting index. Preserve the subsequent
scope-and-key locking read, stored-identity validation, lock order and capacity
rules; do not replace the existing subject or add retries.

The classifier must inspect each error node with bounded `errorgraph.AsShallow`
inside the existing isolated traversal. Committed/unknown outcomes exposed only
through custom `As` take precedence over overload/contention in either join order.
Typed-nil results, panic/Goexit and incomplete inspection fail closed as Internal.
Keep causes, redaction and single callback invocation.

Port regression sources from `internal/credential/gate_test.go`,
`auth/token/postgres/contention_test.go`, and both session/challenge
`postgres/subject_concurrency_test.go` files. Preserve the original token
capacity/revocation assertions and its safe diagnostic improvement.

Two test adaptations are required on main, verified with read-only `git show`
against `10030ad4`:

- In `auth/token/postgres/backend_test.go`, add the `database/postgres` import
  (aliased `dbpg`) and change `prepare(t *testing.T)` to
  `prepare(t *testing.T, adjust ...func(*dbpg.Config))`. Pass `adjust...` to
  `pgtest.Open(t, adjust...)`. Main's testkit already supports these callbacks;
  the contention tests need them to configure lock and pool acquisition limits.
- In `auth/token/postgres/contention_test.go`, adapt the unknown-outcome fixture
  to main's existing signature: replace `sqlowner.Outcomes()` with
  `sqlowner.Seal{}` in the `database.FoundryStatementUnknown` call. Main's
  `database/errors.go:288` accepts `sqlowner.Seal`, and its
  `internal/sqlowner/seal.go` has no `Outcomes` constructor. This adaptation is
  only for main's framework-internal regression; retain the validated
  `sqlowner.Outcomes()` capability on this branch.

Current-branch commands, outcomes and hashes are in
[the contention evidence](goway-token-contention.json); this is not a claim that
main has been changed or verified.
