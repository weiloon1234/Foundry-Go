# Token issue-capacity contention follow-up

No commit. Runtime changes are limited to token subject creation and the shared
credential gate; no schema, wire contract, public method or footprint ceiling changes.

## Findings and fixes

The original eight-worker test reproduced the failure during the five-repeat
race run. Safe diagnostics identified PostgreSQL `23505`, constraint
`foundry_token_subjects_key_scope_key`, with a confirmed rollback. The subject
has both a primary key and `UNIQUE (key, scope)`, while its insert targeted only
the primary-key conflict. The token store now uses targetless `ON CONFLICT DO
NOTHING` to handle either index, then performs the same scoped locking read and
identity validation. No retry, lock-order, capacity, or persisted-schema change.
The pre-ORM implementation at `7ee1b7e0` also targeted only the primary key.
This reproduces a concrete cause of the lead's reported symptom; the lead's
original run did not retain enough diagnostics to prove that occurrence had the
same cause.

Separately, deterministic tests demonstrated that the shared credential gate
labelled both a confirmed `55P03` lock-timeout rollback and a pool acquisition
failure as `fault.Internal`. It now reports confirmed lock/deadlock/serialization
rollbacks as `fault.Conflict`, preserves `fault.Overloaded` admission failures,
and classifies recognized deadlines as `fault.Timeout`. Causes and database
outcomes remain available. Bounded, isolated inspection checks the whole error
graph; committed/unknown outcomes, panic, Goexit and incomplete inspection prevent
an optimistic classification. Nothing automatically retries or publishes partial
credentials.

The original capacity/revocation assertions remain intact, including exactly two
successful issuances. Unexpected errors now include redacted framework diagnostics
(SQLSTATE, constraint and outcome), rather than only the outer generic message.

## Regression coverage

- Hold a subject row lock with a 50 ms lock timeout, and separately hold the sole
  pool connection with a 50 ms acquisition timeout, while Issue and RevokeAll run.
  Assert classifications, no returned secrets/count, unchanged persisted family,
  valid original credential and successful operations after releasing the resource.
- Inject PostgreSQL `40001`/`40P01` exceptions to test classified rollback paths
  without depending on deadlock victim selection; assert exactly one callback.
- Check committed and unknown outcomes in both join orders, including late
  cancellation; check custom unknown-outcome matching, cyclic error graphs,
  panic/Goexit containment, redaction and no replay.

## Acceptance

Passed on r14: final `go test -race -count=1 ./internal/credential ./auth/token/...`
with PostgreSQL required; twenty race-enabled repetitions of the original test
and the two new database regression tests after the subject-insert fix; races for
`auth`, session, challenge, MFA and lockout unit packages; targeted vet; docs-check;
gofmt and diff-check. The token PostgreSQL suite uses retained isolated schemas.
No other packages' PostgreSQL suites ran. The initial sandbox connection failure
was rerun successfully with authorized database access. Pre-fix failures and exact
commands are retained in [the evidence JSON](goway-token-contention.json).

## Shared files

- `internal/credential/gate.go`, `failure.go`, `gate_test.go`
- `auth/token/postgres/backend.go`, `backend_test.go`, `contention_test.go`
- `docs/guides/tokens.md`

## Docs notes

Token subject creation handles both unique indexes. Credential failures preserve
contention/admission classification without changing uncertain-outcome or replay
rules. No blueprint or changelog was edited.

The review follow-ups are now addressed: session and challenge use the same
all-index conflict policy while retaining scoped locking and identity validation.
The classifier uses bounded shallow custom `As` inspection. Regressions cover
As-only committed/unknown database errors joined with overload or contention in
both orders, and typed-nil/panic/Goexit As results. The three special-As cases
failed before this fix and pass afterwards.

Fresh acceptance passed on r14: all token/session/challenge PostgreSQL suites and
shared credential tests with `-race`; ten repetitions of each new session/challenge
first-subject regression; affected auth unit/race packages including password;
targeted vet, docs-check, formatting and diff-check. Session regressions verify
all issued credentials, exact capacity, complete revocation and stable subject
identity. Challenge regressions verify replacement order, exactly one preparation
per issue, single-use consumption and separate reset/verification subject scopes.
No existing assertions were relaxed. The shared-file list additionally includes
`auth/session/postgres/backend.go`, `auth/challenge/postgres/subject.go`, and their
new `subject_concurrency_test.go` files.

The [B39 main-port note](goway-b39-main-port.md) lists the exact reviewer-named main
files and preserves main's existing query API. No main files were changed here.
