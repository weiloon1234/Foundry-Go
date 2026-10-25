# Stage 9: extension matching and independent consumers

Base `6d80111`, on the accepted D8 + D1 delta. No commit.

## API and consumers

`attachments.Matching`, `translations.Matching` and `metadata.Matching` return
`orm.Predicate[M]` for the declaration's concrete owner. They validate the ORM
identity before dispatch and reuse the existing manager matching implementation:
exact collection/locale or metadata version/value, identity validation, bounded
reads, attachment owner deduplication and cancellation. Empty results compile to
false; the later owner query retains its current visibility and caller filters.
Legacy and zero declarations are rejected. No Query, Table or descriptor method
was added; the public-method snapshot is unchanged.

- **profiles:** exercises all three matching functions against stored extension
  data, caller filters and fresh soft-delete visibility. Its D1 ORM lifecycle
  cutover remains intact.
- **multifactor:** provider reads, password compare-and-swap, account locking,
  MFA updates, retirement and test setup now use `AccountRecords` and
  `AccountIdentity`. Existing freshness, locking, rollback and reconciliation
  assertions remain. A SQL parity test checks the keyed `FOR UPDATE` query.
- **idempotenthttp:** ORM workspace binding and order insertion, preserving
  scoped authorization, transaction ownership and idempotency assertions.
- **isolatedhttp:** ORM insert/read and `factory.Define`, preserving separate
  application pools, scoped transactions and effect assertions. Its editor probe
  now targets handwritten field handles.
- **teamworkflow:** ORM parent/child binding, pagination, patch assignments and
  submission insertion. Omitted values still cause no assignment; null clears
  nullable title, and zero budget remains an explicit write. Existing tenant,
  transport and outbox assertions are retained.

These consumers are operationally ported; model directives/generated APIs and
explicit parity oracles remain until the lead's generation barrier. Their new
`Tables()` helpers and declarations pass through the ordinary `orm.Check` tests.
No generated output, manifest, schema declaration, module pin, Makefile, OpenAPI
or TypeScript artifact was edited. The 39 retained generated/contract artifacts
in these packages are byte-identical to HEAD.

## Type and footprint coverage

Ported catalogue row: `attachment_query_owner`, now using the actual bounded
extension assertion. Four additional Go-way negatives cover translation/metadata
owner mismatch, metadata value mismatch and a plain string used as a locale.
Catalogue totals: 548 ported, 34 pending, 444 unaffected.

Matching goldens compare legacy/new SQL and binds, two driver statements per
lookup (`SET LOCAL` and the bounded select), row closure, error classification,
empty results, malformed identities, overflow and cancellation. Separate tests
reject zero and legacy declarations before the manager can run.

The identical three-matcher importer workload measures **8,438 functions new**
versus **8,432 old** (Go 1.27.1, darwin/arm64; archive T/t symbols). The six extra
function symbols are the new package adapters; both probes retain the existing
unbound descriptor families. New ceilings are 9,704 and 9,697 respectively
(`ceil(measured × 1.15)`). No existing ceiling or bound-handle workload changed.
These are static importer measurements, not runtime throughput claims. The
full footprint suite passed. Tight inherited probes remain `q_with` 1,211/1,211
and `aggregate_families` 4,366/4,369; thin extension handles remain 1,481 and the
bound slot workload is 2,083 (old namespace 9,730).

## Remaining work and requests

- **Articles / attachment owner:** `attachments.AddFiles` in
  `attachments/slot_write.go` accepts legacy `ManySlot[M,K]`, but its ORM namespace
  exposes `ManyHandle[M,K]`. The upload handler and PostgreSQL write regression
  need an equivalent typed bulk helper that validates the bound slot and batch
  limit before opening files, returns every attempted result including the failed
  one, and preserves partial publication without retry. No consumer loop or
  legacy-handle substitute was introduced. The query counter helper used by its
  tests is unchanged and is not the blocker.
- **Lane B integration:** caching, limiting, Redis, coordination, messaging,
  pagination and downstream passwords/client contracts wait on mutatorqueries
  input/storage foundations. Recovering waits on typed draft-unset.
- **Lane C integration:** shared model/query/projection dependents remain pending.
- Mailing, storing, outgoing, security, validationinput and validationrules have
  no legacy model operations to port; their retained service/contract APIs remain.

## Shared files and Docs notes

Shared edits in this increment: D's `attachment_query_owner` catalogue row,
own Go-way compiler registry and footprint entries, consumer README and the
`internal/agent` probe. Guide: `docs/guides/model-extensions.md`.

Lead-owned docs note: bounded owner-typed extension matching now supports ORM
queries; four additional independent consumer workflows use ORM declarations.
Retained generation and contract export remain lead-owned at the barrier. Live
PostgreSQL, full TypeScript and the complete editor gate remain lead checks.

## Targeted acceptance

Build, vet, tests/races, 5 Go-way and 31 consumer compiler cases, ORM surface
guards and all footprint budgets passed. The attachment race run initially hit
a sandbox loopback-listener denial; the package passed when rerun with loopback
access. The interrupted consumer build was rerun on r7 and the remaining checks
continued there; completed earlier segments were retained. The changed
`scoped-orm-fields` probe passed against the approved real gopls. Catalogue,
docs-check, formatting and diff-check results are in the evidence.

[Evidence and source hashes](goway-stage9-d23.json). Logs:
`/private/tmp/goway-stage9-d23/`. Live PostgreSQL and full integration gates were
not run. No model-generator or contract export was run outside the lead barrier.
