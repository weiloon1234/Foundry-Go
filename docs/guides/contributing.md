# Contributing to Foundry-Go

## Framework boundary

Foundry-Go is a library/framework repository. Applications consume its public packages. The nested [consumer fixture](../../tests/fixtures/consumer/README.md) checks that external module boundary; it is not the future starter project.

Read the [blueprint index](../../blueprint/README.md) and [master roadmap](../../blueprint/00-master-architecture-and-parity.md) before implementation. Work on one milestone at a time. Inspect the referenced Rust source and real usage before finalizing a Go API, then prove the Go version in an independent fixture.

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
| `make fixture-check` | Vet and compile/test the independent consumer module |
| `make generate` | Regenerate the consumer fixture's model/enum code |
| `make generate-check` | Type-check generated output and fail on stale files without rewriting them |
| `make gopls-install` | Explicitly install the pinned development tool into `bin/` |
| `make agent-smoke` | Verify real gopls against the consumer, requiring `FOUNDRY_TEST_GOPLS` to select an existing approved executable |
| `make docs-check` | Check local inline Markdown link targets, contiguous blueprint numbering, and fixture Go requirements |
| `make release-tools-check` | Verify the independent module packager and native measurement harness |
| `make verify` | Run the normal repository gate |
| `make race` | Race-enabled framework/tool and consumer checks |
| `make test-postgres` | Required real PostgreSQL acceptance, including the independent consumer, with race detection |

`GOWORK=off` is set by Make so a local workspace cannot hide module-boundary mistakes. The consumer and two plugin fixtures use test-only local replacements; the root module has none. The [release gate](../release-checklist.md) separately builds packaged modules in copied consumers with every replacement removed.

Use ordinary native Go compiler and GC defaults for host development. Do not
carry historical VM-specific optimization or tiny heap settings into native
acceptance. If measured capacity requires it, GOFLAGS=-p=1 limits simultaneous
package compilation without removing checks; choose concurrency from available
host memory. A GC soft limit does not cap total process memory or solve a
compilation unit whose live objects exceed available RAM.

Keep acceptance tests grouped in feature packages, as with the smaller
[join packages](../../tests/fixtures/consumer/joinqueries), projections and newer
advanced queries. Shared fixture helpers belong under the consumer's internal
directory. Resource settings are development controls, not framework requirements.

Repeated public generic API changes can accumulate large ordinary/race compilation caches. Before a large rebuild, inspect filesystem space and the directory returned by `go env GOCACHE`; allow room for both the cache and temporary build/link files. Once builds have stopped, `go clean -cache` clears rebuildable compilation artifacts; the next build recreates them. On a small disk, clear disposable compilation cache before or between the separate gates when their artifacts would not fit. If compilation reports a full disk, record the environment failure and rerun the failed check after space is restored. The [nullable correlation fixture](../../tests/fixtures/consumer/correlations/nullable/) also separates complex joined scopes from the other correlation tests to reduce the compiler's live memory requirement.

`make test`, `fixture-check`, `race` and `test-postgres` discover every package and run tests in bounded batches. `TEST_PACKAGE_BATCH_SIZE` in the Makefile owns the default and can be overridden for local capacity. This limits temporary test/link artifacts without reducing coverage. Failed or empty package discovery fails the gate; the PostgreSQL runner preserves required database execution, race instrumentation and fresh test runs in every batch. Compilation caches still need room and may need clearing between ordinary and race modes.

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
Documentation-only corrections use docs-check and factual review.

Milestone status lives only in the master roadmap. Each subsystem blueprint owns its API and semantics. Delivered guides explain available APIs; planned snippets stay in blueprints until implementation makes them compile-checkable.

Use inline Markdown links with relative local paths. The repository checker validates target files/directories, not remote URLs or heading fragments, and ignores fenced code blocks and matched inline code spans. It is a lightweight repository check, not a full Markdown parser. Keep future package/file names in code spans until those files actually exist.

Add focused behavior tests for real guarantees, compile-fail fixtures for type safety, and integration tests for actual databases/providers. For [managed model time](model-timestamps.md), inject `testkit.Clock` through application assembly (or `database.WithClock` for direct pools); advance it instead of sleeping to test timestamp changes. Infrastructure cancellation still uses real context deadlines. Do not claim that a memory fake certifies distributed or cloud behavior. Isolate test data without destructive database resets. Update [CHANGELOG.md](../../CHANGELOG.md) for user-visible changes.

No Git commits, pushes, merges, releases or remote repositories are created by these commands.
