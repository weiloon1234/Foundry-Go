# 01 — Repository and toolchain

## Purpose and prerequisites

Prerequisite: [00](00-master-architecture-and-parity.md). Establish a maintainable framework repository and verifiable import boundary. This milestone contains no application runtime or starter project.

Rust references: root `Cargo.toml`, `Makefile`, `AGENTS.md`, `tests/fixtures/blueprint_app/Cargo.toml`, and `tests/public_api_acceptance.rs`.

## Package and toolchain decisions

- Use module path `github.com/weiloon1234/Foundry-Go`, following the Rust project's repository owner and the selected Go repository name. This is the intended import identity; it does not create or publish a remote repository.
- Keep the Go version in the root `go.mod`; tools and CI read it. Nested fixture module declarations must match, checked by repository validation.
- The root package is `foundry`. Initially it contains package documentation only. Application assembly belongs to milestone 02.
- Add only directories that contain delivered files. Future public packages in the master plan are not empty placeholder directories.
- Keep one framework module. Use a nested consumer test module with a local `replace` to test external imports with `GOWORK=off`.
- Generated Go files will be checked in when generation exists; temporary binaries, credentials, caches and local workspaces are ignored.

## Delivered repository shape

```text
go.mod, doc.go, README.md, CHANGELOG.md, AGENTS.md
Makefile, .gitignore, .editorconfig
blueprint/                         # Complete design suite
docs/guides/contributing.md
internal/cmd/checkdocs/            # Standard-library repository checks
tests/fixtures/consumer/           # Import-only module; not a boilerplate
```

The future `cmd/foundry`, feature packages, examples and plugin fixtures are introduced with their implementations. Do not add a fake `Version`, no-op builder, or stub server merely to give the scaffold an exported symbol.

## Consumer contract

The milestone 01 fixture imports `github.com/weiloon1234/Foundry-Go` from a separate module. It verifies the package/module boundary only. It contains no server, domain models, frontend, credentials, or application scaffolding. Milestone 02 replaces the import-only smoke coverage with a real lifecycle example and assertions.

## Implementation steps

1. Create repository instructions preserving SSOT, scope boundaries, non-destructive database rules, dependency reuse/authorization, and the prohibition on commit/push/merge.
2. Add the Go module, root package documentation, and independent consumer module.
3. Add `make toolchain-check`, `fmt`, `fmt-check`, `vet`, `test`, `race`, `fixture-check`, `docs-check`, and `verify`. Formatting uses the formatter belonging to the selected Go toolchain.
4. Add a standard-library checker for local Markdown targets, the numbered suite, and fixture Go-version alignment. Keep it scoped to repository hygiene, not runtime behavior.
5. Document native host execution, checked commands, blueprint ownership, and package layout rules.
6. Run all milestone checks with the repository-selected native Go toolchain. Do not initialize Git or publish anything.

## Failure and environment behavior

The user changed this workspace to native macOS Go execution on 2026-09-15. Run Go, formatting, generation, build/test and language tools on the host from the existing repository, without a VM or SSH bridge. Completed VM checks remain historical evidence. Go toolchain auto-selection may fetch the version required by go.mod; report download or environment failures honestly. Reuse the framework's existing isolated database credentials and documented Mac loopback endpoint.

No database or Redis state is needed for this milestone. No third-party Go modules are required. Do not add dependency placeholders for future milestones.

## Acceptance checklist

- [x] The root package resolves and the independent consumer imports it.
- [x] Initial make verify passed in the VM with the required Go toolchain; current development uses the native host workflow above.
- [x] `make race` passes for current packages; this is not runtime concurrency validation yet.
- [x] Repository checks catch missing local Markdown targets and mismatched fixture Go versions.
- [x] No Go-Starter project, application runtime, generated placeholder APIs, or empty feature packages exist.
- [x] Actual verification evidence is recorded in the master roadmap.
