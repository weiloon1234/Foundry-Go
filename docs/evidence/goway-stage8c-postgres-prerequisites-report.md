# Stage 8c PostgreSQL prerequisites

The sensitive and mutator behavior tests now depend only on handwritten ORM
models. The pure legacy/current mutator hydration comparison has moved into
`pgsmoke/oracle_legacy_test.go`, still recorded from an actual legacy read under
`--postgres`. No final freeze or PostgreSQL execution was performed in this lane.

## Assertions preserved

| Source | Preserved guarantee |
| --- | --- |
| `pgsmoke/sensitive_test.go` | Reordered raw projection hydrates encrypted models; stored envelopes exclude plaintext; table, row and column authentication reject copied ciphertext. |
| `pgsmoke/sensitive_test.go` | Text/JSON hydration, nullable JSON presence, omitted nullable fields, encrypted updates, explicit null hydration and actual SQL NULL storage. |
| `pgsmoke/sensitive_test.go` | Password hashing and verification after persistence, omitted nullable hash, nullable hash update, equality filtering and ordering. |
| `pgsmoke/mutators_test.go` | Owned input snapshot; insert normalization; database defaults; false and null preservation; binary hydration; persisted email; update normalization and explicit null. |
| `pgsmoke/mutators_test.go` | Transaction veto rolls back preceding writes; batch veto inserts nothing; count remains one; set-based update transforms inputs. |
| `pgsmoke/mutators_test.go` | Distinct input primary key persists and retrieves; omission returns Missing without inserting. |
| `pgsmoke/oracle_legacy_test.go` | Pure email hydration parity keeps the same `goway/pgsmoke/mutators/hydration` site, under `TestMutatorHydrationParity`. The capture reads legacy SQL results, never synthesized expectations. |

Legacy duplicate wrong-row/wrong-column rejection assertions are represented by
the same ORM rejection assertions. Cross-API writes/reads became ORM writes/reads
with the same persisted guarantees. The ordinary mutator read also checks its
persisted email independently of the oracle comparison.

The audit additionally removed generated declarations from
`bridge_capability_test.go` and the two `old_text_comparison*_owner_conversion`
compile-fail sources. Handwritten legacy query declarations retain the security
coverage of both APIs, and local owner types retain both wrong-owner conversion
negatives. No production API, method surface, generated output or footprint budget
changed.

## Import boundary

`freeze-oracles` parses Go import declarations throughout the checkout and rejects
legacy model imports outside reviewed oracle comparison sources, the three
oldGolden adapter files, registered legacy footprint probes, and the legacy model
packages themselves. Regressions distinguish actual imports from descriptor package-name strings,
reject unrecorded tests even inside a recorded file, and detect escaped import
paths. Fifteen ordinary tests now live in separate behavior files. The dual-API
redaction test uses a handwritten legacy runtime declaration and preserves cause
identity, all four formatting checks, and rejection before SQL. See the review-34
follow-up evidence for checks of this stricter boundary.

There are **41 remaining importer files**, all in the recording path: 37 audited
comparison files, three oldGolden helpers and one registered legacy footprint
probe. None are in ordinary behavior tests or compile-fail cases. The audit also
includes oldreporting; its only external importers are the two listed oldGolden
helpers. There are no actual imports within oldmodels/oldreporting themselves.

Every remaining importer is listed below (paths relative to repository root):

