# Stage 7: validation/database

Implemented on top of the accepted 3a+3b delta; no commit. Other lanes' application,
seeder, pagination-consumer, datatable, source-write and typed-role work is untouched.

## API and behavior

The existing `validation/database` entry points now accept `orm.Query[M]` and
`orm.KeyColumn[M,V]`: `Exists`, `Unique`, `ExistsAll`, `UniqueAll`, `ExistsEach`,
`UniqueEach`, `Lookup` and `Scoped`. `UniqueIgnoring` retains a typed check-time
slot and a same-owner stored column with `Ne(K)`; alternate stored keys remain
supported. Nullable and mutated columns accept their non-null **stored** type.

`orm.Lookup` returns the thin `orm.ValueLookup[M,V]`, reusing the 3a stored-column
family validation and private lookup state. It observes existence without model
hydration, retrieval hooks or implicit transactions. Filters, context scopes,
soft deletes, named executors, codecs and early-exit behavior are preserved.
`AllExist` uses at most 64 SQL equality observations per statement; `AnyExist`
uses at most 64 IN operands. Empty inputs perform no I/O. Neither compares a Go
set size against SQL DISTINCT counts.

Both lookup handles retain state behind private pointers and redact formatting,
including private/nested wrapper fields and error diagnostics. Foreign embedded
column wrappers fail validation before their promoted methods can dispatch.

## Tests and catalogue

Old/new goldens compare SQL and bindings for filtered text, nullable fields,
exact decimals, nullable mutated inputs, soft deletes and refreshed tenant scopes.
They assert exact batch counts and early exit for 130 values, zero-I/O empty and
cancelled inputs, no retrieval callbacks, and matching rule descriptions, codes,
messages and issue paths. Additional tests cover trusted slot exclusions, invalid
sources/columns, zero handles, scope failures and redaction. Existing consumer
PostgreSQL scenarios now use ORM declarations; all four declarations also receive
a positive check without a database.

Five rows are ported in both consumer and Go-way compiler fixtures:
`validation_batch_id`, `validation_database_id`, `validation_database_locked`,
`validation_database_owner`, `validation_database_value`. Only those mapping rows
changed in this slice, including their synchronized source expressions.

## Footprint and surface

Go 1.27.1 darwin/arm64, archive T/t symbols, shared r3 cache:

| Probe | Functions | Ceiling |
| --- | ---: | ---: |
| validation_database (new) | 1,441 | 1,658 |
| q_with | 1,211 | 1,211 |
| aggregate_families | 4,366 | 4,369 |
| extension_slots | 2,093 | 2,478 |
| extension_slots_old | 8,530 | 35,067 |

The new ceiling is `ceil(1441 × 1.15)`. Every existing ceiling is unchanged.
The full footprint guard passes. No Query/Table methods were added.

Surface additions: `orm.Lookup`; `orm.ValueLookup` with `Validate`, `Exists`,
`AllExist`, `AnyExist`, `String`, `GoString`, `Format`; and redacting `String`,
`GoString`, `Format` on `databasevalidation.ModelLookup`. The ORM snapshot gains
17 lines over the accepted 3a+3b snapshot, with no removals.

## Acceptance

All Go commands used `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r3`,
`GOMAXPROCS=4`, `GOFLAGS=-p=4`, `GOWORK=off`.

- PASS: `go build ./...`.
- PASS: `go vet ./database/... ./internal/... ./validation/...`.
- PASS: race tests for `./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/... ./validation/...`.
- PASS: targeted Go-way validation and consumer declaration races.
- PASS: all four fixture modules' vet/tests; Go-way PostgreSQL smoke compilation.
- PASS: catalogue, `make generate`, `make generate-check`, `make typescript-check`, docs, gofmt/fmt-check and diff-check.
- PASS: direct real-gopls completion, hover and definition for `Unique` and `ExistsAll`, asserting the ORM query/column/value signatures and correct source definitions.

The catalogue initially detected stale expression text in the five migrated
consumer cases; those rows were corrected and the catalogue rerun passed.
The aggregate editor harness remains blocked by its pre-existing
`typed-auth-signed-endpoint` source-anchor check in `authenticating/composed_routes.go`.
It fails before reaching the selected validation probes. Six direct invocations
of the existing `foundry agent` CLI, with equivalent result assertions, provide
the validation editor evidence; unrelated auth probes were left unchanged.

Regeneration wrote and removed zero files in all 15 generation directories.
All generated-file hashes and all nine exported manifest/OpenAPI/TypeScript
artifacts remain byte-identical. PostgreSQL sources were compiled/vetted only;
live PostgreSQL remains the lead's gate.

Exact commands, results, timestamps, source and contract hashes are in
[the evidence JSON](goway-stage7-validation-database.json).

## Shared files

`database/orm/README.md`, `database/orm/method_surface_test.go`,
`database/orm/testdata/public-methods.txt`, `docs/guides/validation.md`,
`internal/agent/gopls_acceptance_test.go`, `validation/lookup.go`,
`tests/fixtures/goway/compiler_cases_test.go`, `tests/fixtures/goway/footprint_test.go`,
`blueprint/goway-data-layer/compilefail-mapping.tsv`.
The slice also migrates the consumer modelvalidation adapter, its two PostgreSQL
test sources, and the five existing consumer compiler cases. New runtime,
regression, probe and Go-way compiler files are listed by hash in the evidence.

## Docs notes

Stage 7 advisory database validation now uses ORM declarations, retaining typed
owners/keys/stored values, scoped existence semantics, stable rule metadata and
redacted diagnostics. Five additional catalogue rows are ported. Wire exports
and generated output are unchanged. No lead-owned documentation was edited.
