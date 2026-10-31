# Contributing to Foundry-Go

## Framework boundary

Foundry-Go is a library/framework repository. Applications consume its public packages. The nested [consumer fixture](../../tests/fixtures/consumer/README.md) checks that external module boundary; it is not the future starter project.

For milestone work, read the [blueprint index](../../blueprint/README.md) and
[master roadmap](../../blueprint/00-master-architecture-and-parity.md). Foundry-Go
source, public contracts and executable consumers are the implementation authority.
Inspect the relevant implementation and closest consumer before changing an API.
Historical cross-repository mappings do not authorize consulting sibling projects.

## Environment and toolchain

Run this workspace's Go, formatting, generation, build/test and language-tooling
commands natively on macOS from the framework repository. This follows the user's
2026-09-15 workflow change; completed VM runs remain valid historical evidence.
Do not route new Go commands through a microVM or SSH. Source, generated artifacts
and ignored local configuration remain in the existing host checkout.

```sh
go version
make verify
make race
```

Root [go.mod](../../go.mod) owns the required Go version. Automatic toolchain
selection can obtain that version if the installed SDK is older; never lower the
module requirement to work around an environment error. The formatter comes from
the selected toolchain's GOROOT. Use a native executable for the host architecture;
a Linux gopls binary from a historical VM run cannot execute on macOS.

Approved runtime dependencies are pinned in root go.mod. The approved gopls tool
is pinned in [tools/gopls.version](../../tools/gopls.version) and installed into
ignored bin/ with an explicit make gopls-install. Normal verification never
installs it. Existing version approvals remain valid when preparing their native
host binaries; needed new dependencies are preauthorized, provided they do not duplicate an existing dependency’s function. Keep database and
provider credentials out of tracked files and command output.

## Commands

| Command | Purpose |
| --- | --- |
| `make toolchain-check` | Show the Go version selected for this module |
| `make fmt` | Format current Go source, including the consumer fixture |
| `make fmt-check` | Fail if source formatting differs from the selected formatter |
| `make vet` | Static analysis of framework/tool packages |
| `make test` | Current framework/tool tests |
| `make fixture-check` | Vet and test both plugin fixtures, the independent consumer and the Go-way fixture |
| `make generate` | Regenerate framework-owned declarations and both consumer fixtures, removing obsolete managed field notices |
| `make generate-check` | Type-check generated output and fail on stale files without rewriting them |
| `make gopls-install` | Explicitly install the pinned development tool into `bin/` |
| `make agent-smoke` | Verify real gopls against the consumer, requiring `FOUNDRY_TEST_GOPLS` to select an existing approved executable |
| `make docs-check` | Check local Markdown link targets, explicit future-work claims against completed milestones, contiguous blueprint numbering, and fixture Go requirements |
| `make release-tools-check` | Verify the independent module packager and native measurement harness |
| `make verify` | Run the normal repository gate |
| `make race` | Race-enabled framework/tool and consumer checks |
| `make test-postgres` | Required real PostgreSQL acceptance, including the independent consumer, with race detection |

`GOWORK=off` is set by Make so a local workspace cannot hide module-boundary mistakes. The consumer, Go-way and two plugin fixtures use test-only local replacements; the root module has none. The [release gate](../release-checklist.md) separately builds packaged modules in copied consumers with every replacement removed.

Use ordinary native Go compiler and GC defaults for host development. Do not
carry historical VM-specific optimization or tiny heap settings into native
acceptance. If measured capacity requires it, `GOFLAGS=-p=1` limits simultaneous
package compilation without removing checks; choose concurrency from available
host memory. The `Makefile` canonically exports `GOFLAGS` containing `-trimpath`
(while preserving any caller-provided flags) so that action IDs omit absolute
workspace directories and multiple worktrees or checkouts share one build cache.
A GC soft limit does not cap total process memory or solve a
compilation unit whose live objects exceed available RAM.

Keep acceptance tests grouped in feature packages, as with the smaller
[join packages](../../tests/fixtures/consumer/joinqueries), projections and newer
advanced queries. Shared fixture helpers belong under the consumer's internal
directory. Resource settings are development controls, not framework requirements.

