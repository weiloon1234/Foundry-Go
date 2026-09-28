# Consumer startup acceptance

All five milestones, C01–C05, are accepted. C05 passed its pre-audit native gate
in 534.1 seconds and its final post-audit gate in 494.9 seconds, then passed private
packaging, native measurements, command signal checks and security review.
The [master blueprint](../../blueprint/00-master-architecture-and-parity.md#consumer-startup-delivery)
owns milestone status. [Final evidence](../evidence/consumer-startup-c05.json),
[native results](../evidence/consumer-startup-c05-native.json) and the
[complete change audit](consumer-startup-audit.md) retain the proof and limitations.

## Runnable compact consumer

The [profile executable](../../tests/fixtures/consumer/configuredprofile/cmd/profile/main.go)
uses the same public API as the larger PostgreSQL/auth/upload fixture. From the
independent consumer module:

```sh
go run ./configuredprofile/cmd/profile
```

By default it boots two memory caches, serves a real loopback HTTP request and
shuts down. `-serve` keeps the HTTP kernel running until interrupted; `-config`
selects an optional TOML deployment file. `PROFILE__HTTP__SERVER__ADDRESS` can
select a fixed listener. These are framework acceptance examples, not a starter
application product or a production authentication template.

The entry point is ordinary Go:

```go
settings, err := configuredprofile.Load(configPath)
if err != nil { return err }
app, err := configuredprofile.Build(ctx, settings)
if err != nil { return err }
return app.Run(ctx, foundation.HTTP)
```

`Build` registers typed domain routes through `application.New(settings).HTTP(...)`.
The [route constructor](../../tests/fixtures/consumer/configuredprofile/routes.go)
resolves its default once and retains `cache.Cache[string, string]`. Runtime
requests decode no configuration and perform no service lookup. The default
aliases `Primary`; `NamedCache` explicitly selects the independent `Reports`
store. Both selectors have the same `cache.StoreName` type.

Use Go structs for type declarations and defaults. Generated config schemas/keys
supply checked overrides; TOML and environment supply deployment values. There
is no runtime Go config execution or model-directory scan. Store configuration
and shared services in constructor fields; use `context.Context` for cancellation
and attribution and concrete actor arguments for authenticated handlers.

## Proof matrix

| Requirement | Current source and evidence |
| --- | --- |
| Single typed config schema | [C01 acceptance](../evidence/consumer-startup-c01.json), generator config fixtures and `startupconfig` consumer |
| Named/default identity and isolation | [C02 acceptance](../evidence/consumer-startup-c02.json), infrastructure tests, configured profile identity test |
| PostgreSQL-only database | Infrastructure database settings and existing `database/postgres` adapter |
| Four cache backends with unchanged domain code | [Switch regression](../../tests/fixtures/consumer/configuredprofile/backend_switch_test.go), shared cache backend contracts and C02 native evidence |
| Local/S3/R2 selection | Same switch regression: local read/write and real cloud signing; [storage certification](storage.md) retains its recorded live-provider scope |
| Minimal typed web bootstrap | [C03 acceptance](../evidence/consumer-startup-c03.json), compact profile plus PostgreSQL/auth/upload bootstrap fixture |
| Model-specific actors and browser CSRF | Bootstrap persistent test, C03/C04 compiler failures and HTTP regressions |
| Supporting services and one selected kernel | [C04 acceptance](../evidence/consumer-startup-c04.json), jobs/events/scheduler/realtime and manager fixtures |
| Constructor-safe audit dependency | [Scope regression](../../tests/fixtures/consumer/configuredprofile/audit_scope_test.go), exact pool, restored schema and business rollback |
| Ownership and independent applications | C02/C03/C04 startup rollback, shutdown, borrowed resource and race checks |
| IDE completion and compiler rejection | Real gopls probes and independent batched negative compiler cases in the full gate |
| Generated output and onboarding | Full generation/docs checks and both runnable consumer commands |
| Private module boundaries | Format-2 private candidate, three canonical module archives, independent module cache, no local replacements and both linked commands passed |
| Native resource/runtime cost | [Three-profile measurements](developer-resource-measurements.md#consumer-startup-c05-native-results) and three samples of each runtime benchmark passed |
| Complete post-verification re-audit | [Whole-change review](consumer-startup-audit.md), batched improvements and final 494.9-second native gate passed |

## Measurement interpretation

The configured profile intentionally imports application assembly and its built-in
adapters. Disabled resources stay unopened, while Go still compiles imported
packages. The ordinary model-only profile remains the narrow comparison; it is
not an equivalent web application. The full fixture measures the broader feature
catalog. Report these costs separately.

`BenchmarkSelection` measures retained handles versus typed default/named lookup.
Ordinary request code retains its handle; lookups belong in constructors.
`BenchmarkHTTPHandler` serves the same route/cache/security middleware through
an explicitly built router and the configured router. It includes the response
recorder harness. `BenchmarkHTTPRoundTrip` includes the real kernel, HTTP client,
loopback transport and response draining. Neither isolated microbenchmark is a
production throughput or tail-latency guarantee.

Follow the existing [native measurement method](developer-resource-measurements.md).
Cold means an empty dedicated Go/gopls cache after dependency setup, not a flushed
OS cache. Retain native RSS samples, repeated editor/runtime results, source hashes,
known background-work limitations and historical comparisons. The final [C05 results](developer-resource-measurements.md#consumer-startup-c05-native-results)
include build/import costs as well as request measurements.

## Final acceptance boundaries

The runtime benchmark median was 333.2 µs for Build/provider Start/Shutdown with
two memory caches and prepared HTTP routes; it excludes listener startup.
Equivalent direct/configured handler medians were 6.354/6.344 µs with 74 allocations,
including the test response recorder. Real loopback HTTP, including the client,
measured 47.435 µs. Default/named selection measured 91.53/90.51 ns with no allocations.
Constructors retain handles for requests rather than repeating selection.

The configured profile's cold build was 20.95 seconds versus 3.54 seconds for the
ordinary model-only profile. This is the concrete import/build tradeoff of full
assembly, not an equivalent-capability comparison. Full fixture cold build was
66.22 seconds; ordinary/configured/full schema-edit rebuilds were 1.20/4.97/5.24 seconds.

The packaged framework and full consumer scanner reported no affected packages
or functions. Both retained the existing module-only
[OpenPGP advisory](https://vuln.go.dev/ID/GO-2026-5932.json); none of its packages is
imported by the measured profiles. Database timestamp and exact results are in
native evidence. External module versions and license/notice hashes match the
accepted foundation. These source scans do not guarantee absence of unknown issues.

Cloud switching adds SDK signing proof with inert credentials, local I/O and
existing provider protocol coverage. It does not claim a new live S3/R2 account
certification or external email delivery. Native PostgreSQL and Redis tests use
existing services and isolated data. No automatic migration, database reset,
source commit, push, merge or publication was performed.
