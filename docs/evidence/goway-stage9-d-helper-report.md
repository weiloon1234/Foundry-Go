# Stage 9 lane D: barrier helper retirement

Base `dbe6b1b7`. No commit. Acceptance uses r13 with
`GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

## Completed helper step

The rebased root assertions and shared seed path already use
`User.CurrentEmailAssignment()` and `Product.CreatorAssignment()`.
`TestBusinessAssignmentHelpers`, `TestCrossPackageBusinessAssignmentHelper`,
`TestCurrentEmailAssignmentSuppliesExactInput` and
`TestCreatorAssignmentSuppliesExactUserKey` pass the actual returned assignments
through `inspectInsertAssignments`. Its Creating observer consumes the real
operation-local draft before deliberately rolling back. Assertions check supplied
presence and exact values, including empty email, two concrete User keys and
capture before the source model is subsequently changed. These assertions remain
unchanged; no direct assignment substitutes for either helper result.

Removed the unused `User.CurrentEmailDraft` and `Product.CreatorDraft` declarations
under decision 14, and updated the shared model README. A repository-wide Go
source search finds neither old name. `make generate` succeeds without changing
any generated file or ownership manifest: removal of the two helpers makes no
additional output obsolete while the retained oracle callers still use it.

## Zero-reference audit is blocked

The audit cannot certify zero generated model/projection references outside oracle
recording files on this base. The existing comparison census identifies **32**
recording files with exact generated-symbol references. Outside those files,
**140 retained compile-fail programs** and **11 declaration/factory files** still
reference generated exports. There are **49 retained model/projection directives
and 49 generated implementation files**. The oracle receipt remains `pending`;
no records have been frozen or rewritten here.

The exact source/owner/replacement inventory is in
[goway-stage9-d-helper-references.tsv](goway-stage9-d-helper-references.tsv).
The audit indexes actual generated types/functions using the catalogue owner,
resolves imported aliases and same-package names, and masks comments/strings.
The script and source hashes are recorded with the evidence. Receiver-boundary
inspection additionally confirms generated `FoundryReference` uses in the
registered bootstrap/reporting oracle files; the other similarly named boundaries
are handwritten identity adapters, not generated dependencies.

The 11 supporting files belong to lanes B/C: catalog, intervalqueries, jsonqueries,
linkqueries, models (three files), officequeries, pivothooks (two files), and
reporting. For example, `catalog/orm_test.go` calls `QueryProducts`, which still
requires the generated Product relation set and `defineProductRelations` factory.
The compile-fail references include `arithmetic_compare_value`,
`batch_draft_owner` and `correlation_column_id`; their catalogue rows already point
to new-API equivalents, but their original programs remain in the consumer gate.
Deleting those symbols now would replace intended type failures with undefined
symbol errors. No other owner's assertions, declaration factories or oracle
recording inputs were removed.

The lead must decide whether required oracle support belongs to the exception;
otherwise its retirement and migration of the 140 retained negatives remain the
explicit blocker to item (3). The two requested helper removals are complete.

## Acceptance and scope

Commands and final-source hashes are in `goway-stage9-d-helper.json`.
Consumer build/vet, root assignment-consumption races, models/catalog/queryfixture
races, generation, generation-check and catalogue validation pass.
`make agent-smoke` passes all 502 real gopls probes without failures or skips;
its generation-check prerequisite also passes. Docs-check, gofmt and diff-check
pass. The zero-reference audit is the sole incomplete requested criterion. Live PostgreSQL remains
excluded under the lane rules; its source is compiled and vetted.

No framework API/method-surface, footprint probe/ceiling, schema, codec, generated
contract, OpenAPI, TypeScript or dependency change. Cross-owner files touched are
`models/models.go`, `catalog/product.go`, and `models/README.md`, within the helper
retirement scope. Root assertions were already migrated in the supplied base.

## Docs notes

Mark the helper handoff complete. Keep oracle-dependent directives/output until
its freeze/retirement owner closes the inventory above. No lead-owned docs were
edited.
