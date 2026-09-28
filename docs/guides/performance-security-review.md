# Performance and security review — 2026-09-18

This additional review follows the accepted C01–C05 startup delivery. The review is
complete. Three confirmed findings are fixed, the final native gate and broad
race verification passed, all 52 fuzz targets passed, and fresh independent
consumer measurements/security scans completed. The [acceptance record](../evidence/performance-security-review.json)
and [native measurements](../evidence/performance-security-native.json) retain the evidence.

## Scope and method

The review covers framework boundaries, runtime costs, dependencies and developer
tools. Source inspection prioritizes untrusted input, authentication, cryptography,
SQL and filesystem confinement, cancellation, contention, resource ownership and
bounded work. It is a risk-focused source review with framework-wide regression
verification, not a claim that every line or every possible attack was examined.
Application authorization, infrastructure configuration and third-party hosted
services remain outside this local review. Tests use native macOS and existing,
isolated PostgreSQL/Redis fixtures; no real messages or provider writes are sent.

| Area | Reviewed boundaries |
| --- | --- |
| HTTP and WebSocket | Body/frame limits, proxy/origin handling, CSRF/CORS, signing, cookies, transport lifetime, streaming and admission |
| Authentication and cryptography | Model/guard ownership, password work limits, sessions, refresh replay, challenges, MFA, AEAD purpose binding |
| Database | Parameterized query compilation, typed cursors, transaction/savepoint ownership, pool routing and scoped schema restoration |
| Storage, uploads and imaging | Root/key confinement, atomic publication, stream ownership, byte/pixel/workspace limits, attachment cleanup |
| Cache and Redis | Atomic expiry/counters, lock contention, LRU/capacity, integrity, leases, rate limits and bounded pub/sub |
| Jobs, scheduler, events and outbox | Admission, callback lifetime, leadership/lease loss, bounded catch-up, durable publication and retry identity |
| Configuration and lifecycle | Typed schema bounds, isolation, shutdown order, bounded telemetry and protected diagnostics |
| Contracts and developer tools | JSON tree/schema budgets, typed codecs, generator ownership, crash recovery, plugin distribution confinement |
| Supporting services | Mail transport/TLS, outbound HTTP, escaped templates/report output and model-owned notification access |
| Dependencies | Fresh source/binary vulnerability scans, module integrity, tool pins and TypeScript lockfile audit |
| Performance | Same-host before/after cache and stream benchmarks, existing runtime benchmarks and packaged consumer build/editor profiles |

## Verified findings and fixes

- **R01 — persistent cache expiry under contention.** File and PostgreSQL metadata
  operations previously sampled time before acquiring their atomic lock. An entry
  expiring during the wait could appear live or be renewed. They now evaluate
  liveness and the new relative TTL inside the lock using one shared policy.
  Real adapter contention tests cover lookup, finite/forever renewal and TTL origin.
- **R02 — memory cache capacity pressure.** Every insertion into a full cache
  scanned all entries, including when none could have expired. A conservative
  earliest-expiry bound skips unnecessary scans. A due sweep recomputes the bound;
  TTL edits, replacement and removal preserve expiry-before-LRU behavior. This
  uses constant metadata per backend, not another entry index or background task.
- **R03 — outbound stream progress.** A custom reader returning `(0, nil)`
  indefinitely could consume CPU until timeout. Owned request/response streams
  now reuse the shared 100-empty-read guard. Progress resets the counter; failures
  remain sticky and actual close/read ownership remains retained.

## Dependency results

The fresh vulnerability database snapshot is dated 2026-09-15. Framework and
consumer scans report the existing module-only GO-2026-5932 OpenPGP advisory;
no affected package or function was reported. Release tooling and the scanner
binary have no findings. The gopls binary retains module-only x/mod and x/text
advisories with no affected symbol reported. The TypeScript lockfile audit reports
zero vulnerabilities. Module integrity verification passed. Final import graphs
contain 654 framework and 708 consumer packages, with no affected OpenPGP package.
All 42 declared runtime module versions, 44 recorded license-file hashes and 12
module/tool input files match the baseline. The fresh independent graph also
matched all 60 selected modules and 73 license/notice/patent hashes; only the three
private candidate version labels changed. See the [dependency review](../dependency-review.md)
for advisory dispositions. These are bounded scanner results, not proof that
unknown vulnerabilities do not exist.

## Verification and measurements

