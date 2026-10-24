# Stage 8 D8: model scaffolding

`foundry make model` now creates a handwritten ORM model file: the tagged owner key, typed table namespace (slot handles use a `Slot` suffix to preserve fields named Key, Table or Query), `orm.Define`, and a per-model `[]orm.CheckedTable` registration helper. Extension flags add `orm.Slot` bindings, thin handle declarations, a stable ORM owner, typed eager relations and `slots.DeclareORM` cleanup registration. Metadata DTO codecs still use generation. The command flags, filenames, create-only publication, collision checks, disk requirement and invalid-empty attachment policy are preserved.

The existing command has no field, timestamp, soft-delete, mutator or ordinary-relation flags. Its ID-only persisted default remains unchanged. The new guide and executable tests show handwritten additions using CreatedAtOf/UpdatedAtOf, SoftDeleteOf plus SoftDeletable, distinct InputText and relation declarations after tables. WithTables is not in this worktree: per-model helpers compose into an application-owned `models.Tables() []orm.CheckedTable`, without overwriting a shared package registry or adding an application API.

The plain and extension goldens are emitted by the scaffold renderer. Fresh-consumer tests compile them, run orm.Check, validate metadata codecs and extension registrations, reject incomplete attachment policies, preserve repeated-creation refusal and pre-publication collision checks, and ensure later generation never takes ownership of the ORM model. A second test edits actual scaffold output to add managed roles, a distinct mutator input and a relation, then compiles and checks it.

## Initial D8 acceptance

The [evidence JSON](goway-stage8-d8-scaffold.json) records commands, exit codes, source hashes and the segmented cache environment. Completed checks ran on r5; the interrupted editor gate was restarted on r6. No public runtime surface additions, catalogue changes or footprint ceiling changes. Legacy model emitters remain intact for 8c. No live PostgreSQL or commits.

All acceptance checks passed:

| Check | Result |
| --- | --- |
| `go build ./...`; vet database/internal/CLI packages | PASS, refreshed after the final template fix |
| Full generator/CLI races | PASS; generator package 532.342 seconds |
| Final scaffold races, including compilation and orm.Check | PASS; 36.908 seconds |
| ORM/query/querycore/ormcore/shared-loader races | PASS |
| `make fixture-check`, including compiler cases and footprint budgets | PASS |
| Catalogue validation | PASS; no D8 row changes |
| `make generate`; `make generate-check` | PASS; no generated changes |
| `make typescript-check` | PASS |
| Docs, formatting and diff checks | PASS |
| `make agent-smoke` with gopls v0.23.0 | PASS on r6; 466 consumer probes, 249.373 seconds |

The initial focused test exposed a stale legacy `Collection()` call; the test now
uses the thin handle's `Validate()`. A golden-update flag was initially passed to
the CLI test package as well; the corrected invocation passed. Source review then
found table-operation name collisions; suffixed slot handles and the regression
for Table, Key, Query and TableSlot passed before final build/vet. The interrupted
editor gate was rerun on r6 without restarting completed checks.

## P2 review correction: scaffold symbol collisions

Name allocation now reserves the complete scaffold symbol set before choosing
numeric suffixes. Structural declarations retain priority; the table variable and
metadata DTOs move when necessary. Metadata reservations include JSON codecs,
validation helpers and owned generated filenames. Codec and validation names use
the same naming functions as their emitters. Default goldens stay byte-identical;
flags, persisted names and publication behavior stay unchanged.

Eight compilation regressions cover both reported table names, qualified tables,
`--metadata Table`, extension declarations, suffix reservation, and codec/validation
helper collisions. Every case checks deterministic rendering, compiles a fresh
consumer through the scaffold publisher, and runs `orm.Check`.

The correction also touches the shared generator files `generated_names.go`,
`emit_dto.go` and `emit_dto_validation.go`; their emitted DTO text is unchanged.
It adds `scaffold_model_names.go`, updates scaffold rendering/tests and documents
suffix selection in the model-scaffolding guide. No runtime surface, catalogue row
or footprint ceiling changes.

P2 acceptance passed on r6: build; database/internal/CLI vet; full generator and
CLI races (generator: 467.272 seconds); targeted scaffold races with all eight
compilation cases; fixture vet/tests and footprint budgets; catalogue; generation
and generation check; TypeScript; docs; formatting; diff check; and the editor
gate (466 consumer probes plus 17 field probes, 258.900 seconds). Generation
reported zero changed or removed files. The unchanged runtime packages retain
their earlier race results.

The first fixture and TypeScript runs could not bind local HTTP ports inside the
sandbox. Both passed when rerun with local-listener access; no source correction
was required. Exact results and current source hashes are in the evidence JSON.

## P2 follow-up: valid Go identifiers

Read `fg-review-laneD2/last-15.md`. Names derived from accepted SQL identifiers now
receive a `Query` prefix when needed, before collision allocation: `_` becomes
`Query`, `_9` becomes `Query9`, and schema-qualified forms follow the same rule.
Every allocated symbol family is checked with Go's identifier grammar and universe
scope; empty names, keywords, blank identifiers and predeclared names are rejected.
Generated filename reservations are kept separate from Go identifier validation.

The compilation matrix now has 22 cases, adding underscores, digit-derived names,
keywords, predeclared spellings, Unicode Go models, qualified forms and fallback
collisions. All compile actual scaffold output and pass `orm.Check`. Separate tests
cover every Go keyword and predeclared name. Eight invalid-SQL cases confirm that
bare digit-leading and non-ASCII SQL names still fail before publication; this
preserves the existing SQL-name grammar rather than expanding it.

Current acceptance passed on r6: scaffold/CLI races (generator tests: 54.001 seconds),
full build, database/internal/CLI vet, catalogue, generation, generation check,
docs, formatting and diff check. Default goldens and generated output are unchanged.
The preceding full generator, fixture, runtime-race, TypeScript and editor passes
remain recorded for unchanged code; those broader suites were not repeated for
this scaffold-only follow-up. No surface, catalogue or footprint changes.

This follow-up changes `internal/generate/scaffold_model_names.go`,
`internal/generate/scaffold_model.go`, `internal/generate/scaffold_model_test.go`,
the model-scaffolding guide and D8 evidence. No commits.

## Shared files

D8 changes `cmd/foundry/scaffold.go`, `internal/generate/scaffold.go`, `internal/generate/scaffold_developer.go` and its tests. It adds `scaffold_model.go`, its tests and two goldens. Guides changed: developer tooling, model generation, model extension slots and team adoption; the new model-scaffolding guide owns the complete recipe. Exact paths and hashes are in the evidence JSON. Previously accepted lane changes remain intact.

## Docs notes

For lead-owned milestone docs: the existing model scaffold emits handwritten ORM table and extension declarations; only contract DTO codecs still generate. Applications explicitly compose table registration helpers. Field-role and relation customization is handwritten because D8 preserves the existing flag set. Generator model/query emitter retirement remains Stage 8c.

For the P2 correction: scaffold names reserve the complete emitted symbol set, including DTO helpers; numeric suffixes resolve collisions while preserving ordinary names and persisted identities.

## Rebase validation at 6d80111

D8 scaffold/CLI races, root build, generator/CLI vet, docs-check and diff-check
passed with the r6 cache. The earlier full acceptance remains historical; this
is the requested quick check on the rebased source. Stage 9 generation is
reserved for the lead's integration barrier. Logs: `/private/tmp/goway-stage9-d1/`.
