# Foundry-Go blueprints

This suite specifies the **Foundry-Go framework**. It does not specify or create a starter application. Consumer fixtures are framework tests, not application templates. Foundry-Go source and tests are the authority for new work.

Start with [00 — Master architecture and parity](00-master-architecture-and-parity.md). It owns milestone status, dependency order, scope decisions, and historical module mapping. Each numbered document owns the design and acceptance criteria for its subsystem; link to those contracts instead of copying them.

The [consumer startup continuation](consumer-startup/README.md) builds configured,
typed application assembly on the delivered foundation. Its status is also owned
by the master.

The [typed API continuation](typed-api/README.md) follows the Go-only
[source audit](../docs/typed-api-gap-audit.md), addressing generic DTOs, unions,
forms/request hooks, nested binding, isolated HTTP tests and inbound idempotency.

The [security hardening continuation](security-hardening/README.md) implements the
confirmed cache issue and all four additional review items.

## How to implement a milestone

1. Read the master document and the milestone's prerequisites.
2. Inspect the existing Foundry-Go implementation, public consumer fixtures and tests. Reuse their contracts and patterns. Historical cross-repository references do not define new work.
3. Write independent consumer fixtures for the proposed APIs alongside implementation. Compile and exercise them in the milestone verification phase.
4. Implement the smallest complete behavior slices in the documented order, including failure paths.
5. Finish the current milestone implementation, tests and documentation, then run its verification/fix cycle. Do not run full suites or every compiler/editor probe after each slice. Record actual evidence in the master status table.
6. Review the delivered consumer experience before starting the next milestone. Do not implement the whole roadmap in one pass.

## Reading the examples

Except where explicitly identified as delivered, examples in this suite are **planned API contracts**, not APIs currently available for import. Expression fragments omit imports and surrounding application definitions. Naming and signatures must be made concrete and compile-checked when their milestone begins; changes to an accepted contract must update its owning blueprint and affected examples together.

Available APIs are documented in the consumer guides; the master roadmap records verified implementation status. No database, HTTP server, ORM, authentication, storage, or WebSocket runtime is implied by the existence of a blueprint.

## Common completion gate

- Public APIs compile in an independent consumer module using only public imports.
- Behavioral tests cover the documented guarantees and failures.
- Type errors promised by the API have compile-fail coverage.
- Generation is deterministic and current, once generation exists.
- Existing fixtures stay green; guides, package documentation, and changelog match delivered behavior.
- Resource ownership, cancellation, concurrency, and shutdown are explicit for every long-running subsystem.
- Test data is isolated without wiping databases or resetting existing state.

Older numbered documents retain historical external source mappings. Links in new continuation blueprints are relative to this repository. New implementation and verification must work from Foundry-Go alone.

## Implementation and verification cadence

User direction on 2026-09-16: complete each milestone's code, tests and documentation
first, then enter its verification/fix cycle. Avoid repeating full regression,
all compiler-negative cases, all generation checks and all editor probes for each
small change or subsystem slice. During coding, use source review and keep writing tests as features are implemented.
The user clarified that compilation also belongs in the milestone verification
phase: do not compile/test after minor changes. Collect findings and batch fixes
before each verification round, repeating the test/fix loop until fully done.

At the milestone gate, run the complete required acceptance suite. Diagnose and
fix failures using targeted checks, then verify the final source state before
marking the milestone complete and beginning the next. Unexecuted or deferred
checks must be recorded honestly. This changes timing, not the completion criteria
or the final framework-wide audit.
