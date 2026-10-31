# Client contracts and TypeScript

Milestone 21 passed native framework/consumer verification, strict TypeScript,
real HTTP/WebSocket interoperability, compiler/editor, generation/recovery, race
and bounded fuzz checks.

`contract/manifest` collects the application's **registered** HTTP endpoints,
WebSocket channels, notification outputs, datatables, locale definitions, enums
and permissions. DTO descriptors own public fields. Database models and private
notification inputs do not automatically become public schemas. OpenAPI 3.1.1
and TypeScript are adapters over this one manifest.

## Export from the application

Assemble the same registries the application uses, then call
`manifest.Build(ctx, manifest.Sources{HTTP: router, Realtime: &realtime, ...})`.
Obtain `realtime` with `websocket.DescribeClient(channels, websocketConfig)` so
the protocol, replay and resource limits describe the running configuration.
The other optional sources are `Notifications`, `Tables`, `Catalog`, `Enums`,
`Permissions` and explicitly public `Schemas`. Metadata discovery does not run
endpoint handlers, notification renderers, permission checks or table queries.

`Build` validates schema identities, references, wire shapes, access descriptors,
operation names and feature metadata. Conflicting definitions or ambiguous
camel-cased names are errors. `Snapshot()` and `JSON()` return owned copies.
`manifest.Decode(data)` validates a saved manifest without loading application
code. The decoder rejects unknown versions, duplicate JSON keys, unknown fields,
broken references and oversized/deep documents. Manifest version 6 adds bounded
property/parameter presentation metadata. Regenerate saved manifests and clients
together. [Typed descriptors](client-descriptors.md) expose operation/field lookup
and explicit semantic hints through the same contract.

Generate into an existing, dedicated client directory:

```go
source, err := manifest.Build(ctx, sources)
if err != nil {
    return err
}
_, err = typescript.Generate(ctx, source, typescript.Options{
    Dir: "frontend/src/generated",
    OpenAPI: openapi.Options{Title: "Shop API", APIVersion: "1"},
})
return err
```

Imports are `github.com/weiloon1234/Foundry-Go/contract/manifest`, `/typescript`
and `/openapi`. `typescript.Render(source)` and `openapi.Render(source, options)`
also return bytes without publishing. An application export command can save
`source.JSON()` and use the framework CLI:

```sh
foundry contracts --manifest public-contract.json --dir frontend/src/generated \
  --title 'Shop API' --api-version 1
foundry contracts --manifest public-contract.json --dir frontend/src/generated \
  --title 'Shop API' --api-version 1 --check
```

Instead of hand-writing that export command, mount the built-in one. It resolves
the sources from the application when it runs:

```go
export, err := typescript.ExportCommand("contracts.export",
    openapi.Options{Title: "Shop API", APIVersion: "1",
        Servers: []openapi.Server{{URL: "https://api.example.com"}}},
    func(ctx context.Context, services foundation.Resolver) (manifest.Sources, error) {
        router, err := foundation.Resolve(services, routerKey)
        return manifest.Sources{HTTP: router}, err
    })
```

`routerKey` is the application's `foundation.Key[*http.Router]`; resolve the
realtime description and other sources the same way. Register the declaration
with the application's CLI commands, then run
`app contracts.export --dir frontend/src/generated [--prefix contracts] [--check]`.

The default output is the client entry `contracts_foundry.gen.ts`, the shared
runtime modules it imports (`contracts_runtime_foundry.gen.ts` and
`contracts_runtime_realtime_foundry.gen.ts`), `contracts_manifest_foundry.gen.json`
and `contracts_openapi_foundry.gen.json`. `--prefix` changes their shared prefix.
Import an entry module; the runtime modules' exports are not a stable API. There
are no timestamps, absolute source paths or runtime npm imports in the generated
modules. They require ES2022 and DOM types. Enable `strict`,
`exactOptionalPropertyTypes` and `noUncheckedIndexedAccess` in the consuming
project; the modules also compile with `isolatedModules` and
`verbatimModuleSyntax`. `typescript.Render` still returns one self-contained
module with the runtime inlined.

Client JSON limits count containers, values and object names, matching Go. For
example, `{"key":"value"}` uses three nodes; a two-node budget rejects it on
both encode and decode. Byte, depth and schema-work limits remain independent.

## Portal surfaces

An application with several portals declares one surface per portal, so each
bundle carries only its own operations, channels and schemas:

```go
export, err := typescript.ExportCommand("contracts.export", api, sources,
    typescript.Surface{Name: "admin", Paths: []string{"/api/admin"}, Guards: []auth.GuardName{"admin.api"}},
    typescript.Surface{Name: "web", Routes: []http.RouteID{"web", "health.live"}})
```

`typescript.Options.Surfaces` takes the same declarations. Each surface publishes
`contracts_<name>_foundry.gen.ts` beside the full entry, with the same API
restricted to its selection, which combines four kinds of entry:

- `Routes` and `Channels` select the ID itself and every ID continuing it after
  a dot: `admin` selects `admin.login` and `admin.orders.list`, not
  `administration.list`.
- `Paths` selects routes below a literal path prefix by whole segments:
  `/api/admin` selects `/api/admin/orders/{id}`, not `/api/administration`. A
  prefix has no parameters or trailing slash, and `/` selects every route. This
  suits applications whose route IDs are named by feature rather than portal.
