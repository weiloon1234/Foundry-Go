# Foundry-Go agent instructions

## Direction and authority

Build a **fully typed, Laravel-inspired Go framework**: thin consumer code, ordinary
Go performance, useful IDE completion, and explicit ownership. Consumer fixtures
are acceptance examples, not starter applications.

- Work in Foundry-Go. Its source, public contracts and executable consumers are the
  implementation authority; do not consult sibling implementations for new work.
- For milestone work, start with the [blueprint index](blueprint/README.md) and
  [master status](blueprint/00-master-architecture-and-parity.md). The master owns
  scope/status; subsystem blueprints own design. Planned examples are not shipped APIs.
- Inspect the relevant implementation and closest consumer before designing an API.
  Read only the guides needed for the task. Preserve unrelated changes.
- Confirm material scope or compatibility changes when the request does not already
  authorize them. Resolve routine choices from existing contracts and patterns.

## Consumer experience and types

- Put reusable infrastructure in the framework; applications supply domain models,
  concrete DTOs, business handlers and explicit declarations. Improve a missing
  framework capability instead of making every consumer rebuild it.
- Prefer configured `application.New` for ordinary assembly. Keep direct foundation
  providers as the advanced composition path. Use constructors and small interfaces;
  feature packages must not import root application assembly. Keep private
  implementation under `internal` and package dependencies acyclic.
- Preserve concrete model, key, field, actor, request, response and payload types
  across APIs. Reuse existing typed identifiers. Do not replace these contracts
  with `any`, string dispatch or `map[string]any` for convenience. Heterogeneous/raw
  boundaries must be explicit and validated before entering typed domain code.
- Inject retained dependencies into constructors. Use `context.Context` for
  cancellation, deadlines and existing typed request metadata, never a global
  service bag or `ctx.global.config`/`ctx.session.actor` substitute. Guarded handlers
  receive concrete actors through the existing transport contracts.
- Named services use their own typed names and the shared configuration/registry
  path. A default aliases one configured instance; never create an extra default
  resource, select by map order or fall back silently on an unknown explicit name.
  Keep empty-family and default validation consistent with [named services](docs/guides/named-services.md).

- Snapshot caller-owned configuration and declaration maps/slices, including nested
  mutable values, at the existing registration/build boundary. Treat registered
  definitions as immutable and return owned inspection snapshots. Reuse the typed
  snapshot paths; do not invent reflection-based copying of services or callbacks.
  Keep application state instance-owned, with no mutable process-global application
  configuration or registries. Independently built applications must stay isolated;
  explicitly injected shared dependencies retain their declared ownership contract.

## Shared owners and compatibility

- Reuse logic, declarations, schema graphs, validation rules and constants at their
  source. Change the shared owner and its consumers instead of copying a local fix.
- Go declarations and registered descriptors drive runtime codecs, validation,
  manifests, OpenAPI and TypeScript. Do not hand-maintain parallel wire schemas.
  Persistence models are not implicitly public response DTOs.
- Preserve omitted/null/value semantics with existing `value` types and generated
  drafts. A supplied zero, false or empty string is not an omitted field.
- Never hand-edit generated output or discard its ownership manifests. Change its
  handwritten declaration or generator, then regenerate with the matching framework.
- Follow existing Go layout and naming; add a directory with its first real
  implementation. Avoid placeholder APIs, unnecessary wrappers and duplicate libraries.
- Root [go.mod](go.mod) owns the Go requirement. Keep fixtures and tools aligned;
  do not hard-code a second SDK version in scripts. Review the
  [compatibility policy](docs/compatibility.md) for public/generated/persisted changes.
- Dependencies needed for requested Foundry-Go work are preauthorized by the user
  (2026-09-16). Inspect and reuse existing dependencies first; this does not authorize
  unrelated tools, duplicate implementations or arbitrary upgrades.

## Runtime ownership, failures and performance

- Make owners and borrowers explicit. Validate configuration and declarations before
  acquiring runtime resources; register cleanup before fallible startup. Drain
  admitted work before closing dependencies.
  A timeout ends a wait; it does not prove a callback exited or release its capacity.
- Reuse [callback isolation](internal/callback/invoke.go) for extensibility boundaries
  that must contain panic/Goexit. Keep actual callback ownership until exit.
