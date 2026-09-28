# C05 — Complete consumer acceptance and re-audit

Prerequisites: C01–C04. Completion requires the actual final state, not merely
new APIs or a green subset of tests.

## Required deliverables and proof

| Requirement | Authoritative evidence |
| --- | --- |
| Detailed implemented blueprint | Master statuses, contracts and source links agree |
| Typed configuration without duplicate schemas | Handwritten Go declarations plus deterministic generated schemas/keys; file/env/override tests |
| Minimal consumer startup | Independent executable bootstrap fixture with no ordinary adapter resolver closures/native SDK imports |
| Named/default service families | Multi-instance identity, routing, wrong-name and app-isolation tests |
| PostgreSQL-only database | Adapter inventory and consumer configuration |
| File/PostgreSQL/Redis/memory cache | Shared backend contracts and native service/process tests |
| Local/S3/R2 storage | Config-switch consumer proof and relevant existing/new provider verification |
| Model-specific authentication | Multiple actor handlers, negative compiler cases and HTTP auth/CSRF tests |
| Full supporting service integration | C04 matrix tied to concrete fixtures/modules and selected kernels |
| IDE completion | Real gopls probes for nested settings, named selectors, actor fields and handlers |
| Resource ownership | Startup/rollback/shutdown/race tests, shared borrowers and independent apps |
| Documentation/onboarding | Runnable public examples, generated output current, local links valid |
| Build/runtime performance | Controlled native cold/incremental build, editor and bootstrap/request measurements |
| Final audit and improvements | Findings reviewed across all changed implementation; regression evidence for fixes |

Reuse `make verify`, compiler batching, generation checks, native PostgreSQL/Redis,
relevant races, real gopls and TypeScript gates. Do not launch duplicate database
servers or destructive resets. Keep credentials out of source and logs. Package
an independent consumer where assembly/module boundaries changed.

Measure default versus named selection, configured startup/shutdown and ordinary
HTTP behavior. Compare with existing native resource evidence rather than claiming
performance from Go alone. Explain material additional import/build costs.

After full verification, audit the complete changed implementation once more for
SSOT, naming/structure, unnecessary abstractions, duplicated ownership, hidden
runtime typing, error handling, secret exposure and consumer boilerplate. Batch
fixes, verify affected behavior and complete the final gate against the resulting
source. Mark the goal achieved only when every row above has direct evidence.


## Concrete C05 implementation and execution order

1. Reuse the independent consumer module. Add
   [`configuredprofile`](../../tests/fixtures/consumer/configuredprofile), containing
   a short executable, existing generated application settings, two named caches,
   a real HTTP route and the existing `productionprofile` typed model. The profile
   opens no database by default and imports no native provider SDK.
2. Exercise the unchanged typed cache declaration with memory/file/PostgreSQL/Redis
   settings. Use existing native services and isolated test namespaces. Exercise
   local storage I/O and configured S3/R2 credential/region/namespace selection
   through real SDK signing with inert credentials. Retain the explicitly scoped
   historical live-provider certification and current protocol tests; do not claim
   a new cloud account run from signing-only proof.
3. Correct the constructor-lifetime audit finding with `audit.Scope`: resolve
   `services.AuditScope()` in a constructor, retain the concrete handle, and call
   `scope.Within` from a handler. Reuse `internal/sqlscope`; preserve exact-pool
   checks, savepoint rollback and restoration. Prove a constructed HTTP handler
   works after its factory resolver seals and audit writes share business rollback.
4. Extend existing private packaging to manifest format 2 and three consumers:
   ordinary, configured and full. Minimal profiles copy the same typed model;
   configured adds only its own source. Preserve format-1 measurement compatibility,
   deterministic archives, ownership manifests and no-local-replace module graphs.
5. Finish all source, regression/benchmark sources and documentation before any
   compilation. Generate and run the consolidated acceptance/fix loop, including
   native integration/races, tooling, compiler/gopls and full `make verify`.
6. Re-audit every changed implementation from C01 through C05 after verification.
   Review configuration/generation, named registries, credentials/adapters, cache
   persistence, lifecycle, HTTP/auth, supporting features, consumer constructors,
   package boundaries, tooling and documentation. Record concrete findings and
   fix them in a batch. Repeat affected checks and the final full gate.
7. Package the verified final source into a fresh private candidate. Run controlled
   native cold/unchanged/model-edited generation and builds plus three fresh-process
   gopls completion/hover rounds for each profile. Record selected modules, source
   hashes, import counts, linked ordinary/configured sizes and native memory data.
   Run both linked commands and the configured consumer's no-service tests.
8. Run three benchmark samples with ordinary compiler defaults: Build/Start/Shutdown,
   retained/default/named selection, equivalent direct/configured HTTP handlers and
   real loopback HTTP round trips. Distinguish handler harness allocations from
   network/client costs. No acceptance/compile workloads run concurrently with
   measurements. Record known timing/control limitations and compare the prior
   native foundation/C03 evidence without treating different capabilities as equal.
9. Finish the acceptance matrix in the
   [consumer acceptance guide](../../docs/guides/consumer-startup-acceptance.md),
   record final source hashes/results, validate docs, and only then close the goal.

No publishing, version-control mutation, destructive database work or starter
product is part of this milestone. The master owns current verification status.
