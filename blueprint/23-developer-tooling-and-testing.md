# 23 — Developer tooling and testing experience

## Purpose and prerequisites

Prerequisites: [03](03-generation-and-language-tooling.md) and implemented feature APIs. Complete the framework's development experience around the test harness and tools introduced earlier.

Rust references: `src/cli`, `src/testing`, `src/database/scaffold.rs`, `src/foundation/doctor.rs`, `tools/foundry-agent`; `tests/testing_layer_acceptance.rs`, `blueprint_fixture_acceptance.rs`, `plugin_fixture_acceptance.rs`.

## Public contract

`cmd/foundry` is the framework development command. Applications use the CLI kernel to register their own typed commands through shared bootstrap; development generation does not require starting application services.

Implemented commands include:

```text
foundry generate --check
foundry doctor
foundry agent complete --workspace <consumer> --file <file> ...
foundry make model <Name> --table <table> --dir <package>
```

Scaffold individual framework-consumer artifacts, not a Foundry-Go-Starter project. Model/migration/DTO/job/command templates generate code compatible with the selected Foundry version and explicit application paths.

## Implementation slices

1. Consolidate command registration, argument decoding, help/errors, signal cancellation and exit statuses.
2. Artifact scaffolding through the shared generator with path checks and overwrite protection.
3. Doctor and route/job/schedule/plugin/contract inspection with redacted configuration source information.
4. Public `testkit` HTTP/realtime clients, typed response decoding, factories, clocks, storage/mail/job/event fakes and policy-aware auth helpers.
5. Consumer LSP tooling guides and compile-checked recipes covering the mature framework surface.

Database inspection includes typed query-plan helpers corresponding to Rust's `explain` and `explain_analyze` methods. Reuse compiled statements, bindings, context cancellation and the selected executor; ordinary plan inspection must not execute the planned statement. Execution analysis is an explicit operation and must retain transaction requirements for locked or mutating statements. Test successful plans, failures, cancellation and preservation of bound parameters. Plain `Compile`/`SQL` output alone does not complete this reference capability.

## Test and isolation rules

Use production application assembly in test harnesses. Fakes replace explicit external capabilities; they must not bypass authorization or transaction semantics accidentally. `ActingAs`-style helpers skip credential issuance but still run the real guard/policy restrictions relevant to the test.

Factories use generated mutation APIs and model hooks. Parallel tests get distinct application state, cache namespaces and database identities/records. A test database name does not authorize `DROP`, `TRUNCATE`, or reset helpers.

CLI generation and doctor must not read/log secrets unnecessarily or silently install tools. Missing prerequisites produce actionable diagnostics and nonzero status where checks are required.

## Acceptance

Test scaffold collisions/path traversal, fresh-project generation, installed-version compatibility, real gopls completion, fake assertions, clock isolation, test cleanup after failure, policy-aware authentication and user-facing command errors. Compile all guide examples and keep consumer/plugin fixtures green. Apply the [common gate](README.md#common-completion-gate).

## Delivered implementation and acceptance

The [tooling guide](../docs/guides/developer-tooling-and-testing.md) and
[query-plan guide](../docs/guides/query-plans.md) document the delivered APIs.
Typed command/bootstrap and pure inspection, create-only artifact scaffolds,
offline doctor, owned test clients/fakes, generated-model factories and typed
PostgreSQL plans passed native verification and independent consumer review.
All implementation, test sources and documentation preceded compilation/tests.

Final `make verify` passed in 430.4s with existing PostgreSQL/Redis required,
actual gopls and Node/TypeScript, both plugin modules and the application consumer.
The gate passed all 915 compiler cases and 365 editor scenarios (12 and eight
additions), complete generation/freshness, vet, formatting and docs. Relevant
root/consumer races, failed-test cleanup, normal auth/policy behavior and bounded
plan fuzzing passed; the 15-second fuzz campaign covered 108,861 inputs.

Compiler cases share bounded native Go invocations while retaining individual
source diagnostics. Each editor scenario shares one fresh process for its three
operations with independent deadlines/overlays. Make fingerprints sources/tools
for child compiler, editor, TypeScript, generator and prerequisite checks while
preserving ordinary caching. Actual cache-invalidation/reuse tests passed.
Verification fixed a compiler diagnostic expectation and a race helper timeout;
source review corrected agent group help and completed cache-input coverage.
Failed logs were retained and resolved checks rerun before acceptance.

Milestone 24 and the requested final framework verification plus complete audit/fix
round remain required. Milestone 25 stays deferred.
