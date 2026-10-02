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

## Client surfaces and shared runtime modules (starter F-G06)

Status: **implemented** on 2026-10-02; see the
[master status](00-master-architecture-and-parity.md) for its verification. The
[client guide](../docs/guides/client-contracts.md#portal-surfaces) documents use.

### Problem and baseline

Every portal imports the single generated SDK, so it carries the whole embedded
manifest and every runtime feature. The starter measured about +26 kB gzip per
portal in Vite. The client fixture's SDK, compiled to JavaScript without
minification and compressed with gzip -9, measured on 2026-10-02:

| Part | gzip |
| --- | --- |
| Whole SDK | 36.5 KB |
| Runtime code | 25.3 KB: core (wire, formats, validation, HTTP, metadata) about 17 KB, forms 5.4, realtime 4.1, descriptors 1.8 |
| Embedded manifest | 9.8 KB indented; 7.2 KB as compact JSON |
| Generated per-operation glue | about 1 KB |

Per-operation exports alone would save little, would need the wire codec rebuilt
around per-operation metadata, and would add a second helper for each operation,
which this blueprint rules out. They are not planned.

### Declared surfaces

Export options declare named surfaces in Go, never on the command line:

```go
typescript.Options{Dir: dir, Surfaces: []typescript.Surface{
    {Name: "admin", Routes: []http.RouteID{"admin"}, Channels: []websocket.ChannelID{"admin"}},
    {Name: "web", Routes: []http.RouteID{"web", "health.live"}},
}}
```

`typescript.ExportCommand` takes the same declarations, so `contracts:export`
and its `--check` cover every surface. An entry selects the route or channel with
exactly that ID and every ID that continues it after a `.`, so `admin` selects
`admin.login` and `admin.orders.list` but not `administration.x`. After the
starter adopted surfaces with feature-named route IDs, a `Paths` entry was added:
a literal route path prefix selecting whole segments, so `/api/admin` selects
`/api/admin/orders/{id}` but not `/api/administration`. A `Guards` entry then
selected the channels declared for a guard, replacing the starter's own guard
matching; the guard must occur in the manifest but need not have a channel yet,
and the surface must still contain an operation or channel. Every other entry
must select something, and an unknown entry fails generation. A surface needs at least
one route or channel. Surfaces may overlap. Names follow the prefix pattern, are
unique, and exclude the artifact names `manifest`, `openapi`, `react`, `vue` and
`runtime`, so generated file names never collide.

### Manifest projection

`contract/manifest` owns projection; the TypeScript generator never prunes the
document itself. A projection of a manifest contains:

- the selected operations and raw routes, in manifest order;
- the realtime protocol and limits with the selected channels, omitted when none
  are selected;
- the closure of types reachable from those operations (parameters, bodies,
  responses), channels (rooms, presence, event payloads), the error type and the
  explicit roots;
- tables whose row or request type is in that closure, and notifications with a
  channel whose realtime delivery names a selected channel;
- the application-wide label metadata unchanged: error definitions, locales,
  enums and permissions.

A projection is validated like any decoded manifest. A per-surface OpenAPI
document can reuse it later; this milestone does not emit one.

### Generated files

- `<prefix>_foundry.gen.ts` keeps its full API.
- `<prefix>_<surface>_foundry.gen.ts` exports the same names and types, restricted
  to the surface. Its `contractMetadata()` returns the projection.
- Two shared runtime modules are imported by every entry:
  `<prefix>_runtime_foundry.gen.ts` (the Go-owned constants and shared brands,
  wire codecs, formats, validation and the HTTP invoker) and
  `<prefix>_runtime_realtime_foundry.gen.ts`. An entry imports exactly the
  runtime names its code refers to and re-exports the public runtime API, so
  imports from the full SDK keep working. Only entries with channels load the
  realtime module; the full entry always re-exports its API.
- The metadata, descriptor and form runtime stay inlined in each entry: their
  types and caches belong to that entry's operations and embedded manifest.
  Bundlers drop them when unused, so no per-entry contract object was needed.
- Runtime modules have no top-level side effects, so bundlers drop features an
  application never imports. A build with several portals shares one copy of the
  core.
- The runtime sources declare their module wiring as explicit import and export
  lines. `typescript.Render` drops those lines to keep returning one
  self-contained module from the same sources.
- The React and Vue form adapters keep a type-only import of `FormStore` from
  the full entry, which serves every entry of the directory.
- Each entry embeds compact JSON. `manifestJSON` parses to the same document, but
  its text is no longer indented.

### Compatibility and acceptance

The change is additive for imports and wire behaviour. The generated file set
gains the runtime modules, which the ownership manifest publishes, checks and
removes like other generated client files. Removing a surface removes its file.
Older generated clients keep working against the server.

Acceptance covers:

- projection tests: namespace matching, closures, overlap, unknown and empty
  selections, reserved names and validation of the projected document;
- strict TypeScript compilation of the full SDK and each surface, including a
  negative case where one portal's client cannot call another portal's operation;
- real HTTP and WebSocket tests through a surface client;
- generation `--check`, obsolete-output removal and older-client compatibility;
- a two-portal consumer fixture;
- measured gzip sizes of the full SDK and each surface, with minified bundle sizes
  when a bundler is available, recorded as evidence with their conditions.

The client fixture covers these with two surfaces, `members` (seven operations)
and `live` (one operation and two channels), compiled with `isolatedModules` and
`verbatimModuleSyntax`. Its bundle check uses the pinned development esbuild and
asserts which runtime features each application keeps. Measured with esbuild
0.28.2 (minified ES2022 modules, then gzip -9) on 2026-10-02:

| Application imports | Minified | gzip |
| --- | --- | --- |
| Full entry: `createClient` | 107,647 B | 19,161 B |
| Full entry: client, realtime, forms and descriptors | 129,579 B | 26,739 B |
| `members`: `createClient` | 62,484 B | 14,935 B |
| `members`: client, forms and descriptors | 75,789 B | 19,651 B |
| `live`: client and realtime | 55,234 B | 17,708 B |

The single-module SDK measured 107,646 B and 19,167 B for `createClient`, so
the split layout costs nothing for an application that keeps the full entry.
