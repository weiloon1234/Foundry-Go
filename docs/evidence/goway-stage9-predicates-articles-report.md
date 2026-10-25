# Stage 9: predicate redaction and articles

Base `6d80111`, preserving the accepted D8, D1 and D2/D3 delta. No commit.

## API and behavior

ORM model predicates and legacy model/HAVING predicates now retain their AST
through a shared opaque pointer in `internal/querycore/predicate_state.go`.
Constructors and readers wrap/unwrap that storage; AST validation, compilation,
value binding and ownership checks are unchanged. No formatter or other method
was added. Sentinel regressions cover direct values, pointers, private and
nested wrappers, `%v`, `%+v`, `%#v`, error interpolation, identity scopes,
conjunctions, negation and zero predicates. An old/new binding assertion proves
that redaction does not remove the actual SQL parameter.

`attachments.AddFilesTo(ctx, manyHandle, owner, files)` reuses the legacy
`AddFiles` batch engine. It validates binding and batch size before opening any
file, retains every attempted result including the first failure, and preserves
partial publication and unknown outcomes without retry. New negatives reject
wrong owners, wrong key types and single-file handles.

Explicit descriptor operations needed by the articles assertions now use
`translations.FieldOf`, `metadata.KeyOf`, `attachments.OneCollection` and
`attachments.ManyCollection`. These package functions return the existing
declaration through private callbacks, retaining its registration identity and
concrete owner/key/value types. They add no generic fields, embedding chains or
methods to the handles. Registration and zero-handle regressions exercise the
existing managers. No capability is passed to application callbacks.

## Articles migration

Articles now uses ORM inserts, keyed writes, reads, relations, pagination,
chunking and cleanup registration. Its upload handler and partial-publication
regression use `AddFilesTo`. Explicit batch benchmarks and independent stored
metadata, collection/link, rollback, transaction visibility, locking, cleanup
and statement-count assertions remain in place. The eight affected editor
probes now resolve handwritten handles and their framework methods.

The twelve retained generated/contract artifacts in articles are byte-identical
to HEAD. No model directive, schema, codec declaration, ownership manifest,
module pin, Makefile, OpenAPI or TypeScript artifact changed. Generated APIs
remain for parity oracles and the lead's generation barrier.

## Rows, surface and footprint

Three additional Go-way cases: `attachment_files_owner`,
`attachment_files_key`, `attachment_files_single`. No additional old catalogue
row changed in this increment; the catalogue remains 548 ported / 34 pending /
444 unaffected. No public-method snapshot addition or removal.

Go 1.27.1, darwin/arm64; the existing harness counts archive T/t symbols, including
names containing spaces. Identical batch-helper importer probes measure:

| Probe | Functions | Ceiling |
| --- | ---: | ---: |
| extension_files | 1,474 | 1,696 |
| extension_files_old | 7,244 | 8,331 |
| q_with | 1,211 | 1,211 |
| aggregate_families | 4,366 | 4,369 |
| extension_handles | 1,481 | 1,704 |
| extension_slots | 2,083 | 2,478 |

Only the two new probe ceilings are introduced, using `ceil(measured × 1.15)`.
Every existing ceiling is unchanged. All footprint subtests passed. These are
static importer measurements, not runtime throughput claims. An initial manual
symbol counter undercounted names containing spaces; the repository harness's
counts above are authoritative.

## Acceptance

Completed segments: root build; vet on ORM/query/shared cores, attachments,
translations, metadata and agent packages; races on the ORM/query/shared cores
and extension packages; articles build/vet/race; eight real gopls probes; the
complete footprint suite; catalogue; documentation checks. Live PostgreSQL and
the full generation/TypeScript/editor integration gates remain lead-owned.

The interrupted fixture/compiler and fixture-race segments were rerun on r8:
all three new compiler cases, matching/descriptor races and fixture vet passed.
Completed r7 segments are retained because their source is unchanged. The final
matching run corrected a fixture-driver mistake: a nil batch emits a default
row, so empty reads must return an explicit empty batch. No production behavior
was changed for that correction. Formatting and diff checks pass.

[Command results and source fingerprints](goway-stage9-predicates-articles.json)
record the segmented acceptance and corrected setup failures.

## Shared files and Docs notes

Shared edits are the mechanical predicate adapters under `database/orm` and
`database/query`, the querycore holder, the agent probes, D's Go-way compiler and
footprint entries, the matching fixture, consumer README and the slot guide.
The evidence lists each source path and hash. No changes were made under
`database/orm/rows` or `database/orm/expr`, or to lead-owned docs.

Lead docs note: model predicates redact retained literals even through private
wrappers; ORM extension handles support batch file publication and explicit
registered-descriptor operations; articles is operationally migrated. Remaining
consumer prerequisites from the accepted D2/D3 report are unchanged.