- Custom `Is`, `As`, `Unwrap` and formatting methods execute application code too.
  Where framework error classification traverses arbitrary extension errors, reuse
  [bounded error inspection](internal/errorgraph/walk.go) inside the existing owner.
  Preserve classification precedence and treat incomplete inspection conservatively.
  Ordinary matching on known native/framework errors need not be replaced blindly.
- Use safe framework faults and typed/redacted diagnostics. Do not log arbitrary
  error formatting, secrets, credentials, private payloads or partially hydrated models.
- Typed identity, authenticated credentials and token scopes do not replace current
  resource/tenant authorization. Authority errors must never grant access. Preserve
  the existing request-scope freshness and registered policy contracts described in
  [authentication](docs/guides/authentication.md).
- Never turn unknown commit/publication outcomes into success, definite rollback
  or an automatic retry. Preserve typed reconciliation identity. Use the existing
  transactional outbox for required durable dispatch; in-memory after-commit work
  is not crash durable. Keep retry and receiving-side deduplication explicit.
- Bound work, queues, input sizes and retained state using existing policy owners.
  Preserve batching/eager loading and cancellation; avoid hidden N+1 I/O. Support
  performance claims with representative measurements and their conditions.

## Implementation and verification cadence

1. Complete one coherent implementation/milestone, regression sources, consumer
   examples and documentation **before Go compilation/tests**. Source review is the
   implementation-phase check; do not compile after each minor edit or subsystem slice.
2. Enter the verification/fix loop: collect failures, batch corrections, then rerun
   affected checks. Never weaken a guarantee or silently skip a requirement to get green.
3. At milestone acceptance, run `make verify`, relevant races/integration and the
   required consumer/compiler/generation/editor/client checks. Re-audit the completed
   implementation, fix findings and verify the final source before closing the milestone.
   Review its consumer experience before starting the next milestone.
   Use the [Foundry acceptance skill](.agents/skills/foundry-acceptance/SKILL.md) for
   this workflow or a requested implementation audit, not after every edit.
4. Documentation/instruction-only edits require factual and local-link checks
   (`make docs-check`); do not rebuild the framework or create cold caches for them.

[Makefile](Makefile) owns commands and batching. Run Go, formatting, generation and
language tools natively on this macOS workspace. Reuse warm caches for ordinary
checks, avoid competing full builds, and check disk headroom before expensive runs.
Keep benchmark cold caches task-owned and remove them after retaining evidence.
Do not clear shared caches during live work or as a routine test prerequisite.

Update affected guides/examples and the changelog for delivered behavior. Update
master milestone status only from actual current-source results. Report skipped or
blocked checks honestly; historical acceptance does not verify a new revision.

## Data and version control

- Inspect existing private test configuration without printing it. Use existing
  project services and isolated, non-destructive test data. Never wipe/reset/drop
  databases or test schemas, flush shared stores, or start duplicate database servers.
- Never overwrite working credentials or put them in source, logs, fixtures or chat.
- Never run `git commit`, `git push` or `git merge`. Leave version control to the user.
  Publishing, tagging, deployment and externally delivered messages need authorization.

## Scoped instructions

Read the applicable file before editing its subtree, including when starting at
repository root. These add local invariants without restating this file:

| Area | Instructions |
| --- | --- |
| Database runtime and typed queries | [database/AGENTS.md](database/AGENTS.md) |
| HTTP endpoints and middleware | [http/AGENTS.md](http/AGENTS.md) |
| Authentication, credentials and authorization | [auth/AGENTS.md](auth/AGENTS.md) |
| Storage disks, adapters and transfers | [storage/AGENTS.md](storage/AGENTS.md) |
| Declaration discovery and generated APIs | [internal/generate/AGENTS.md](internal/generate/AGENTS.md) |
| Independent consumers and plugins | [tests/fixtures/AGENTS.md](tests/fixtures/AGENTS.md) |
| Verification, packaging and measurements | [tools/AGENTS.md](tools/AGENTS.md) |

Maintain shared rules here and specialized rules in their owning subtree. Add a
skill only for a repeatable workflow; link existing contracts instead of copying
module manuals or storing transient task status in instructions.
