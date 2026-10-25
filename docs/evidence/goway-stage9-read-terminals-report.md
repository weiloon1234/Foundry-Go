# Stage 9: ordinary read-family inspection and iteration

Framework gap assigned to lane A, on laneA9 at `6d80111`, above the accepted
Stage 7 remainder. No commit. Other Stage 9 migrations and H1 are unchanged.

## API and shared owner

Three additive package functions accept the existing `rows.ReadSource[S,R]`:

- `rows.CompileRead(read)` returns `orm.Statement` without I/O.
- `rows.PreflightRead(ctx, read)` validates with the executing context.
- `rows.EachRead(ctx, executor, read, func(R) error)` iterates with bounded memory.

Pass `ReadScalar`, `ReadRecord`, `ReadSet`, `ReadValues`, `Read(query)`, or a
`RecordRead` directly. Transaction-only adapters fail before I/O; their existing
transaction terminals are unchanged. No Query/Table methods or capability
families were added. Existing `Compile`, `Preflight` and `Each` signatures remain.

The compile path uses the existing `ormcore.CompileRecord`. Scalar/DTO delivery
reuses `rows.Each`. Complete model results retain the model's primary metadata
and dispatch retrieval through `ReadRecord`/`Collect`. Both the legacy API and
ORM use `ormcore.RecordBatches` for stable ordering, window accounting, keyset or
offset continuation, and isolated delivery. Keyset eligibility traces plain
SELECT/CTE boundaries to the model source; nested joins, sets and recursion
conservatively use offsets so repeated primary IDs are retained. Factories run
once per nonempty closed batch. A concrete owner can prove callbacks absent and stream models one
at a time; unknown wrappers cannot suppress callbacks. Batch size remains 100.

## Parity and contracts

The retained assertions in lane C's blockers table are covered by executable
old/new tests, without modifying its consumer packages:

- advancedqueries / ctes: scalar early-stop identity, CTE definition discovery,
  exact SQL/bindings/windows and row release;
- recordqueries / setqueries / recursivequeries / distinctqueries: complete
  records, preserved nested queries, shared recursive definitions, set reads,
  pool/session release and cancellation; joined and UNION ALL duplicate IDs
  survive batches, including behind CTEs;
- framequeries / expressionqueries / scalarqueries / keyqueries: computed scalar
  compilation, repeated named-frame definitions/bindings, CASE, text,
  geographic expressions and computed window partition keys.

Tests also cover 100/100/5 model batches, factory/retrieval counts, offset
fallback, scan/close/hook failures, panic/Goexit, nil/zero/transaction sources,
zero-window validation and fresh scope context. The PostgreSQL smoke source
exercises a 205-row CTE stream and early-stop/session reuse for record, scalar and
set terminals; live PostgreSQL execution remains with the lead.

Three new negative cases pin scalar, record and set callback result types.
No legacy catalogue rows were ported or reclassified by this framework slice.

## Footprint and acceptance

Final source measurements, commands, outcomes and hashes are recorded in
[the evidence JSON](goway-stage9-read-terminals.json). Measurements count T/t
symbols in actual independent consumer package archives, using the existing
footprint harness; they are not runtime timing or whole-binary size claims.
New probe ceilings use `ceil(measured * 1.15)`. Existing ceilings are unchanged.
The final acceptance uses r7, `GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

| Probe | Functions | Ceiling |
| --- | ---: | ---: |
| read_scalar (new) | 1,165 | 1,340 |
| read_record (new) | 1,002 | 1,153 |
| read_set (new) | 1,050 | 1,208 |
| q_with | 1,211 | 1,211 |
| aggregate_families | 4,366 | 4,369 |
| rows_each | 899 | 1,034 |

Targeted build, vet, tests and races passed, including old/new parity fixtures.
The three compiler negatives, all 133 footprint probes, catalogue, formatting,
docs and diff checks passed. Smoke sources compiled and vetted without a live
database. The broader ORM/query/querycore suite passed on r7; affected packages
and fixtures were rerun after the final composition correction. Initial negative
source filenames and formatter invocation were corrected before the final pass.

Shared files touched by this slice: `database/orm/record.go`,
`database/orm/rows/read.go`, `database/orm/method_surface_test.go`,
`database/orm/testdata/public-methods.txt`, `internal/ormcore/record.go`,
`database/query/read_lifecycle_chunks.go`,
`tests/fixtures/goway/compiler_cases_test.go`, and
`tests/fixtures/goway/footprint_test.go`. New implementation, regression, probe,
smoke and evidence files are listed in the JSON. The package results guide is
updated. Generated files, module files and Makefile are unchanged by this slice.

## Docs notes

Lead changelog/blueprint note: ordinary scalar, record, set and complete-record
reads now support typed package-level inspection, preflight and bounded
iteration. Complete models retain retrieval lifecycle, batch/window semantics
and resource ownership through the shared legacy/ORM iteration owner. Both APIs
use offset continuation when composed sources cannot prove unique primary keys,
preserving duplicate IDs in joins, sets and recursion.

Lane C can use these terminals to remove the corresponding inspection/streaming
blockers. CTE scalar source composition, correlated DISTINCT/HAVING, JSON
natural-key containment, relation-filter scalar grouping and cursor compilation
remain with their assigned feature owners. A2 still awaits the lead's integration
signal. No decisions are needed for this completed API family.
