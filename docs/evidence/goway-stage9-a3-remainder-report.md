# Stage 9 lane A roster completion

Base fdaff938, with the previously accepted lane A delta retained. This turn changes
only tooling, httpmodels and this evidence. No commit.

## Migration

- `tooling`: ORM factory assignments, ordinary and transaction-only explain,
  read/primary-read queries, and the diagnostics test's explicit model identity.
  The factory test observes the bound sequential names at the database boundary.
  Explain and both read paths retain old/new SQL and binding parity oracles.
- `httpmodels`: explicit keyed ORM resolvers for UUID and natural keys and the
  parent-scoped order query. Existing persisted tests use native inserts, updates,
  soft-delete visibility and retrieval declarations. Stored fields, the handwritten
  AccessName getter, eager parent hydration and retrieval veto assertions remain.
  Three database-free parity cases compare the legacy and ORM lookup statements.
- The remaining A3/I-only HTTP roster was inspected. Its generated DTO, enum,
  config, path and query-parameter APIs are retained transport contracts. There
  are no other active generated persistence symbols in those packages. Legacy
  persistence calls in the two new parity tests are explicit SQL oracles only.
  `httpetags` moved to lane B under decision 10 and is excluded from this roster.

## Negatives and blockers

Of the original 26 waiting negatives, 18 model/relation cases are already ported
by A1 and six link/soft cases by the accepted A2 slice. No additional mapping or
registry changes are needed here; all 24 remain native ORM consumer negatives.
The other 184 originally assigned cases retain their transport/service contracts.

Two cases remain blocked on **lane D's nestedbindings declarations**:

- `nested_field_owner`: needs Teams.Name, preserving wrong Project/Slug ownership.
- `nested_relation_owner`: needs TeamLinks.Projects, preserving the Team/Task
  target mismatch. This supplier is absent in this worktree too; the legacy
  negative remains until D's declaration lands.

`factory_state_owner` is a framework-integration case owned by lane D. Its call to
`tooling.Records()` still rejects the wrong model, but its state input still uses a
legacy UserDraft. Lane D should port that input to []orm.Assignment[models.User]
when completing its factory negatives. This turn does not edit D's case.

## Acceptance

Passed on r10 with GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off:

- Consumer build and vet: ./tooling ./httpmodels.
- Tests and races: both packages, including six SQL parity cases and the factory
  bound-name sequence assertion. Initial tooling runs stopped at its existing
  HTTP helper because the sandbox denied a loopback listener. Both complete
  tooling-package reruns passed with local-listener access; httpmodels passed
  the initial runs. No source correction was needed after compilation.
- 210 originally assigned compiler cases plus the dependent factory_state_owner:
  all 211 passed with their intended diagnostics. The two blocked cases were
  checked as retained legacy negatives, not claimed as native ports.
- Catalogue (570 ported, 18 pending, 444 unaffected), make fmt-check,
  make docs-check and git diff --check.

PostgreSQL tests were compiled only; the lead owns live integration. Exact commands,
outcomes, source hashes and the full negative inventory are recorded in
[the evidence JSON](goway-stage9-a3-remainder.json). Logs are under
/private/tmp/goway-stage9-a3-remainder. No shared registry or framework source was
changed in this turn; earlier accepted cumulative changes remain intact.

Consumer signatures: Records now returns Factory[WriteRecord, []orm.Assignment[WriteRecord]];
UserPlan and LockedUserPlan return orm.Plan (the same shared plan alias). HTTP
resolver and service signatures remain unchanged. No framework API or surface
additions, footprint probe changes or ceiling changes.
No generated output, contracts, declarations, module pins or Makefile edits.
The SQL/transport contracts are retained; no contract regeneration is needed.

## Docs notes

Lead changelog: the remaining tooling and route-model consumer examples now use
explicit ORM table declarations and keyed lookups, retaining factory recipes,
transaction-only planning, retrieval hooks and parent/soft-delete scopes. Keep
legacy generated coexistence and parity oracles until the lead's cleanup barrier.
