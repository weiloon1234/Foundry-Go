# Stage 7 slice 2: framework stores and scoped keyed writes

Base: `5cbe31dce4fe3c3e1aacef402ad637d161a87590`, on the accepted slice 1 patch.
No commit. Slice 1 sources remain byte-identical except the intentionally extended
Makefile generation list and footprint table.

## Stores and API

Migrated seven packages (eleven models): `countries`, `internal/jobarchive`,
`internal/idempotencystore`, `internal/mfastore`, `internal/challengestore`,
`internal/sessionstore` and `internal/tokenstore`. Together with outbox, eight of
thirteen framework store packages now use ORM declarations.

Declarations are `countries.Countries`, `jobarchive.Failures`,
`idempotencystore.Records`, `mfastore.Factors`, challenge/session `Subjects` and
`Entries`, and token `Subjects`, `Families` and `Entries`. Natural string primary
keys use the existing table `Key()` handle. Challenge pruning retains its narrow
`PruneCandidate` result through `PruneCandidateRows`.

The only ORM public surface additions are package functions `orm.Update` and
`orm.Delete`, in `database/orm/write_keyed.go`. Each takes the scoped `Query[M]`,
its own `Key[M,K]` and a `K`; Update also takes `Assignment[M]` values. Both retain
scope and primary-key predicates, reject foreign/zero handles before I/O and
reuse `writeOne`. No Query/Table methods, capability bridges or dependencies were
added. `write.go` was not edited.

Existing high-level store APIs, persisted model field shapes/tags, schema,
codecs, defaults, redaction, claim freshness, lock order and transaction ownership
are retained. Session/token raw SQL files are byte-identical. Country seeding
still excludes application-owned status, conversion rate and default selection
from conflict updates. Unknown outcomes remain errors with reconciliation data;
there is no retry path added.

## Parity, generation and typing

Twenty-three frozen transcripts, captured against the branch-base generated APIs
through a source overlay, compare SQL, bindings, hydrated-result hashes and
transaction/savepoint commands: read/write for every migrated model plus the
challenge pruning projection. Each executes exactly one data statement.
Scoped-write regressions compare the old emitter's key-predicate + Patch/Remove
expansion with ORM: ordinary writes use one data statement, writes with hooks use
two (locked read plus mutation). Wrong scope, wrong declaration/zero key,
cardinality, returning, lifecycle and unknown outcomes are covered. A separate
test checks typed unknown-commit candidates and no retry.

New compiler cases: `scoped_write_owner`, `scoped_write_key`. The existing
`country_status_contract` consumer case and its catalogue row now use the actual
`Countries.Status` handle. No catalogue disposition or stage changed.

The Makefile generation path removed twelve obsolete model/projection files and
retained ownership manifests. Countries retains generated enum output. Full
regeneration and freshness passed; no generated file was hand-edited. Nine fresh
baseline/current export artifacts, including manifest, OpenAPI, TypeScript,
React and Vue, are byte-identical. No contract source changed, so
`make typescript-check` was not required.

## Footprint

All 81 probes pass. New archive-function baselines and ceilings:

| Probe | Measured | Ceiling |
| --- | ---: | ---: |
| countries | 4,122 | 4,741 |
| jobarchive | 2,280 | 2,622 |
| idempotencystore | 2,249 | 2,587 |
| mfastore | 2,947 | 3,390 |
| challengestore | 2,558 | 2,942 |
| sessionstore | 4,166 | 4,791 |
| tokenstore | 5,315 | 6,113 |
| scoped writes | 1,111 | 1,278 |

Each new ceiling is `ceil(measured × 1.15)`. Existing ceilings did not change.
`q_with` remains 1,211/1,211 and `aggregate_families` 4,362/4,369: no growth.
These are compile-footprint counts, not runtime performance measurements.

## Acceptance

Environment: `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r2`,
`GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`; Go 1.27.1 darwin/arm64.

- PASS: `go build ./...`; `go vet ./database/... ./internal/...` plus affected stores/callers.
- PASS, segmented: races for ORM/query/querycore/ormcore, migrated store consumers,
  application and agent packages. Six application HTTP-listener tests were blocked
  by the sandbox and passed on the permission-enabled targeted rerun.
- PASS: Go-way fixture vet/test (including 81 footprints and compiler cases);
  consumer vet and affected profiles/client-contract races.
- PASS: scoped keyed-write/surface regressions and final store/projection races.
- PASS: Makefile generation/freshness, catalogue, documentation and formatting checks.
- PASS: fresh contract export comparison and focused real country gopls
  completion/hover/definition. An unrelated stale `typed-auth-signed-endpoint`
  precheck blocked the unfiltered editor harness; a temporary test overlay selected
  only the country probe. No full real-gopls suite is claimed.

Corrections included a missing import, natural-key namespace shadowing, the
challenge row's missing builder binding, and preserving MFA assignment order.
The accompanying JSON retains initial failures and targeted reruns. PostgreSQL
sources were compiled/vetted only; no live PostgreSQL/Redis or full `make verify`
acceptance is claimed.

## Shared files

`Makefile`; `application/maintenance_schedule_test.go`;
`database/orm/README.md`, `method_surface_test.go`, `testdata/public-methods.txt`;
`internal/agent/gopls_acceptance_test.go`; `docs/guides/countries.md`;
`tests/fixtures/goway/compiler_cases_test.go`, `footprint_test.go`;
`tests/fixtures/consumer/testdata/compilefail/country_status_contract/invalid.go`;
`blueprint/goway-data-layer/compilefail-mapping.tsv` (only country_status_contract).
The application test now owns a narrow old-query fixture so it continues testing
the existing pruning adapter independently of archive-store implementation.
All store/caller, new test and removed generated paths are recorded in the JSON.

## Docs notes

Lead-owned docs were not edited. Record eight of thirteen store packages migrated,
the two scoped keyed-write package functions, unchanged wire/schema contracts and
service-free acceptance limits. Remaining stores: notifications (bounded per-model
writes), webhooks (ordered UUID seeks), extensions/attachments (owner/slot adapters;
ordered UUIDs for attachments), and audit/translation source deletion (6e3).
Datatable, model-binding, factory/identity and pruning adapters remain later Stage 7
work. Stage 7 and generator retirement are not complete.
