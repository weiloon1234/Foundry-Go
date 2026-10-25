# ORM performance

The independent consumer's `BenchmarkORMCompile` measures typed query creation,
validation and SQL compilation without database I/O. `BenchmarkORMPostgresReads`
compares direct `database/sql`, Foundry SQL execution and handwritten ORM model
queries at 1, 100 and 1000 rows. Every variant uses identical compiled SQL, the
same pgx connector configuration and equivalent concrete codecs. The agreement
test compares complete decoded models, including NULLs, enums, UUIDs and dates.

These are warm, sequential reads over an indexed 1000-row table in a unique
retained schema. Both pools have one connection; driver statement caches and
PostgreSQL buffers are warmed before timing. The typed variant constructs and
compiles its query on each read; SQL variants reuse the compiled statement.
The model has eight persisted columns and many unloaded relation slots, so
its collection allocation includes those slots. The measurements do not compare
other ORM libraries, concurrent saturation or transaction/hook write workloads.

Run from the repository root with an explicitly configured private
`FOUNDRY_TEST_POSTGRES_URL` and `FOUNDRY_TEST_POSTGRES_REQUIRED=1`:

```sh
make benchmark-orm
```

Keep credentials out of command text and reports. The existing private test
configuration and [PostgreSQL helpers](../../testkit/postgres/postgres.go) own
connection and non-destructive namespace setup. Read latency depends on database
placement, host load, cache warmth and the selected fields; a local ratio cannot
predict a remote application's latency.

For the Stage 11 main-versus-branch comparison, use the
[alternating-trial procedure](goway-final-measurements.md). Main runs its own
legacy fixture; do not copy this branch's ORM fixture into that checkout.

The historical optimization described below reuses immutable validated model selections, removes
an unused read compiler map, shares the fixed LIKE escaping grammar and avoids
splitting identifiers into temporary slices. Codec scanners retain only their
decoder and concrete destination while still rejecting incomplete codecs and
publishing values only after successful decode. At that revision, public/generated signatures,
query scopes, SQL text and bindings are preserved; the optimization required no consumer code changes or regeneration.
The later ORM migration is covered separately by the [migration guide](orm-migration.md).

## Measured results — 2026-10-11

Five alternating runs of the original and optimized binaries used Go 1.27.1
on an Apple M4 Max with 64 GiB RAM and local PostgreSQL 18.6. Runtime GC and
CPU settings were Go's defaults (16 logical CPUs). Compilation finished before
the timing comparison; both paths include the same adapter's schema checkout
checks. Each entry below is the median of five one-second samples.
The [acceptance evidence](../evidence/orm-performance-20261011.json) retains every
sample, range, source fingerprint and check result.

| Rows per read | Direct SQL after optimization | Typed ORM before | Typed ORM after | Added ORM cost after |
| --- | ---: | ---: | ---: | ---: |
| 1 | 62.4 µs | 69.1 µs | 67.4 µs | 5.0 µs / 8.1% |
| 100 | 197.1 µs | 218.0 µs | 204.8 µs | 7.7 µs / 3.9% |
| 1000 | 1.190 ms | 1.220 ms | 1.170 ms | Within sample variation |

The 1000-row ORM and direct-SQL ranges overlap; their median difference is not
evidence that the ORM is faster. Overall typed reads improved by 2.5%, 6.0%
and 4.0% at these sizes. Allocated bytes fell by 13.0%, 8.0% and 7.1%.
The codec improvement also benefits direct SQL using Foundry codecs.

| Build, validate and compile | Before | After | Time reduction | Allocated bytes before → after | Allocations before → after |
| --- | ---: | ---: | ---: | ---: | ---: |
| List query | 3.57 µs | 3.01 µs | 15.7% | 5335 → 4125 | 106 → 72 |
| Filter-heavy query | 6.06 µs | 4.26 µs | 29.7% | 14086 → 6057 | 153 → 106 |

Allocation profiles identified repeated LIKE replacer construction, model
selections, the unused read compiler map and identifier splitting. For large
reads, result-slice growth and complete model storage dominate allocations;
smaller scanners remove 32 bytes per scanned column without changing decoder
behavior. The result-equivalence test also verifies one application SELECT
for every tested list size; driver schema checks are separate and shared by both
paths.

`make verify` passed with PostgreSQL required and the existing real gopls.
Fresh `database/...` races and independent consumer races for model reads/writes,
tenant scopes, retrieval hooks, sets, projections, cursors and observers passed.
All 4408 source fingerprints matched before and after verification. This is
runtime ORM evidence; it does not measure Go build time or other ORM libraries.