- `Guards` selects every channel declared for a guard, such as
  `websocket.Private(id, rooms, adminGuard, authorize)`, so a portal picks up its
  guard's new channels without listing them. The guard must guard some route or
  channel in the manifest, but need not have a channel yet. Public channels have
  no guard; select them by ID. Operations are selected by path or ID, since a
  portal's login and refresh routes are public.

The embedded manifest is a projection (`manifest.Manifest.Project`) containing:

- the selected operations and channels and the schemas they reach;
- the tables whose row or request schema those operations reach;
- notifications' inbox deliveries, and realtime deliveries on selected channels;
- the application-wide error definitions, locales, enums, permissions and
  explicit schemas.

`contractMetadata()` returns that projection. Schema names are the full
manifest's in every entry, and identity brands and runtime classes come from the
shared modules, so values pass between entries of one directory. A surface
without channels neither imports the realtime module nor exports
`createRealtime`. Surface names follow the prefix pattern and are unique ignoring
case. `manifest`, `openapi`, `react`, `vue` and names beginning with `runtime`
are reserved, and at most 64 surfaces are allowed. Every `Routes`, `Channels`
and `Paths` entry must select something, every guard must be known, and a
surface must contain an operation or a channel. Removing a surface removes its
file.

The runtime modules have no top-level side effects, so bundlers drop the runtime
features an application does not import. Measured with esbuild 0.28.2 (minified
ES2022 modules, then gzip -9) on the client fixture's 23 operations and two
channels, 2026-10-02:

| Application imports | gzip |
| --- | --- |
| Full entry: `createClient` | 19.2 KB |
| Full entry: client, realtime, forms and descriptors | 26.7 KB |
| Seven-operation surface: `createClient` | 14.9 KB |
| Seven-operation surface: client and forms | 19.7 KB |
| One operation and two channels: client and realtime | 17.7 KB |

The remaining cost is the core runtime: lossless codecs, request validation and
the HTTP invoker.

To publish the generated directory as a workspace package, map the entries
through `exports` and block the runtime modules, which entries import by
relative path:

```json
{
  "exports": {
    ".": "./dist/generated/contracts_foundry.gen.js",
    "./portal/runtime": null,
    "./portal/runtime*": null,
    "./portal/*": "./dist/generated/contracts_*_foundry.gen.js"
  },
  "sideEffects": false
}
```

Node's `*` matches at least one character, so `./portal/runtime*` does not block
`./portal/runtime`; keep both keys. Keep the pattern under a subpath such as
`./portal/`, since a bare `./*` would also expose every other file of the
package. `sideEffects: false` lets a bundler resolve a shared name such as
`APIError` imported from the full entry straight to the runtime module, without
the full entry's operation table.

## Operation documentation, examples and servers

Document an endpoint with `WithDocumentation(http.RouteDocumentation{Summary,
Description, Tags, Deprecated})` on its route or endpoint. Documentation is
bounded (`MaxRouteSummaryBytes`, `MaxRouteDescriptionBytes`, `MaxRouteTags`),
never changes routing or access, and is exported with the manifest. OpenAPI
receives `summary`, `description`, `tags` (plus a sorted top-level tag list) and
`deprecated`; the TypeScript `API` interface receives the same text as TSDoc with
`@deprecated`. The route ID remains the OpenAPI `operationId`.

`WithBodyExample(value)` and `WithResponseExample(value)` take values of the
endpoint's own request and response types and encode them immediately through
the same JSON contracts, so an example can never disagree with the declared
schema; an example that fails its contract makes the endpoint invalid. OpenAPI
publishes them as the media type `example`. `openapi.Options.Servers` adds
server entries: absolute `http`/`https` URLs without credentials, query or
fragment, or paths such as `/api`.

## Ownership, checking and recovery

The shared generator publisher owns every generated file through a version 2
`.foundry-gen.json`. Keep it with the generated outputs. This client directory
must be separate from directories owned by Go generation. A prefix rename
deletes obsolete, unchanged owned files; unrelated files remain untouched.
Edited, orphaned, symbolic-link or conflicting outputs are rejected. The input
manifest is limited to 16 MiB; each artifact is limited to 48 MiB, enough for a
client module that embeds the largest accepted manifest.

`--check` is read-only, including on first use and after interruption. Ordinary
publication recovers an interrupted batch through the existing confined,
hash-checked journal. Recovery alone uses:

```sh
foundry generate --recover --dir frontend/src/generated
```

