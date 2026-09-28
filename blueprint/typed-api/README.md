# Typed APIs and complete HTTP application tests

This continuation follows the Foundry-Go source assessment requested on 2026-09-18.
Its objective is Laravel-like developer convenience with concrete Go types, native
IDE support and measured runtime/build costs. The [audit](../../docs/typed-api-gap-audit.md)
explains which claims were confirmed, partial or already addressed.

The [master](../00-master-architecture-and-parity.md#typed-api-delivery) owns status.
This directory owns design and acceptance contracts. Read in order:

1. **T01** — [Generic DTO schema generation](01-generic-dto-generation.md)
2. **T02** — [Discriminated payload unions](02-discriminated-unions.md)
3. **T03** — [Forms and typed request lifecycle](03-forms-and-request-lifecycle.md)
4. **T04** — [Alternate keys and nested scoped binding](04-scoped-model-binding.md)
5. **T05** — [Isolated PostgreSQL HTTP tests](05-isolated-http-tests.md)
6. **T06** — [Inbound idempotent operations](06-inbound-idempotency.md)
7. **T07** — [Integrated acceptance and final re-audit](07-acceptance-and-audit.md)

## Existing contracts to preserve

The framework already has typed handler registration, explicit DTO responses,
typed pagination envelopes, declared public errors, Optional/Nullable PATCH values,
validated JSON/query/multipart input, typed model resolvers and a transactional
outbox. Extend those owners. Do not create competing descriptor graphs, validation
engines, patch types, query builders, dispatch queues or HTTP test clients.

New snippets are proposed contracts unless explicitly identified as existing.
Public names/signatures must be finalized in consumer fixtures during their owning
milestone. They must support ordinary Go compilation and gopls; do not rely on
generic methods unsupported by the language, runtime Go compilation or a DSL that
requires a separate editor type system. Go's [method declarations](https://go.dev/ref/spec#Method_declarations)
define the generic receiver constraints; generic package functions and generated
concrete methods fit the existing framework patterns.

## Scope and ownership

- Framework only, with independent consumer acceptance fixtures; no starter.
- Foundry-Go source and tests are authoritative. This work needs no sibling repo.
- One contract graph supplies runtime codecs, metadata, OpenAPI and TypeScript.
  Persistence models never become public DTOs implicitly.
- PostgreSQL only. Existing named/default services and application ownership remain
  the assembly contract; new persistence must select a concrete configured pool.
- Constructor injection supplies dependencies. Request hooks and bound models keep
  concrete argument types; no `ctx.global` service lookup or actor casts.
- Additive opt-in APIs preserve current endpoint, status, error and wire behavior.
  Explicitly version incompatible schema/manifest changes before publication.
- Authentication, authorization, relation scoping and idempotency serve distinct
  purposes. Replays never bypass current authentication or disclose another caller's
  result. Side effects in a PostgreSQL transaction use the same actual transaction.
- Existing process-local after-commit callbacks stay available with their documented
  limitations. Durable effects use the existing outbox, with at-least-once delivery.

## Implementation cadence

Finish each milestone's implementation, regression sources, consumer examples and
documentation before compiling/testing. Then run its consolidated verification
round, collect failures, batch fixes and repeat affected checks until the final
source passes its required gate. No per-edit compilation, full-suite repetition or
repeated gopls startup. Reuse the existing compiler/generation/editor batching.

Every implementation milestone requires `make verify`, relevant native integration
and races, deterministic generation, public consumer acceptance and the applicable
compiler/TypeScript/gopls checks. Review consumer ergonomics before proceeding.
During each logical milestone pause, simplify duplicated logic and ownership by
source review. T07 additionally requires verification, one complete re-audit of all
implementation in this series, fixes/improvements and final-source verification.

This blueprint delivery itself uses documentation/link/numbering checks and source
review only. Implementation gates must not be reported as run merely because the
plan names them. Deferred providers or tests need explicit status and cannot count
as acceptance evidence.
