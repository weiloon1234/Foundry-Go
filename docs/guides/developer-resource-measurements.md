# Native developer resource measurements

Foundation native measurements passed on 2026-09-17 against an independent private module
candidate. The [C05 continuation results](#consumer-startup-c05-native-results) below
add configured startup and runtime measurements. [Recorded results](../evidence/production24-native.json) contain the raw
phase timings, memory samples and generated sizes; interpretation follows below.
Historical acceptance durations and constrained VM runs remain separate evidence.

After the [release preparation](../release-checklist.md), run:

```sh
python3 tools/measure-release.py .cache/release-candidate-24 \
  --go /absolute/path/to/go --gopls /absolute/path/to/gopls \
  --govulncheck /absolute/path/to/govulncheck
```

Choose existing native executables. The Go requirement comes from the candidate's
root go.mod, and the actual SDK/version/architecture is retained in the report.
The optional scanner flag controls vulnerability collection; omitting it leaves
security review explicitly `not-run`. It does not install tools, start databases
or use application credentials. Three completion/hover rounds are the default;
`--rounds` permits 1–10, and `--timeout` bounds each command (default 30 minutes).

Every attempt needs a fresh candidate directory. The runner verifies its input
hashes before creating `measurements/` exclusively. Download/setup time has
separate records. Mutations are limited to cloned consumer source, module sums
and isolated caches. A failure retains logs, completed measurements and an
`incomplete` report. Nothing is deleted to make a retry appear successful.

## Conditions and phases

Record hardware model, physical/logical CPUs, RAM, OS, SDK, gopls build identity,
selected modules, source hashes and free disk before execution. Keep the machine
otherwise idle and retain whether power/thermal or competing workloads affected
the run. The harness strips inherited compiler/GC/architecture flags and private
environment values, disables user Go environment configuration and automatic
toolchain selection, and uses ordinary native compiler defaults. `-mod=readonly`
controls dependency changes; it does not change compiler optimization.

Current format-2 candidates have three profiles reusing checked-in source.
Historical format-1 candidates have ordinary/full only and remain supported:

- **Ordinary:** one persisted model with named ID, scalar, nullable and binary
  fields, generated drafts, typed predicates/aggregate and a small linked command.
  Its command compiles query ASTs and opens no service.
- **Configured:** one typed model plus `application.New` startup, two named memory
  caches and HTTP, with no external database or cloud account required. It uses
  the same model schema edit and probes typed default-cache completion/hover.
- **Full consumer:** the independent acceptance fixture, including its query
  combinations and plugin imports. Its build excludes test execution; the full
  framework test/compiler-negative/race/editor suite is measured separately.

For each profile, generation uses a prebuilt candidate generator with an empty
dedicated Go build cache, then an unchanged repeat. Existing generated files and
ownership manifests must reproduce byte-for-byte. The build phase uses a
different empty cache, followed by an unchanged build. A `Revision string` field
is then added to the copied profile model; regeneration and the dependent build
measure an actual incremental schema edit. Report generated file counts/bytes
before and after the edit. The original checkout is never changed by this probe.

Cold here means an empty dedicated Go or gopls cache with module downloads already
complete. It does not mean a flushed OS file cache, cold disk, or reboot. Cold
generation and cold compilation have separate caches. There are no race flags,
package-specific compiler flags, tiny GC heaps or shared acceptance caches in
these profiles. Do not compare results with different cache or toolchain settings
without identifying those differences.

Every editor command starts a fresh gopls process, matching `foundry agent` usage.
The first completion has an empty gopls disk cache; later completion/hover calls
reuse it. The Go build cache is warm after the profile build. JSON `timing`
separates process startup/initialize from the request/response interval. Request
time includes gopls loading caused by that request; full wall time additionally
includes source preparation and process shutdown. These are fresh-process agent
latencies, not claims about a continuously running IDE server. The response must
contain the expected generated method and the source must remain unchanged.

## Memory and interpretation

Native `/usr/bin/time -l` supplies maximum process RSS. Sampling every 200 ms
additionally records the largest observed `compile` process RSS and the sum of
the owned process tree's RSS. A zero compiler sample can mean no compiler ran or
a short process escaped sampling. The sum can count shared pages repeatedly;
neither it nor one process's maximum is an exact concurrent physical-memory peak.
Retain raw resource output, timing and sampling limitations alongside the values.

Publish each raw phase plus medians/ranges for repeated editor operations. Compare
the ordinary profile separately from the broad fixture. Set CI/developer machine
budgets from measured working sets with headroom for the OS, editor and concurrent
work; a Go heap limit is not a total-memory guarantee. If capacity is insufficient,
record the failure and a separately labeled constrained profile. Do not silently
disable compiler optimizations or omit coverage to report an ordinary-build pass.

Current runs also retain per-profile import counts, linked ordinary/configured
binary sizes, both executable smoke results and configured runtime benchmarks.
The runtime cases separately cover startup/shutdown, retained/default/named
selection, equivalent direct/configured handlers and loopback HTTP round trips.
Three benchmark samples use ordinary native compiler defaults. They do not run
concurrently with acceptance or another profile build.

The final report must state whether default consumer builds/editor use fit the
tested host and how generated size and incremental cost change with the added
field. Remaining resource limitations belong in release notes and the developer
experience audit. Timing alone says nothing about production request throughput.

## Recorded foundation candidate

The measured host was Mac16,5, arm64, 16 physical/logical CPUs, 64 GiB RAM,
macOS 26.6.2, Go 1.27.1 with CGO enabled, and existing gopls v0.23.0. No other
Foundry verification ran concurrently. OS file caches, background workloads,
power and thermal conditions were not controlled or instrumented. These are
single cold/incremental observations and three editor rounds on one host.

The three module archives were consumed through a private file proxy with no
local replacements. All 60 selected dependency modules had license files and
`go mod verify` passed. Download/setup preceded the phase caches. Both profiles
reproduced their generated output byte-for-byte before the schema edit.

| Profile / phase | Wall seconds | Maximum process RSS MiB | Sampled tree RSS MiB | Sampled compiler RSS MiB |
| --- | ---: | ---: | ---: | ---: |
| ordinary / generation-cold | 4.34 | 521.7 | 694.2 | 420.1 |
| ordinary / generation-unchanged | 0.25 | 43.3 | 1.6 | 0.0 |
| ordinary / generation-edited | 1.41 | 545.8 | 597.1 | 540.6 |
| ordinary / build-cold | 3.30 | 525.7 | 489.5 | 452.2 |
| ordinary / build-unchanged | 0.24 | 150.4 | 1.3 | 0.0 |
| ordinary / build-edited | 1.19 | 549.5 | 585.4 | 414.6 |
| full / generation-cold | 61.43 | 2679.1 | 12992.0 | 2679.1 |
| full / generation-unchanged | 0.95 | 181.6 | 137.9 | 0.0 |
| full / generation-edited | 2.15 | 493.0 | 572.5 | 492.9 |
| full / build-cold | 60.24 | 2640.9 | 12224.9 | 2640.9 |
| full / build-unchanged | 0.97 | 192.7 | 231.1 | 0.0 |
| full / build-edited | 1.44 | 527.0 | 589.0 | 525.8 |

The ordinary profile had one generated file, 68,111 bytes before the edit and
71,967 bytes afterward. The full consumer had 144 generated files, 4,308,000 bytes
before and 4,311,856 bytes afterward. One additional persisted string field added
3,856 generated bytes in either profile. The linked ordinary command passed.

| Profile / request | Three wall times, seconds | Median initialize, seconds | Median request, seconds |
| --- | --- | ---: | ---: |
| ordinary / complete | 1.731, 1.475, 1.493 | 0.049 | 0.289 |
| ordinary / hover | 1.503, 1.499, 1.507 | 0.037 | 0.218 |
| full / complete | 2.487, 1.979, 2.193 | 0.037 | 0.784 |
| full / hover | 2.174, 1.966, 1.993 | 0.037 | 0.763 |

All requests found the expected generated method without changing source. The
fresh-process wall time includes work beyond the initialize/request timings.
The raw evidence retains every individual initialize/request measurement.

## Developer and CI budgets

Default builds and editor use fit this host. For planning, reserve 8 GiB RAM for
ordinary consumer development and 32 GiB for the broad fixture/full development
job, with at least 50 GiB free disk for isolated dependencies, generation/build
and editor caches. These are operating budgets with headroom, not certified
minimum hardware or latency service levels. Review them as consumer scope grows.

The full profile sampled about 12.7 GiB across its process tree during cold
generation; its largest compiler process was about 2.62 GiB. The ordinary profile
sampled less than 0.7 GiB. Shared-page counting and 200 ms sampling limit these
figures. The run increased used filesystem space by about 13.9 GiB, including
isolated caches; that filesystem delta is not an exact per-process disk quota.
The earlier 4 GiB RAM / 20 GiB disk VM is not accepted for the default broad
fixture. No constrained profile or special compiler/GC flags were substituted.

Use focused application checks during routine development. Complete milestone
source/test/documentation batches first, then run the consolidated acceptance
and fix loop. The full compiler/editor/framework suite is a separate workload;
its 739.5-second post-audit gate is not the cost of one ordinary model edit.

## Consumer startup C05 native results

[Recorded C05 evidence](../evidence/consumer-startup-c05-native.json) uses private
candidate `v0.0.0-candidate.startup.5`, after the complete C01–C05 implementation
audit and final 494.9-second acceptance gate. The host/toolchain remained M4 Max,
64 GiB, macOS 26.6.2, native Go 1.27.1 and gopls 0.23.0. No other Foundry gate ran
concurrently. OS caches, power, thermal state and unrelated background work were
not controlled. Source hashes stayed unchanged; final acceptance documentation
was added afterward. Nothing was published.

All three profiles reproduced their generated output exactly before adding the
single `Revision string` field. It added 3,856 generated bytes per profile.
Ordinary/configured each had one consumer-generated file (68,111 bytes); full had
153 files (4,546,152 bytes). Framework-generated files live in its packaged module.
The module graph had no local replacements. All 60 selected modules had license
or notice files, with all 57 external versions and notice hashes unchanged from
foundation acceptance. Private archive/log checks found no configured credentials.

| Profile | Imported packages (`./...`) | Linked command bytes | Cold build seconds | Unchanged build seconds | Schema-edit rebuild seconds |
| --- | ---: | ---: | ---: | ---: | ---: |
| Ordinary | 174 | 5,346,498 | 3.54 | 0.25 | 1.20 |
| Configured | 548 | 35,281,362 | 20.95 | 1.65 | 4.97 |
| Full | 708 | Multiple commands; no aggregate size | 66.22 | 2.84 | 5.24 |

| Profile | Cold generation seconds | Unchanged generation seconds | Schema-edit generation seconds | Highest sampled tree RSS MiB in build/generation |
| --- | ---: | ---: | ---: | ---: |
| Ordinary | 4.57 | 0.25 | 1.41 | 733.4 |
| Configured | 20.95 | 0.48 | 4.25 | 6,305.9 |
| Full | 56.21 | 1.17 | 4.99 | 11,522.5 |

The largest compiler process was 2,739.5 MiB; sampled tree RSS is not an exact
physical-memory peak. The filesystem used-space delta was about 19.26 GiB for
three isolated profile caches and dependencies. These default builds fit the
tested host. The earlier foundation planning budgets (8 GiB ordinary, 32 GiB
broad development, 50 GiB free disk) remain conservative operating guidance,
not certified minima. Configured development needs headroom above its observed
6.16 GiB tree sample for the OS/editor and concurrent work.

| Profile / editor operation | Three fresh-process wall times, seconds | Median initialize ms | Median request ms |
| --- | --- | ---: | ---: |
| Ordinary / complete | 1.756, 1.543, 1.483 | 42.34 | 292.07 |
| Ordinary / hover | 1.539, 1.535, 1.514 | 40.64 | 223.59 |
| Configured / complete | 2.262, 1.771, 1.761 | 37.63 | 584.05 |
| Configured / hover | 1.781, 1.763, 1.773 | 35.18 | 584.34 |
| Full / complete | 2.486, 2.029, 2.023 | 42.38 | 730.13 |
| Full / hover | 2.241, 2.027, 2.015 | 37.74 | 708.27 |

Every response contained the expected typed symbol without source edits. These
are fresh-process agent calls, not continuously running editor timings.

| Runtime case | Median ns/op | Median B/op | Allocations/op |
| --- | ---: | ---: | ---: |
| Build/provider Start/Shutdown | 333,159 | 333,303 | 2,654 |
| Retained handle control | 1.763 | 0 | 0 |
| Default selection | 91.53 | 0 | 0 |
| Named selection | 90.51 | 0 | 0 |
| Direct handler and test recorder | 6,354 | 5,243 | 74 |
| Configured handler and test recorder | 6,344 | 5,242 | 74 |
| Loopback HTTP including client | 47,435 | 16,838 | 197 |

Each row has three one-second samples retained in evidence. Startup includes
two memory caches, prepared HTTP routes and the typed model/query check; it does
not start a listener. The direct/configured handler ranges overlap, so the data
shows no meaningful handler-cost difference from assembly. Requests retain their
cache handle. The loopback row includes network/client work and is not a throughput
or tail-latency guarantee. Both linked commands passed, and both serve commands
answered HTTP before exiting 0 on SIGINT with their configured shutdown timeout.

C03's 82.6 µs startup and 9.97-second configured cold build covered a narrower
configuration and different measurement targets. C05 adds supporting-service
assembly, generated feature configuration, two caches and typed model work. Its
21-second configured cold build and 0.33 ms startup are real costs; disabling
services avoids acquisition but does not remove imported Go packages. The prior
foundation full cold build was 60.24 seconds versus C05's 66.22 seconds. These
single observations on different source/capability sets do not isolate a regression
percentage. The native evidence retains all phases rather than hiding slower ones.

Security review used govulncheck 1.8.0 and database timestamp
2026-09-15T18:39:25Z. Framework/full-consumer scans reported no affected packages
or functions; the unchanged module-only unused OpenPGP advisory is documented in
the [acceptance guide](consumer-startup-acceptance.md#final-acceptance-boundaries).

## Additional performance/security review

The [2026-09-18 review](performance-security-review.md) records fresh ordinary,
configured and full-consumer profiles, five-sample before/after cache and stream
measurements, and three-sample runtime checks. Its [native evidence](../evidence/performance-security-native.json)
retains cold, unchanged and edited builds, real editor timings and RSS. Historical
C05 numbers above remain unchanged.