The client journal cannot authorize writes to application Go sources. Individual
replacements are atomic; the entire set is recoverable rather than one atomic
filesystem transaction. See [generator ownership](model-generation.md#commands-and-ownership)
for process guards, retained backups and edit-conflict handling.

## Values and wire precision

Generated `Operations` and `ContractTypes` expose the public types. Operation
methods use registered IDs converted to stable camel-cased names, for example
`items.echo` becomes `api.itemsEcho(request)`. Schema symbols use the Go type's
own name, including generic arguments (`Page_Invoice` for `Page[Invoice]`,
`List_Invoice` for `[]Invoice`), without its package path, so moving a package
keeps its names. When two types share a name, both are qualified with trailing
package path elements (`Billing_Invoice`, `Archive_Invoice`); names that would
shadow a declaration of the generated TypeScript module, such as `Map` or
`Payload`, are qualified the same way, and only a remaining collision receives a
stable digest suffix. Adding a same-named type can therefore rename an existing
symbol; clients that must be immune to that select types by identity, for
example `ContractTypes["example.com/app/billing.Invoice"]`.

| Go contract | TypeScript value | JSON wire |
| --- | --- | --- |
| Narrow integer, float32/64 | `number` with declared bounds | JSON number |
| 64-bit integer, native 64-bit int/uint | Decimal `string` | Exact JSON integer token |
| `json.Number` | Numeric `string`, exponent retained | Exact JSON number token |
| Exact decimal | `string` | JSON string |
| UUID/model ID | Branded `string` by full source identity | JSON string |
| Natural string key | `string` | JSON string |
| Date/time/interval | `string`, original precision retained | JSON string |
| `[]byte` | Canonical base64 `string` | JSON string |
| Optional field | Property may be omitted | Omitted property |
| Nullable field | Value may be `null` | JSON null |

Required nullable fields must still be present. An optional field's explicit
`undefined` is rejected. `json:",string"` retains its quoted wire behavior while
the client uses the underlying semantic value. Model brands prevent mixing
identities at compile time; use `contractValue(typeID, value)` to validate and
brand an external value. Branding is not authorization.

The SDK's bounded parser and encoder preserve existing Go numeric wire formats
without passing complete payloads through JavaScript's `JSON.parse` or
`JSON.stringify`. Do not preparse transport responses: a rounded integer cannot
be recovered. `encodeContract` and `decodeContract` expose the same codecs for
standalone DTOs. Explicit dynamic JSON uses `import("./generated/contracts_foundry.gen.js").JSONNumber` for exact numeric tokens
and `JSONValue` for its tree. String-keyed maps use owned objects without inherited
properties; typed map keys retain their canonical domain syntax. An explicit map
schema without a key descriptor accepts arbitrary string names while retaining its
declared value type, nullability and wire limits in both Go and TypeScript.

The generated `Locale` type is the union of the exported locale catalog's
supported locales (`"en" | "ms"`), or `string` when no catalog is exported. A
request map keyed by `i18n.LocaleID`, such as translated slot input, is a
`LocaleMap<V>`: TypeScript rejects a locale outside the catalog, and descriptors
bind its entries with `.at(locale)` and `.element()`. The server still validates
locales against its current catalog. Received values keep `string` keys, because
a newer server may support more locales than the client was generated with; a
schema reachable from server output that contains such a map therefore has a
`...Received` variant. OpenAPI keeps its open object keys.

## Tolerant server output

Deployed clients, especially mobile applications, often outlive the server
version they were generated from. Responses, error envelopes, server events and
presence payloads therefore decode tolerantly by default:

- unknown object properties are ignored and omitted from the decoded value;
- an unknown enum value decodes as an `UnknownEnumValue` carrying the schema ID
  and the raw wire value, instead of failing the whole payload;
- a map entry keyed by an unknown enum case is omitted.

Everything else remains strict: required properties, types, formats, numeric
bounds and resource limits still fail with `ContractError`. Requests and
published events are always encoded strictly, so an `UnknownEnumValue` cannot be
sent back. Each schema reachable from server output that can contain enum values
has a generated `...Received` type, used by `Operations[...]["response"]`,
`ErrorResponse` and event handlers. Narrow it before reuse:

```typescript
const item = await api.itemsEcho(request);
if (item.state instanceof UnknownEnumValue) showUnsupported(item.state.value);
else render(item.state);
```

Set `strictResponses: true` in `ClientOptions` (or `strictEvents: true` in
`RealtimeOptions`) to reject unknown properties and enum values instead.
`decodeReceived(type, input)` applies tolerant decoding to a standalone value;
`decodeContract` stays strict. The embedded manifest is parsed once, on first
use rather than at import.

## Enums, schemas and error codes

### Enum declaration merging and label helpers

Each declared enum emits a TypeScript union type and a companion constant value sharing the same name:

```typescript
export type AdminType = "superadmin" | "developer" | "admin";
export const AdminType = /* @__PURE__ */ Object.freeze({
  cases: ["superadmin", "developer", "admin"] as const,
  labels: /* @__PURE__ */ Object.freeze({
    superadmin: "admins.type.superadmin",
    developer: "admins.type.developer",
    admin: "admins.type.admin",
  }) satisfies Record<AdminType, CatalogMessageKey>,
});
```

When an enum defines labels for a subset of cases, `labels` satisfies `Partial<Record<AdminType, CatalogMessageKey>>`. If no cases declare a label, the `labels` property is omitted. Wide integers (64-bit) preserve string representations in both `cases` and `labels`.

The shared runtime provides a generic helper `enumLabel` to resolve an enum's display label:

```typescript
import { AdminType, enumLabel, formatMessage } from "./generated/contracts_foundry.gen.js";

const label = enumLabel(AdminType.labels, user.type, key => formatMessage(messages, key));
```

`enumLabel` returns the raw value when given an `UnknownEnumValue` or an unmapped case, and never accepts arbitrary strings.

### Named schema constants and brand identities

Each entry module exports `schemas`, a frozen map of declared schema names to their Go type-path strings, filtered to types reachable by that surface:

```typescript
import { decodeContract, newIdentity, schemas } from "./generated/contracts_foundry.gen.js";

const payload = decodeContract(schemas.Payload, networkData);
const orderId = newIdentity(schemas.OrderID); // generates a fresh UUID v4
const existingId = newIdentity(schemas.OrderID, "0190a8f0-0000-7000-8000-000000000001");
```

`newIdentity<Owner>(owner, id?)` brands a UUID string as `Identity<Owner>`. When `id` is omitted, it generates a fresh UUID v4 via `crypto.randomUUID()`. It validates UUID v4 syntax and rejects nil UUIDs.

### Application error codes

Each client exports the application-wide `ErrorCode` constant and type:

```typescript
import { ErrorCode } from "./generated/contracts_foundry.gen.js";

if (error.code === ErrorCode.ValidationFailed) {
  // handle validation failure
}
```

Error codes defined in the manifest are transformed to PascalCase keys (e.g. `validation_failed` → `ValidationFailed`). If two error codes in the manifest collide on the same PascalCase key, code generation fails with a conflict error.

### Typed datatable descriptors

Generated entry modules export a frozen `tables` registry for surfaces that define datatables, together with typed identifiers and row helper types:

```typescript
import { tables, type TableId, type TableRow } from "./generated/contracts_foundry.gen.js";

const membersTable = tables["reports.members"];
const memberRow: TableRow<"reports.members"> = member;

// Invoke typed query or export methods directly through the descriptor:
const page = await membersTable.query(api, {
  body: {
    filters: [{ op: "eq", column: "state", values: ["active"] }],
  },
});
const csv = await membersTable.export(api, {});
```

Each descriptor in `tables` carries:
- `id`: unique table identifier string.
- `columns`: readonly array of `TableColumn<Row>` with `name` typed strictly as `keyof Row & string`, `kind` derived from Go `datatable.ColumnKind`, `nullable`, `sortable`, `searchable`, `exportable`, and declared filter `operators`. Enum columns carry `kind: "enum"` and reference the S1 enum value object through `enum`. Read allowed values from `column.enum.cases` and optional label keys from `column.enum.labels`. Integer enums use numbers up to 32 bits and decimal strings for wider integers. Without registered enum metadata, the value object carries schema-owned cases without labels.
- `defaultSort`: readonly array of `TableSort<keyof Row & string>`.
- `exports`: boolean flag indicating whether the table supports export operations.
- `query(client, input, options)` and `export?(client, input, options)`: strongly-typed invokers that call client operations without casts or `any`.

Surfaces without tables omit the `tables` constant. The framework runtime remains strictly decoupled from UI and state management libraries (such as Zustand, table sources, or display formatters).

### Deterministic readable names and v0.3.0 migration

Beginning in v0.3.0, TypeScript contract generation replaces nondeterministic hashed names (`Go_<hash>`, `String_<hash>`, `ID_<hash>`) with human-readable, deterministic identifiers:

- **Entity IDs**: `model.ID[T]` produces `<Entity>ID` (e.g., `OrderID`, `UserID`). When cross-package type name collisions occur, names qualify with package elements (e.g., `Reporting_MemberID`, `Consumer_MemberID`, `Models_UserID`).
- **Anonymous Structs**: Anonymous nested structs produce `<Owner><Field>` or `<Owner>Item` for repeated elements (e.g., `ReportItem` for `report.items []struct{...}`).
- **Collections**: Generic slice and map kinds produce deterministic identifiers:
  - `[]T` → `List_<T>` (e.g., `List_Filter`, `List_String`)
  - `[N]T` → `Array_<N>_<T>` (e.g., `Array_2_String`)
  - `map[K]V` → `Map_<K>_<V>` (e.g., `Map_Int64_String`, `Map_Status_String`)
  - `i18n.LocaleMap[V]` → `LocaleMap_<V>` (e.g., `LocaleMap_String`)
  - `value.Nullable[T]` → `Nullable_<T>`
- **Reserved Primitives**: When Go primitive wrappers clash with reserved global types, deterministic names apply (e.g., `GoString` for Go `string`, `GoError` for Go `error`).
- **Hashing as Last Resort**: Type hashing is reserved strictly as an insurmountable collision fallback and is accompanied by a code reason.

#### Migrating from v0.2 to v0.3

Applications upgrading from v0.2 to v0.3 should update TypeScript imports and type annotations referencing generated types:
1. Replace hashed `Go_<hash>` and `String_<hash>` references with their corresponding `List_<T>`, `Map_<K>_<V>`, `Array_<N>_<T>`, or `GoString` names.
2. Replace `ID_<Entity>` or hashed `User_<hash>` references with `<Entity>ID` or `<Package>_<Entity>ID`.
3. Replace handwritten datatable metadata parsers with the generated `tables` registry.
4. Regenerate all client contracts using `foundry contracts` or the application's exported command.

## Injected HTTP transport

`createClient(transport, options)` owns endpoint paths, query defaults/cardinality,
JSON, URL-encoded form and multipart encoding, declared status checks, payload limits and errors.
The transport owns network I/O, credentials and cancellation. Supply raw response
bytes or text and a `close()` method. For example, a browser adapter can stream
without predecoding JSON:

```typescript
import { createClient, type HTTPTransport } from "./generated/contracts_foundry.gen.js";

const transport: HTTPTransport = async request => {
  const response = await fetch(request.url, {
    method: request.method, headers: request.headers,
    ...(request.body === undefined ? {} : { body: request.body }),
    ...(request.signal ? { signal: request.signal } : {}),
    ...(request.duplex ? { duplex: request.duplex } : {}),
    credentials: request.credentials, redirect: request.redirect,
  } as RequestInit);
  let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
  async function* chunks() {
    reader = response.body?.getReader();
    if (!reader) return;
    try {
      for (;;) {
        const next = await reader.read();
        if (next.done) return;
        yield next.value;
      }
    } finally {
      try { await reader.cancel(); } finally { reader.releaseLock(); reader = undefined; }
    }
  }
  return {
    status: response.status,
    headers: Object.fromEntries(response.headers),
    body: chunks(),
    close: async () => {
      if (reader) await reader.cancel();
      else if (response.body && !response.body.locked) await response.body.cancel();
    },
  };
};
const api = createClient(transport, { baseURL: "https://api.example.com" });
```

Call options accept an `AbortSignal` and headers. Shared headers and credential
mode belong in client options. Configure bearer headers or cookie credentials
according to the exported access metadata; the SDK never stores login secrets.
Use the server's CSRF policy for cookie-authenticated writes. Optional auth and
required permissions remain explicit metadata, not client-side access grants.

Unknown/custom URL syntax requires an explicit `urlCodecs` adapter. Built-in
path/query codecs validate width, enum cases, room identity, cardinality and
escaping. Signed operations require `signedURL` obtained from the server; the
SDK checks the declared origin/path/domain query and preserves the signed URL's
bytes. Following the exported link policy it accepts absolute links on the
client's origin, origin-relative links (sent to `baseURL`), permanent links when
the route permits them, and declared ignored parameters such as `utm_source`.
It never creates signatures or exports signing keys.

`APIError` carries the declared status, code and decoded framework error envelope.
`ContractError` reports invalid inputs, unexpected statuses and malformed network
payloads. A failure found after the server answered (an undeclared status, a
malformed error envelope, wrong media type or a body, file or event stream that
breaks its contract) is its subclass `ResponseContractError`, carrying the received
`status` (0 when the transport reported no usable status). The request may already
have taken effect, so reconcile before retrying; a plain `ContractError` from the
SDK's own input checks means nothing was sent. An intermediary
returning HTML or a mismatched error code/status cannot silently become a typed DTO. Transport failures remain transport failures. No
automatic mutation retries occur. A cache adapter must resolve a JSON `304` to
its stored representation and successful status before returning it to the SDK.

Forward every request field. `redirect` is `"manual"` for redirect operations and
`"follow"` otherwise; `duplex` is `"half"` when a raw body is a stream, which
`fetch` requires for a `ReadableStream` request body.

An operation declaring several success statuses (`JSONResponses`) returns
`StatusResult<S, T>`: `{ status, body }` with `status` typed as the union of its
declared statuses, for example `200 | 201`. Other statuses are rejected.
A redirect operation (`RedirectResponse`) returns `RedirectResult<S>`:
`{ status, location }`, where `location` is the relative target on the server's
origin; the SDK never follows it and rejects absolute or scheme-relative targets.
Browser `fetch` hides a manual redirect (an `opaqueredirect` with status 0); the
SDK reports that as a `ContractError` with code `opaque_redirect`, so call
redirect operations from server-side or native clients, or navigate the browser
to the endpoint directly.

An event stream operation (`EventStreamResponse`) returns `EventStreamResult<T>`,
an async iterable of frozen `{ id?, name, data, retry? }` events. Consume it once
with `for await`; the response closes when iteration ends, or call `close()`.
Event data decodes tolerantly within `limits.Response`. Framing follows
`EventSource` (comments ignored, CR/LF/CRLF lines, an unterminated final event
discarded). The SDK does not reconnect; pass the last `id` as a `last-event-id`
call header to resume.

A raw request body (`RawRequestBody`) takes `{ data, mediaType }`. `data` is a
`Blob`, `ArrayBuffer`, typed array or `ReadableStream<Uint8Array>`; `mediaType` is
one of the declared media types and may be omitted when only one is declared.
The SDK bounds the body by `EndpointLimits.Raw.Bytes`, including a stream as it is
sent. Client-side validation is skipped (`skipped: ["raw_body"]`) because the
body is opaque; the server still applies its rules.

Multipart requests use `Upload` values containing a `Blob` and filename; the SDK
encodes text, repeated values, JSON fields and files from their descriptors.
JSON fields have no filename. MIME framing counts toward the request limit.
Seekable downloads admit native `206` ranges and `304` results. A `FileResult`
owns a bounded byte stream; consume it once and call `close()` in `finally`, or
close it without consuming it. Download transfer limits include multipart range
framing and remain independent of JSON limits.

Pagination parameters, defaults, validation, response metadata and navigation
links come from the registered [HTTP pagination](http-pagination.md) endpoint.
The client does not infer public page items from persistence fields or invent
another page model.

## Realtime lifecycle

`createRealtime(transport, options)` accepts an already-open connection whose
negotiated protocol equals exported `realtimeSubprotocol`. An adapter supplies
`send(text)`, `listen(receive, closed)` returning a detach function, and `close()`.
Forward raw string/byte messages unchanged. Browser cookies are established by
ordinary HTTP authentication; the WebSocket adapter must honor the server's
origin and authentication policy.

A browser that authenticates with bearer tokens cannot set `Authorization` on a
WebSocket. Fetch a single-use ticket from the application's guarded ticket
endpoint (a generated operation like any other) for every connect and
reconnect, and offer it with the generated `realtimeProtocols(ticket)`:

```typescript
const { ticket } = await api.realtimeTicket();
const native = new WebSocket(realtimeURL, realtimeProtocols(ticket));
```

`realtimeProtocols()` without a ticket returns only `realtimeSubprotocol`; an
empty or non-token ticket throws `RealtimeError("ticket")`. The ticket travels in
`Sec-WebSocket-Protocol`, never the URL, and the server negotiates only
`realtimeSubprotocol`. The endpoint name above is the application's own.

Operations that read, set or clear a browser
[refresh cookie](tokens.md#browser-refresh-cookies) carry `refresh_cookie` in
the manifest; a cookie logout's is `optional`, since it also succeeds without the
cookie. Keep the client's default `credentials: "same-origin"` (or
`"include"` for a deliberately configured cross-origin deployment) so the
browser sends and stores that cookie; `"omit"` breaks the refresh flow.

Generated room handles expose only declared event directions:

```typescript
const room = realtime.channels.updates("7");
const off = room.on.updated((payload, info) => {
  render(payload, info.replayed);
});
await room.subscribe({ replay: 4 });
await room.publish.relay(payload, { onAccepted: showAccepted });
// At the end of this room's lifetime:
await room.unsubscribe();
off();
room.dispose();
```

The example names come from the independent consumer fixture; your registrations
determine your methods. Owned private rooms require an explicit subject key.
Presence exposes only the registered member DTO, a public member identifier and
connection count. Event/presence payloads are frozen before callback delivery.
Synchronous and asynchronous callback failures are reported through `onError`
without corrupting protocol state; failures in the error observer are contained.
Listeners added during delivery begin with subsequent frames, and closing the
client stops the remaining callbacks for that frame.
Subscribe/publish completion is correlated to the actual acknowledgement;
an intermediate accepted response does not mean delivery is complete.

Replay, frame, presence, subscription and deduplication limits come from the
server configuration. Replay is bounded history, not durable delivery. The SDK
does not reconnect or retry automatically. An aborted or timed-out in-flight
operation closes the connection so it cannot leave an untracked server
subscription. `close()` rejects pending operations and releases timers,
listeners and handles. Dispose unused room handles to release their capacity.
For a new connection, create a new client and deliberately request replay.

## Portal sign-in sessions

Declare `Surface.Auth` alongside the portal's route selection. The referenced
login, refresh, logout, and profile routes must all belong to the surface:

```go
typescript.Surface{
	Name: "browser",
	Routes: []http.RouteID{"browser"},
	Auth: &typescript.SurfaceAuth{
		Guard: "browser_accounts",
		Login: "browser.login",
		Refresh: "browser.refresh",
		Logout: "browser.logout",
		Profile: "browser.profile",
		Identifier: "login_id",
	},
}
```

`Identifier` is the case-sensitive JSON property name in the login body. It must
exactly match a non-secret manifest property; generation performs no case folding
or Go-field-name conversion. The emitted `signInIdentifier` and
`authDescriptor.identifier` use that same name, typed as `keyof PasswordCredentials`.
The login body must declare exactly one password field with `client:"kind=password"`.

The profile route must use the declared guard. Login uses `TokenCookieResponse`;
refresh pairs `RefreshTokenCookie` with `TokenCookieResponse`; logout pairs
`RefreshTokenCookieLogout` with `ClearRefreshCookie`. All three share one
`http.DefineRefreshCookie` declaration with a `__Host-` name. See
[browser refresh cookies](tokens.md#browser-refresh-cookies) for the server's cookie protections.

The surface entry exports `PasswordCredentials`, `signInIdentifier`,
`authDescriptor`, and `createPortalSession`. The descriptor binds the surface's
typed client operations and includes no cookie name, UI messages, or catalog keys.
Its session helper imports the separate
`<prefix>_runtime_auth_foundry.gen.ts` module. Generation emits that module only
when at least one surface declares `Auth`; other entries do not import it.

```typescript
import { createPortalSession } from "./generated/contracts_browser_foundry.gen.js";

const portal = createPortalSession(transport, { clientOptions: { baseURL } });
const profile = await portal.signInWithPassword({ login_id: email, password });
const restored = await portal.restoreSession();
await portal.signOut();
portal.session.dispose();
```

Use the login DTO's JSON keys for credentials. This example follows the
[client-contract consumer](../../tests/fixtures/consumer/clientcontracts/browser_auth.go),
whose `Email` field has `json:"login_id"` and whose surface declares
`Identifier: "login_id"`. Declaring `Identifier: "Email"` fails generation.
`portal.api` is the authenticated client; `portal.credentials` is the plain
client used by cookie operations.
The transport must honor generated request credentials and release response
bodies through `close()`.

Access tokens stay in memory. The browser alone stores the HttpOnly refresh
cookie. A session coalesces concurrent renewals in a tab, uses Web Locks to
serialize renewal and login/logout cookie writes across tabs of its guard, and
shares access-token transitions over BroadcastChannel. Without Web Locks,
serialization covers only the current tab. An unknown refresh outcome signs that
tab out and is never resent.

The authenticated transport renews after a 401 and retries once for replayable
bodies. Streams are never retried. Explicit Authorization headers pass through.
Credential versions prevent a request from replaying under a later login;
a superseded sign-in profile throws `SessionChangedError` from the auth runtime
without assigning an HTTP status or UI message. Logout clears local state after
the server call succeeds. Applications own error presentation and disposal.

## Public browser configuration

Mark browser-visible scalar or enum fields in a `//foundry:config` declaration
with `config:",public"` and a full environment key. Existing configuration paths
can precede `,public`:

```go
//foundry:config
type Settings struct {
	AppName       string `config:"app_name,public" client:"env=VITE_APP_NAME,fallback=ConsumerApp,max=80"`
	DefaultLocale string `config:"default_locale,public" client:"env=VITE_DEFAULT_LOCALE,fallback=en,kind=locale"`
}
```

Pass the generated `SettingsPublicConfig()` to `manifest.Sources.PublicConfig`.
This exports declaration metadata and public fallbacks, without reading runtime
settings. Secret fields cannot be public. Treat descriptions and fallbacks as
browser-visible data.

The generated entry exports `PublicEnv`, `publicEnvSchema`, `decodePublicEnv`,
and `findPublicEnvProblems`. Boolean and numeric settings decode to their
TypeScript scalar types; enums decode to their declared cases. A locale setting
uses the catalog's `Locale` union when locale metadata is available. The supported
kinds are `string`, `boolean`, `number`, `enum`, `locale`, `currency`, and
`time_zone`; `max` limits string length.

```typescript
import { decodePublicEnv, findPublicEnvProblems } from "./generated/contracts_foundry.gen.js";

const problems = findPublicEnvProblems(rawEnvironment, "VITE_");
if (problems.length > 0) throw new Error(problems.join("\n"));
const settings = decodePublicEnv(rawEnvironment);
console.log(settings.appName, settings.defaultLocale);
```

Supply the build tool's public environment object as `rawEnvironment`.
`findPublicEnvProblems` reports unknown prefixed keys and invalid values without
printing their values. `decodePublicEnv` trims strings, uses declared fallbacks
for missing, blank, or invalid values, and returns a frozen object. Run the
problem check when invalid build configuration must fail instead of using a
fallback.

## Frontend message keys

Pass a filesystem of frontend JSON catalogs to `manifest.Sources.Frontend`, with
one directory per supported locale, such as `en/ui.json` and `ms/ui.json`.
Provide the locale set through `Sources.Catalog` or `Sources.Locales`. A filesystem
root containing a `frontend` directory is also accepted. Applications can embed
`lang/frontend` and pass a sub-filesystem rooted there.

```json
{
  "ui": {
    "welcome": "Hello, {{name}}!",
    "farewell": "Goodbye"
  }
}
```

`i18n.DiscoverDefinitions` discovers keys and placeholder arguments through the
existing catalog reader. Every configured locale must have a catalog directory.
The manifest stores definitions in `Locales.FrontendMessages`; translation text
stays in application catalogs. Plural messages use the existing `$plural` form
map. Across all plural forms and locales for a key, discovery requires exactly
one distinct placeholder and types that argument as numeric. Forms can omit or
repeat that placeholder. Multiple placeholders are ambiguous, including when one
is named `count`, and cause export to fail with a diagnostic naming the key.
For plural messages with additional arguments, move the message to the shared
catalog and declare its numeric argument with `MessageDefinition.Plural`.

The generated `FrontendMessageKey` union and `FrontendMessageArguments` interface
keep frontend keys separate from Go-declared `CatalogMessageKey` values.
`formatFrontendMessage` uses the same translations, fallback catalogs, and plural
renderer as `formatMessage`:

```typescript
import { catalogTranslations, formatFrontendMessage } from "./generated/contracts_foundry.gen.js";

const messages = { locale: "en", translations: catalogTranslations([frontendEnglish]) };
const greeting = formatFrontendMessage(messages, "ui.welcome", { name: "Ada" });
const farewell = formatFrontendMessage(messages, "ui.farewell");
```

Unknown keys and missing arguments fail TypeScript compilation. Missing or
unrenderable translation text returns the key at runtime. Applications own
catalog text and error presentation.

## Validation and UI metadata

`validateRequest(operationName, request)` uses the same codec and portable rule
metadata as calls. It returns `issues`, `complete` and `skipped`. Presence,
length/count, exact numeric bounds, enum membership, comparisons and supported
format rules run locally. Database/application rules and unsupported portable
rules are reported as skipped. A report with `complete: false` is partial even
when no issues were found. The server always remains the validation authority.

`contractMetadata()` returns an owned lossless JSON tree containing locale IDs,
message argument definitions, enum case labels, permission labels, validation
trees, rendered notification payload references and datatable columns/filters.
Numeric metadata tokens use `import("./generated/contracts_foundry.gen.js").JSONNumber`. Locale metadata describes contracts;
it does not bundle translations. The runtime's `catalogTranslations`,
`formatMessage` and `formatText` format an application's own JSON catalogs; see
[browser catalogs](localization.md#browser-catalogs). Permission labels and table
capabilities support UI presentation; server authorization still decides access.

React and Vue can share the generated module and one transport adapter. Keep
network ownership in each component's lifecycle:

```typescript
// React effect body: invoke the generated operation and abort on cleanup.
useEffect(() => {
  const controller = new AbortController();
  api.itemsEcho(request, { signal: controller.signal }).then(setValue, showError);
  return () => controller.abort();
}, [api, request]);

// Vue setup: the same generated operation and controller need no Vue adapter.
const controller = new AbortController();
onMounted(() => {
  api.itemsEcho(request, { signal: controller.signal }).then(setValue, showError);
});
onUnmounted(() => controller.abort());
```

These are integration patterns, not framework-owned React/Vue dependencies.
Ignore intentional aborts in `showError`. For realtime, a component must release
its listeners and room handle; close a connection only when that component owns
it. Do not use an operation-level abort to remove one room from a shared
connection—unsubscribe normally instead.

## Versioning and acceptance

Manifest version 4 includes inbound idempotency key/replay policy, closed tagged unions, URL-encoded forms and request
preparation metadata; regenerate older manifests and clients. See
[typed forms and request lifecycle](forms-request-lifecycle.md).
The manifest version and exported realtime protocol version are independently
checked. An old generated client remains compatible when existing operation IDs,
paths, statuses, access, payloads and events retain their contracts. Adding an
unrelated operation is compatible. With the default tolerant decoding, adding
response/event properties or enum values an old client can receive is also
compatible; clients created with `strictResponses`/`strictEvents` still need
coordination. Renaming/removing a used endpoint, changing required fields,
types or statuses, adding union variants an old client can receive, or changing
request contracts requires coordinated clients or an application API version
boundary. Manifest version is the document format, not a promise that every
application change is compatible.

The independent `tests/fixtures/consumer/clientcontracts` fixture contains strict
TypeScript positive/negative contracts, codec adversarial cases and real HTTP/
WebSocket interoperability, including an older generated client's existing
operation against a newer server, surface clients and minified bundles.
Framework maintainers can install the pinned compiler and esbuild with
`npm ci --prefix tools/typescript --ignore-scripts`, select absolute
`FOUNDRY_TEST_NODE` and `FOUNDRY_TEST_TYPESCRIPT` (the compiler's `lib/tsc.js`), and
run `make typescript-check`. Set `FOUNDRY_TEST_TYPESCRIPT_REQUIRED=1` with both
paths for the full `make verify` acceptance gate. Installation is tooling only;
generated clients have no npm runtime dependency.

`make benchmark-typescript` measures the Go renderer with validated frozen
manifests: 8 DTOs/16 routes, 128 DTOs/256 routes, eight client surfaces over the
larger manifest, and a 512-object reference chain ending in an enum. It also
isolates schema naming for that chain. Manifest construction, OpenAPI and
filesystem publication are outside the renderer's timed loops. A separate
`BenchmarkGenerateCheck` includes the larger manifest's eight surfaces, OpenAPI,
manifest JSON and the publisher's read-only current-file check. Initial
publication is outside its timed loop. The default is five
one-second samples; `TYPESCRIPT_BENCH_COUNT` and `TYPESCRIPT_BENCH_TIME` override
those settings. The fixture test checks repeat output and logs complete output
hashes with `go test -v ./typescript -run '^TestRenderPerformanceFixtures$'`.
Received-type naming walks the schema graph's reverse references once for enum
values and once for locale-keyed maps. Recursive DTOs and shared references keep
their existing names and tolerant decoding rules. Embedded runtime text is
parsed once per process; each render owns its output and manifest snapshot.
Surface selection and validation remain in `manifest.Project`.

Five alternating one-second native samples on an Apple M4 Max, Go 1.27.1,
darwin/arm64 and `GOMAXPROCS=16` measured these medians:

| Workload | Before | After | Less time | Less allocated bytes |
| --- | ---: | ---: | ---: | ---: |
| Inlined client, 8 DTOs/16 routes | 0.504 ms | 0.360 ms | 28.5% | 36.5% |
| Inlined client, 128 DTOs/256 routes | 3.731 ms | 3.501 ms | 6.2% | 7.3% |
| Inlined client, 512-object chain | 13.468 ms | 2.632 ms | 80.5% | 73.2% |
| Module renderer, small manifest | 0.599 ms | 0.434 ms | 27.5% | 15.8% |
| Module renderer, large manifest | 5.170 ms | 3.638 ms | 29.6% | 10.6% |
| Module renderer, eight surfaces | 28.574 ms | 24.397 ms | 14.6% | 10.5% |
| Schema naming, 512-object chain | 10.960 ms | 0.382 ms | 96.5% | 94.8% |
| Complete generation check, eight surfaces | 53.438 ms | 49.354 ms | 7.6% | 4.1% |

All fixture output hashes matched the original renderer, including full/surface
entries and shared runtime modules. These are steady-state rendering measurements;
CLI startup, first-use initialization and fresh-file publication are not timed.
Allocated bytes describe allocation volume, not peak memory. Exact samples,
output hashes, source fingerprints and verification are in the
[performance evidence](../evidence/typescript-performance-20261011.json).

Inbound idempotency uses the [same runtime policy](idempotent-operations.md) for its
required typed client key and safe outcomes. Version 3 readers must regenerate.

For optional draft state and form subscriptions, see the
[framework-neutral controller](client-forms.md). It reuses these codecs, validation
reports and calls. Optional React/Vue modules are generated only when requested;
ordinary direct calls and the core package remain independent of UI libraries.