The final native `make verify` gate passed in 528.1s,
and `make race` passed in 1235.4s across the framework and independent
plugin/consumer modules. PostgreSQL, Redis, real gopls, compiler rejection,
TypeScript, generation freshness, formatting, vet and documentation checks used
the existing required native setup. Existing unchanged Go test caches were
retained. The focused regressions also passed 20 race-enabled repetitions.
All 52 existing fuzz targets passed (6,045,567 reported executions), with two
workers and a three-second search per target plus seed coverage. Gate and fuzz
timings include compilation; they are not application runtime measurements.

Original-source overlays reproduced all four expiry cases on both durable cache
adapters across five repetitions (40 expected assertion failures), and the
non-progressing stream failure. The fixed source remained in place throughout.
One source/test/documentation batch preceded verification. No implementation
correction was required by this verification round. Completion review checked
all six runtime diffs, seven new test/benchmark sources, dependency reachability,
source fingerprints and the final evidence; it found no additional required fix.

Five-sample same-host medians with ordinary compiler settings:

| Case | Before | After | Allocation effect |
| --- | ---: | ---: | --- |
| Full cache, 100 persistent entries, one eviction/Put | 738.5 ns | 330.3 ns | Unchanged: 520 B, 6 allocations |
| Full cache, 10,000 persistent entries | 62,320 ns | 330.3 ns | Unchanged: 520 B, 6 allocations |
| Full cache, 10,000 finite entries, none due | 75,114 ns | 338.7 ns | Unchanged: 520 B, 6 allocations |
| Owned 64 KiB stream lifecycle | 5,180 ns | 5,315 ns | One additional allocation, approximately 64 B |

The cache cases use a fixed clock and capacity+1 rotating keys. A due expiry
sweep still scans entries; the improvement applies when no expiry is due.
File/PostgreSQL cache quota enforcement retains coarse locking and entry/file
scans. Their correctness is verified here; production throughput under capacity
pressure is not measured by the memory-cache benchmarks.
The stream guard's small measured cost buys bounded empty-read handling while
preserving close ownership. These are five-sample medians, not a statistical
throughput claim for a production application.

The additional 28 runtime benchmark cases and seven configured-consumer cases
passed with three samples each. Configured startup/shutdown measured 332.6 µs;
default/named selection measured about 91 ns with zero allocations. Equivalent
direct/configured HTTP handlers measured 6.29/6.32 µs, and the loopback HTTP case
50.3 µs. Files/uploads stream within their configured budgets. XLSX allocation
totals grow with rows; compression includes codec workspace. Total allocated
bytes are not peak live memory. Raw samples and ranges are retained in the record.

Fresh packaged profiles use isolated module, build and editor caches and no
workspace replacements. Module integrity, generation reproducibility, edited
model regeneration, real completion/hover, executable smoke checks and final
source vulnerability scans passed. Their raw build/editor/RSS records are in
the native evidence; the historical [C05 measurements](developer-resource-measurements.md#consumer-startup-c05-native-results)
remain a separate comparison. Only this assistant's competing verification
work was excluded during measurements; OS caches, desktop workloads and thermal
state were not controlled. No public release or external delivery was performed.

| Packaged profile | Cold build | Unchanged build | Edited-model build | Imports |
| --- | ---: | ---: | ---: | ---: |
| ordinary | 3.50s | 0.25s | 1.40s | 174 |
| configured | 21.40s | 1.85s | 4.65s | 548 |
| full | 60.76s | 2.78s | 4.89s | 708 |

Cold means an empty dedicated Go cache with dependencies already downloaded,
not a cold OS or disk. The broad consumer remains the expensive developer
profile; no compiler or coverage setting was weakened to improve these numbers.


The native host is an Apple M4 Max with 16 physical/logical CPUs and 64 GiB RAM.
Resource and fresh-process editor measurements were:

| Profile | Cold generation | Largest process RSS | Largest sampled tree RSS | Editor command median |
| --- | ---: | ---: | ---: | ---: |
| ordinary | 4.48s | 0.58 GiB | 0.74 GiB | 1.48s |
| configured | 20.35s | 2.19 GiB | 6.29 GiB | 1.73s |
| full | 58.33s | 2.65 GiB | 12.18 GiB | 2.15s |

Tree RSS sums owned processes at 200 ms intervals, can count shared pages more
than once, and can miss short peaks. It is not exact physical-memory usage.
Editor medians combine three completion and three hover commands, each starting
a fresh gopls process; they are not steady-state IDE keystroke latency. Full
generation and compilation remain substantial costs of the broad typed fixture.
The ordinary/configured profiles show the separate cost of smaller consumers.