| Importing file | Imported package(s) |
| --- | --- |
| `tests/fixtures/goway/adjustments_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/aggregate_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/audit_golden_test.go` | `foundry.test/goway/oldmodels/hooks` |
| `tests/fixtures/goway/composition_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/conditional_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/conflict_advanced_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/conflict_golden_test.go` | `foundry.test/goway/oldmodels/hooks` |
| `tests/fixtures/goway/correlation_lateral_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/expression_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/extension_handles_test.go` | `foundry.test/goway/oldmodels/extensions` |
| `tests/fixtures/goway/golden_old_loading_test.go` | `foundry.test/goway/oldmodels`, `foundry.test/goway/oldreporting` |
| `tests/fixtures/goway/golden_old_queries_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/golden_old_relations_test.go` | `foundry.test/goway/oldmodels`, `foundry.test/goway/oldreporting` |
| `tests/fixtures/goway/integration_adapters_test.go` | `foundry.test/goway/oldmodels/hooks` |
| `tests/fixtures/goway/joins_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/lifecycle_golden_test.go` | `foundry.test/goway/oldmodels/hooks` |
| `tests/fixtures/goway/load_query_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/mutators_golden_test.go` | `foundry.test/goway/oldmodels/mutators` |
| `tests/fixtures/goway/on_conditions_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/pgsmoke/oracle_legacy_test.go` | `foundry.test/goway/oldmodels/mutators` |
| `tests/fixtures/goway/reads_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/records_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/relation_aggregates_golden_test.go` | `foundry.test/goway/oldmodels/joined` |
| `tests/fixtures/goway/relation_filter_test.go` | `foundry.test/goway/oldmodels/joined` |
| `tests/fixtures/goway/relation_writes_test.go` | `foundry.test/goway/oldmodels/joined` |
| `tests/fixtures/goway/relations_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/relations_joined_golden_test.go` | `foundry.test/goway/oldmodels/joined` |
| `tests/fixtures/goway/report_projection_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/result_read_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/retrieval_scope_golden_test.go` | `foundry.test/goway/oldmodels/reads` |
| `tests/fixtures/goway/sensitive_golden_test.go` | `foundry.test/goway/oldmodels/sensitive` |
| `tests/fixtures/goway/source_write_golden_test.go` | `foundry.test/goway/oldmodels/hooks` |
| `tests/fixtures/goway/stage7_remainder_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/statement_detector_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/store_capabilities_golden_test.go` | `foundry.test/goway/oldmodels/hooks` |
| `tests/fixtures/goway/subqueries_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/terminal_writes_golden_test.go` | `foundry.test/goway/oldmodels/hooks` |
| `tests/fixtures/goway/testdata/footprint/extension_slots_old/probe.go` | `foundry.test/goway/oldmodels/extensions` |
| `tests/fixtures/goway/transaction_composition_golden_test.go` | `foundry.test/goway/oldmodels` |
| `tests/fixtures/goway/validation_database_test.go` | `foundry.test/goway/oldmodels`, `foundry.test/goway/oldmodels/hooks`, `foundry.test/goway/oldmodels/reads` |
| `tests/fixtures/goway/window_golden_test.go` | `foundry.test/goway/oldmodels` |

## Capture census

The census now contains 500 test sources and 84 paired entry points. The number of
comparison sites is unchanged: 119 full / 117 offline. Only the mutator PostgreSQL
entry point moved; both PostgreSQL sites remain required for final publication.
The recorder regression checks offline/full selection for this site. The lead's
integrated-tree `--freeze --postgres` will produce the records. No existing frozen
record was rewritten.

## Acceptance

Passed targeted checks:

- Recorder race tests, source-census/full-versus-offline planning, and the complete import inventory.
- PostgreSQL source compilation (`go test -run '^$' ./pgsmoke`); no database tests ran.
- Eight selected fixture race tests: capability security, mutator parity/ownership/redaction, distinct-input key parity, sensitive/password parity and streaming failure parity.
- Both owner-conversion compile-fail cases, with their expected `cannot convert` diagnostic.
- `go vet . ./pgsmoke ./cmd/freeze-oracles` in the goway fixture.
- Catalogue validation; changed Go files formatted; `git diff --check` clean.

The documentation check is recorded separately in the companion evidence JSON.
Recorder race and PostgreSQL compilation completed on r12 before the requested
cache rotation. The interrupted behavior race was rerun on r13; every subsequent
Go command used r13 with GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off. No previous
completed command was repeated merely to refresh its cache path.

Commands, logs and exact source hashes are in the companion evidence JSON.
PostgreSQL behavior execution and final `--freeze --postgres` remain lead-owned.
The accepted earlier harness capture/replay evidence is historical; this turn adds
targeted migration, import-boundary and capture-selection evidence.

## Docs notes

Stage 8c may retire generated model declarations after the integrated-tree oracle
freeze: sensitive/mutator behavior coverage now survives independently, and the
recorder enforces the remaining import boundary. Shared lead-owned docs unchanged.
