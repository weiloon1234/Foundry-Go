# Stage 9 Lane B — reassigned D3 consumers

Packages taken: **recovering, authenticating, nestedbindings, notifying, realtime,
cursorqueries, pagequeries, genericdto, internal/authfixture, localization, mailing,
modelvalidation, outgoing, requestflow, security, storing, unions, validationinput,
validationrules** (19). D4 and the eight explicitly excluded packages are untouched.
The accepted upsert delta remains in the worktree.

## Status

| Packages | Result |
| --- | --- |
| `recovering` | ORM table, identity, write hooks and all recovery/login/verification callers migrated. |
| `authenticating` | Provider reads, proof/session/token identities and order bindings migrated. |
| `nestedbindings` | Three tables, relation slots, retrieval hooks and parent-scoped alternate-key bindings migrated. |
| `notifying`, `realtime` | Stored identity references migrated; notification pagination uses shared ORM aliases. DTOs and transport contracts unchanged. |
| `cursorqueries` | Projection, completed-record/CTE/set, recursive and validation/failure cursor tests migrated. Two original clause tests retained explicitly pending lane C. |
| `pagequeries` | Model/eager lookahead and projection/failure pagination migrated. Original relational-clause test retained explicitly pending lane C. |
| `genericdto`, `internal/authfixture`, `localization`, `mailing`, `modelvalidation`, `outgoing`, `requestflow`, `security`, `storing`, `unions`, `validationinput`, `validationrules` | Audited: no active generated-model query/draft/identity call needs migration. Existing service/DTO/validation contracts retained and included in targeted acceptance. `modelvalidation` already uses ORM declarations. |

## Behavior and API

Four fixture tables are published: `recovering.MemberRecords` and
`nestedbindings.TeamRecords`, `ProjectRecords`, `TaskRecords`. Recovery publishes
`MemberIdentity`; nested bindings publish `TeamLinks` and `ProjectLinks`. No
framework API, Query/Table/column methods, validated capabilities, seals or
runtime engines change.

Recovery's ORM `Saving` hook calls `orm.WriteUnset(write, fields.EmailRevision.Set)`
for unchanged email. The legacy and ORM adapters share revision-generation and
stored-state validation policy. A PostgreSQL `BEFORE UPDATE OF email_revision`
trigger regression rejects even assigning the old value, proving actual omission
while retaining the stored revision and an unrelated explicit false assignment.
The original revision rollback, stale-link, concurrent-lock, credential-revocation,
redaction and commit-reconciliation assertions remain.

Password rehash retains its stored-hash compare-and-swap predicate. Model locks use
the supplied transaction before challenge/credential locks. Auth request snapshots
and fresh-scope re-resolution remain intact. Nested lookup tests retain parent
isolation, uniqueness failures, authorization order, query counts, soft visibility,
eager retrieval and callback vetoes. No application registers both observer families.

## Catalogue and shared files

Six Stage 7 rows move from `port` to `ported`: `nested_bundle_handler`,
`notification_payload_owner`, `notification_recipient_owner`,
`session_metadata_model`, `token_id_model`, `websocket_disconnect_model`.
Fifteen consumer negative programs move to ORM references, columns or queries;
92 existing service negatives are selected for acceptance. The evidence JSON lists
exact cases and commands. The D3 delta touches 66 files including its report and
evidence; the previously accepted upsert files remain separate. Catalogue: **576 ported, 12 pending, 444 unaffected**.

Shared edits are confined to owned entries in:

- `blueprint/goway-data-layer/compilefail-mapping.tsv`;
- `tests/fixtures/consumer/types_test.go`;
- `tests/fixtures/goway/compiler_cases_test.go`.

Generated outputs, directives, manifests, module files, shared models/reports,
client contracts, editor and footprint testdata are untouched. No persisted schema
or wire change. No footprint source/budget changes or new framework surface;
existing ceilings remain unchanged and are not remeasured for this fixture-only slice.

## Acceptance

Build, vet, targeted tests/races, 92 consumer negatives, 22 Go-way negatives,
catalogue, docs-check, gofmt and diff checks pass across the recorded segments.
See [evidence](goway-stage9-d3.json) for exact commands, results and source hashes.
Completed build/vet/test/race and initial compiler segments used r8 before the
cache rotation. Remaining compiler fixes and docs checks use r9. All retain
`GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off` with PostgreSQL/Redis disabled. PostgreSQL sources compile; live DB execution remains lead-owned.
Build covers the 17 packages with production Go files; vet/test/race also cover the
two test-only query packages. Sandbox listener failures in `realtime`, `mailing`
and `validationrules` are rerun with local loopback access, without live providers.

## Requests and docs notes

Lane C: complete scalar DISTINCT and ordinary `RecordRead` DISTINCT ON adapters.
`cursorqueries/legacy_clauses_postgres_test.go` preserves grouped/joined winner
selection and nullable scalar/window traversal. `pagequeries/legacy_clauses_postgres_test.go`
preserves grouped, CTE/set, scalar/window and joined winner-page assertions. These
remain partial packages, not completed migrations. Their assertions were moved
intact rather than replaced with raw SQL or DTO decoding substitutes.

Lane A's separate complete-record streaming dependency remains open in
`retrievalqueries`; it was not edited. The lead retains generator retirement,
D4 integration and live PostgreSQL verification. Publish recovery's `WriteUnset`
example: removing a pending revision means omitted, never NULL/zero or reassignment
of the previous revision. Retain the model-first lock and uncertain-outcome guidance.