Ordinary and race builds retain separate compilation artifacts. Before a large
rebuild, inspect disk headroom and `go env GOCACHE`; allow room for the cache and
temporary build/link files. Reuse warm caches and keep expensive gates sequential.
Never trim a cache while a build uses it. If a resource failure interrupts a gate,
retain its completed evidence, restore capacity and resume the failed segment with
the same command and flags. Remove only task-owned cold measurement caches after
their processes exit and their evidence is retained. See
[query build resources](query-build-resources.md) for measured cache behavior.
The [nullable correlation fixture](../../tests/fixtures/consumer/correlations/nullable/)
separates complex joined scopes to bound the compiler's live memory requirement.

`make test`, `fixture-check`, `race` and `test-postgres` discover every package and run tests in bounded batches. `TEST_PACKAGE_BATCH_SIZE` in the Makefile owns the default and can be overridden for local capacity. This limits temporary test/link artifacts without reducing coverage. Failed or empty package discovery fails the gate; the PostgreSQL runner preserves required database execution, race instrumentation and fresh test runs in every batch. Compilation caches still need room for both ordinary and race artifacts.

`TEST_TIMEOUT` in the Makefile sets the per-package timeout for normal, race, required-PostgreSQL and agent-smoke gates. Its default is 20 minutes to allow complete suites on slower hosts. Independent real-gopls probes use Go’s parallel testing with a shared maximum of four active sessions; Go test’s `-parallel` option can impose a lower limit. Every probe keeps its own server session, source checks and semantic assertions. Individual gopls operations still have a 45-second deadline. Override the package budget with, for example, `make TEST_TIMEOUT=25m verify`; direct `go test` commands must supply their own `-timeout` when running the full language-tooling suite.

## PostgreSQL acceptance

Use this framework's current project-scoped PostgreSQL test account. Native Go
connects directly to the configured local service using the private .env.test or
an explicitly supplied test URL. Verify the endpoint and database identity when
local services move; switching build hosts does not provision or migrate a
database. Do not start a replacement server or reuse another application's
credentials. Keep connection files private and URL-encode credentials correctly.

`make test-postgres` accepts `FOUNDRY_TEST_POSTGRES_URL` from the process environment or an ignored private `.env.test` file at the repository root. The file must be a regular file with no group/other permissions. It contains exactly one `FOUNDRY_TEST_POSTGRES_URL=value` assignment; blank lines and comments are allowed. Values are literal, with no shell sourcing, `export`, quote stripping or interpolation. The command never prints the URL and requires real database execution in both modules.

Tests create unique `foundry_test_…` schemas and retain them for inspection. They apply ordinary migrations and write only their isolated records. There is no reset, DROP or TRUNCATE cleanup. Missing configuration fails this required gate; ordinary `make test` skips PostgreSQL acceptance when no URL is supplied. An unavailable or incorrect database fails rather than silently skipping.

## Documentation and tests

Complete each milestone's implementation, test sources, consumer examples and
documentation before running Go compilation, generation, formatting or tests.
Use source review during implementation. Then collect failures in a consolidated
verification round, batch the fixes, and rerun the affected checks together.
Run the complete repository gate against the final source before declaring the
milestone complete. Do not repeat an unchanged full suite merely because a
focused check has finished. Failures and unresolved interactions determine which checks need to run
again; preserve the evidence for checks that already passed. Required race,
PostgreSQL, generation, editor and consumer coverage still applies at completion.
Documentation-only corrections use docs-check and factual review. The checker
rejects explicit forms such as `remain milestone 07 work`,
`planned in milestone 08` and `Milestone 09 remains open` when the master table marks that
milestone complete. Dated guide records and blueprint design/history retain
their historical wording. Other factual claims still need source review.

Milestone status lives only in the master roadmap. Each subsystem blueprint owns its API and semantics. Delivered guides explain available APIs; planned snippets stay in blueprints until implementation makes them compile-checkable.

Use inline Markdown links with relative local paths. The repository checker validates target files/directories, not remote URLs or heading fragments, and ignores fenced code blocks and matched inline code spans. It is a lightweight repository check, not a full Markdown parser. Keep future package/file names in code spans until those files actually exist.

Add focused behavior tests for real guarantees, compile-fail fixtures for type safety, and integration tests for actual databases/providers. For [managed model time](model-timestamps.md), inject `testkit.Clock` through application assembly (or `database.WithClock` for direct pools); advance it instead of sleeping to test timestamp changes. Infrastructure cancellation still uses real context deadlines. Do not claim that a memory fake certifies distributed or cloud behavior. Isolate test data without destructive database resets. Update [CHANGELOG.md](../../CHANGELOG.md) for user-visible changes.

No Git commits, pushes, merges, releases or remote repositories are created by these commands.
