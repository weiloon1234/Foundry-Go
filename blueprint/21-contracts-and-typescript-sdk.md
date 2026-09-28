# 21 — Contracts and TypeScript SDK

## Purpose and prerequisites

Prerequisites: [03](03-generation-and-language-tooling.md) and delivered HTTP, auth, WebSocket, notification, datatable and localization contracts. Assemble their already-owned metadata into one client-facing contract system.

Rust references: `src/contract`, `src/typescript`, `src/openapi`, `src/app_enum`, `docs/guides/typescript.md`, `blueprints/20-rust-ssot-contract-generation.md`; Starter's generated shared frontend contracts.

## SSOT and public contract

Go DTOs, enums, typed endpoint/channel registrations, runtime validation rules and permissions own the manifest. OpenAPI is an output adapter, not the canonical schema. Do not maintain an OpenAPI document by hand alongside the Go declarations.

Normalize request/response schemas, path/query/body/multipart semantics, operation IDs, success/error statuses, access metadata and realtime event directions. Keep persistence-only fields out unless explicitly mapped into a response DTO.

Export the [HTTP pagination contract](08-http-validation-and-responses.md#automatic-pagination-at-the-http-boundary) from the same manifest: declared page/size/cursor parameters, defaults and bounds, DTO item types, metadata and navigation links. Generated clients must not recreate a separate pagination schema or infer public DTOs from model fields.

Planned generated usage:

```typescript
const profile = await api.updateProfile(request);
const unsubscribe = realtime.orders(orderId).onUpdated(handleUpdate);
```

Operation and event names come from registered descriptors. SDK transport is injected; endpoint-specific fetching, encoding and error normalization are framework-owned.

## Implementation slices

1. Complete normalized manifest validation and schema references across existing feature metadata.
2. OpenAPI export with named request/response/error schemas and transport-accurate parameters.
3. Pure TypeScript types and HTTP SDK, preserving nullable versus optional fields, exact-decimal wire formats, model identity brands and uploaded-file types.
4. Typed realtime subscribe/publish/event APIs using the versioned protocol and replay/delivery limits from milestones 14–15.
5. Validation, locale/enum/permission metadata and framework-neutral integration guides for React/Vue.

## Generation and compatibility

Reuse the generator's deterministic output, ownership manifest and `--check` path. No timestamps or machine paths in generated source. Generate into a configured consumer output directory and never overwrite unrelated files.

TypeScript types alone do not validate network input. Runtime adapters decode required envelopes/errors and apply declared validation where supplied. Large integers and exact decimals need a deliberate wire representation that does not lose precision in JavaScript; use string wire codecs for values that cannot safely be represented as JS numbers.

Version manifest/protocol compatibility and document breaking changes. Avoid exporting multiple competing client helpers for the same operation. Dart is deferred.

## Acceptance

Compile generated TypeScript and run contract tests against real HTTP and WebSocket fixtures. Cover route status agreement, optional/null fields, enums, UUID/natural keys, decimals, files, errors, validation paths, guarded channels and wrong-event payloads. Test clean regeneration, deletions/renames and old generated-client compatibility within the promised version range. Apply the [common gate](README.md#common-completion-gate).

## Delivered contract and acceptance

Milestone 21 is accepted. [The client guide](../docs/guides/client-contracts.md)
documents `manifest.Build`/`Decode`, immutable snapshots, OpenAPI 3.1.1 and the
generated ES2022 module. Actual typed feature registrations own the metadata;
private models and notification input/transport fields are excluded.

The client uses decimal strings for wide integers and exact JSON numbers, with
bounded lossless codecs preserving existing Go numeric tokens. Explicit quoted
JSON and exact-decimal string wire forms remain distinct. Injected HTTP and
realtime transports share registered limits, schemas, statuses and protocol
metadata. Validation reports distinguish portable checks from skipped server
rules. No runtime npm dependency is introduced.

Publication uses shared ownership, read-only checking, guarded recovery and
obsolete-output removal in a dedicated client directory. Native full acceptance,
strict TypeScript positive/negative cases, real HTTP/WebSocket tests, older-client
additive compatibility, consumer/compiler/editor checks, races and bounded fuzz
passed. Details and actual timings are in the [master evidence](00-master-architecture-and-parity.md).
