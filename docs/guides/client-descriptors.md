# Typed client descriptors and presentation

<a id="agent-entry"></a>
## Agent entry

- Inspect: [Operation fields](#recipe-field-descriptors)
- Address issues: [Nested and escaped paths](#recipe-issue-addresses)
- Present: [Go field metadata](#recipe-field-presentation)
- Bind locale maps: [Wire values](client-contracts.md#recipe-wire-values)
- Decode schemas: [Schema handles](client-contracts.md#recipe-schema-handles)
- Submit: [Form outcomes](client-forms.md#recipe-form-outcomes)

<a id="recipe-field-descriptors"></a>
## Inspect operation fields

The generated SDK exposes `operation(name)` and `schema(typeID)` alongside the
existing typed client. They inspect the same normalized manifest; neither sends
requests nor creates form state. React, Vue, Angular and plain JavaScript can
consume them without runtime package dependencies.

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/testdata/guide-recipes.ts#field-descriptors -->
```typescript
export async function fieldDescriptors(api: sdk.API, request: sdk.Operations["itemsEcho"]["request"]) {
  const operation = sdk.operation("itemsEcho");
  const amount = operation.field("body", "amount");
  const value: sdk.FieldValue<typeof amount> = request.body.amount;
  const report = operation.validate(request);
  const response = await operation.call(api, request);
  return {
    value,
    kind: amount.presentation.kind,
    required: amount.required,
    nullable: amount.nullable,
    report,
    response,
  };
}
```

The operation name, location and field must exist in the generated contract.
`FieldValue<typeof amount>` retains its exact value type, including omission and
null. Descriptor identity also retains its operation/location/field owner, so
unrelated fields cannot be substituted merely because their values are strings.
Nested objects use `.field(name)`; arrays and open maps use `.element()`, and
`.at(index)`/`.at(key)` binds one concrete entry. Maps keyed by model IDs are
collections too: their entries take an `Identity` key through `.at(...)`, never
`.field(...)`. `.at` checks keys with the codec's own rules (enum cases, integer
syntax, lowercase non-zero model IDs) and indexes against a fixed array length.
Tagged unions require `.variant(discriminator, tag)` before selecting payload
fields; the discriminator must be the key that tags every variant, so an
enum-typed property of a plain object is not accepted. A variant descriptor describes the payload without the discriminator,
which belongs to the parent union. Enum-keyed maps retain their explicit keys.
`operation(name).body()` describes a JSON request body from its root at
`/body`, so a body that is itself a union, array or map is navigated with the same
`variant`, `at`, `element` and `field` steps; `field("body", name)` remains the
shortcut for an object body's property. Form, multipart and raw bodies have named
parts or no JSON root: `body()` is a type error there and a `ContractError` at
runtime.

`schema(typeID)` describes an explicitly public schema without a route or submit
method, accepting a declared schema identity string or a typed `schemas.<Name>` constant
(such as `schema(schemas.Payload)`). `operation(name).metadata` exposes route identity, access, media types,
statuses and declared endpoint limits. These are the exported endpoint limits;
inherited server/proxy limits and file MIME detection remain server policy.
Upload field descriptors (`part.kind === "file"`) expose `accept` (MIME media types
and dot-prefixed extensions formatted for HTML file inputs) and `maxBytes` (maximum file byte size),
composed from the endpoint's validation rules and multipart limits.
Unsafe-size metadata numbers remain `import("./generated/contracts_foundry.gen.js").JSONNumber` values instead of being rounded
or clamped. Enum `choices` use the existing codec's client values, including wide
integers as strings; each type's choices are decoded once and shared. Metadata
and descriptors are immutable snapshots checked against the manifest version. Unknown
JavaScript operations, fields, locations and variants throw `ContractError`.

<a id="recipe-issue-addresses"></a>
## Address nested and escaped issues

Use each descriptor's `path` with the SDK's decoded issues. Operation paths include
the request location, such as `/body/tags/0`; reusable schema paths are relative
to that schema root. JSON Pointers escape `/` as `~1` and `~` as `~0`. Preserve
those paths instead of dot-joining names or reducing them to the first field.

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/testdata/guide-recipes.ts#issue-addresses -->
```typescript
export function issueAddresses(issues: readonly sdk.Issue[]) {
  const tag = sdk.operation("itemsEcho").field("body", "tags").at(0);
  const label = sdk.operation("unionsEcho").field("body", "method")
    .variant("kind", "card").field("labels").at(0);
  const escaped = sdk.schema(sdk.schemas.ExplicitLabels).at("a/b~c");
  return {
    tag: issues.filter(issue => issue.path === tag.path),
    label: issues.filter(issue => issue.path === label.path),
    escapedPath: escaped.path,
    all: issues,
  };
}
```

Keep parent summaries and unmatched or form-level issues visible. A literal field
name can itself look like a pointer; any app adapter for top-level summaries must
keep those names separate from exact-path keys. Retain localized server messages
and use the SDK decoder instead of parsing the error envelope again.

## Portal authentication descriptors

A surface with `Surface.Auth` exports `authDescriptor` with its guard, typed
credential identifier, and login, refresh, logout, and profile functions.
`PasswordCredentials` comes from the login request DTO; `signInIdentifier` is one
of its JSON keys. `Surface.Auth.Identifier` must match that JSON key exactly,
including case. `createPortalSession` connects that descriptor to an injected
transport and the shared optional auth runtime. See
[portal sign-in sessions](client-contracts.md#portal-sign-in-sessions) for route
declarations, cookie requirements, and session ownership.

<a id="recipe-field-presentation"></a>
## Declare presentation once

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/dto.go#field-presentation -->
```go
Amount   decimal.Decimal                        `json:"amount" client:"kind=money,label=fields.amount,help=help.amount"`
State    localization.Status                    `json:"state"`
Optional value.Optional[value.Nullable[string]] `json:"optional,omitzero" client:"kind=multiline"`
```

The optional `client` tag applies to the already declared public field in DTOs,
paths, queries, URL-encoded forms and multipart inputs. It does not opt private
fields into transport. Generated validation selectors inherit the label key,
so the same declaration can supply form labels and validation labels. Label/help
keys use `i18n.MessageKey`; applications supply their locale messages.

The excerpt is part of the fixture's public `Payload` DTO. Typed parameter
bindings use the same presentation metadata:

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/presentation.go#parameter-presentation -->
```go
func SearchPresentation() foundryhttp.QueryParameter[Member] {
	parameter := foundryhttp.QueryParam("display", foundryhttp.StringQuery[string](), func(member *Member) *string { return &member.Display })
	return parameter.WithPresentation(contract.Presentation{Kind: contract.TextPresentation, LabelKey: "fields.display"})
}
```

Manual JSON schemas use `contract.Property.Presentation`. Typed HTTP parameters
and parts use `.WithPresentation(contract.Presentation{...})`. Kinds are the
typed constants `TextPresentation`, `MultilinePresentation`,
`PasswordPresentation`, `EmailPresentation`, `URLPresentation`,
`MoneyPresentation` and `FilePresentation` (`contract.PresentationKinds()`;
TypeScript's `PresentationKind` is generated from the same set). Omit kind to use
ordinary schema, enum and validation metadata. No property-name inference is
performed. A repeated parameter's or part's hint describes each element.

Money requires the exact decimal codec and does not imply a currency, scale or
rounding rule. Text hints require unformatted non-enum strings. File hints apply
only to actual multipart file parts. Keys are bounded by
`contract.MaxPresentationKeyBytes` (128 bytes, the message-key limit). Unknown
options/kinds and malformed keys fail generation. Contradictions generation can
see fail there with a source diagnostic: any kind on an enum field, and hints on
plain scalar DTO, path, query, form and multipart text fields. Concrete
custom/generic codecs are checked when the descriptor is constructed, and that
error names the type and property or parameter. Manifest assembly also rejects
conflicting presentation/validation label keys (including a comparison's other
field) and opposed email/URL rule hints, per element for repeated inputs.

Presentation contains no values, defaults, examples, scripts or component names.
Password hints are input-only. Registration rejects every output whose graph
reaches a password hint, whether or not clients are exported: route registration
for JSON and event-stream responses, the table declaration for its rows, the
notification registry for inbox and realtime payloads, and the WebSocket registry
for presence members and server-to-client events. Client-to-server events,
notification inputs and private transport payloads remain inputs. Exported
contracts re-check these outputs and reject credential body examples and
defaults. Credentials never travel in URLs, which proxies, logs and browsers
retain: path parameters and endpoint query parameters reject password hints,
and manifests reject them on any path, query or realtime room parameter.
Request bodies, including URL-encoded form fields and multipart text parts,
remain valid password inputs. This explicit marker does not discover sensitive
fields by name; continue separate request/response DTOs and explicit response
projections.
OpenAPI publishes `x-foundry-presentation` and marks password fields `writeOnly`.

File field descriptors compose `accept` from the declared MIME types and file
extensions independently. An extension-only constraint such as `csv` produces
`.csv` without requiring a MIME constraint. `maxBytes` carries the declared
upload size limit.

## Validation and compatibility

Descriptors call the existing `validateRequest`; they do not interpret a second
rule schema. Always inspect `complete` and `skipped`. Request preparation and
server-only rules remain incomplete on the client even when `issues` is empty.
No implicit transforms, parsing into floating point, mutation retries or state
management are added.

Manifest **version 7** carries presentation metadata. Regenerate saved manifests,
OpenAPI and TypeScript together using the same framework/tool revision. Older
formats are rejected explicitly. Existing direct client calls remain available.
The [form controller continuation](client-forms.md) implements state and optional
frontend adapters separately; consult the master for its acceptance status.

## Starter adoption

After a reviewed framework revision is published, pin that same revision for the
runtime and generator. Regenerate Go output, manifest version 7, OpenAPI and the
TypeScript client together, then rebuild the existing ESM package. A package that
exports the generated module directly also exports these new descriptor APIs.
Review explicit schema-type imports if a name collides with a new SDK declaration.

Add domain presentation tags and locale messages in the application's Go
declarations; keep separate request and response views. Extend the installed
package's tests with typed field lookup, actual descriptor calls and validation
incompleteness. The framework's
[acceptance record](../evidence/client-descriptors-20260930.json) covers its own
consumers; it does not replace the starter's upgrade checks or close B08.
