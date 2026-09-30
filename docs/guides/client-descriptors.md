# Typed client descriptors and presentation

The generated SDK exposes `operation(name)` and `schema(typeID)` alongside the
existing typed client. They inspect the same normalized manifest; neither sends
requests nor creates form state. React, Vue, Angular and plain JavaScript can
consume them without runtime package dependencies.

```ts
const create = operation("projectsCreate");
const budget = create.field("body", "budget");
const kind = budget.presentation.kind;
const required = budget.required;
const nullable = budget.nullable;
const report = create.validate(request);
const response = await create.call(client, request);
```

The operation name, location and field must exist in the generated contract.
`FieldValue<typeof budget>` retains its exact value type, including omission and
null. Descriptor identity also retains its operation/location/field owner, so
unrelated fields cannot be substituted merely because their values are strings.
Nested objects use `.field(name)`; arrays and open maps use `.element()`.
Tagged unions require `.variant(discriminator, tag)` before selecting payload
fields. A variant descriptor describes the payload without the discriminator,
which belongs to the parent union. Enum-keyed maps retain their explicit keys.

`schema(typeID)` describes an explicitly public schema without a route or submit
method. `operation(name).metadata` exposes route identity, access, media types,
statuses and declared endpoint limits. These are the exported endpoint limits;
inherited server/proxy limits and file MIME detection remain server policy.
Unsafe-size metadata numbers remain `JSONNumber` values instead of being rounded
or clamped. Enum `choices` use the existing codec's client values, including wide
integers as strings. Metadata and descriptors are immutable snapshots. Unknown
JavaScript operations, fields, locations and variants throw `ContractError`.

## Declare presentation once

```go
//foundry:dto
type CreateProject struct {
    Budget decimal.Decimal `json:"budget" client:"kind=money,label=fields.budget,help=help.budget"`
    Notes value.Optional[value.Nullable[string]] `json:"notes,omitzero" client:"kind=multiline"`
}
```

The optional `client` tag applies to the already declared public field in DTOs,
paths, queries, URL-encoded forms and multipart inputs. It does not opt private
fields into transport. Generated validation selectors inherit the label key,
so the same declaration can supply form labels and validation labels. Label/help
keys use `i18n.MessageKey`; applications supply their locale messages.

Manual JSON schemas use `contract.Property.Presentation`. Typed HTTP parameters
and parts use `.WithPresentation(contract.Presentation{...})`. Kinds are the
typed constants `TextPresentation`, `MultilinePresentation`,
`PasswordPresentation`, `EmailPresentation`, `URLPresentation`,
`MoneyPresentation` and `FilePresentation`. Omit kind to use ordinary schema,
enum and validation metadata. No property-name inference is performed.

Money requires the exact decimal codec and does not imply a currency, scale or
rounding rule. Text hints require unformatted non-enum strings. File hints apply
only to actual multipart file parts. Keys are bounded by
`contract.MaxPresentationKeyBytes`. Unknown options/kinds, malformed keys and
known codec contradictions fail generation or descriptor validation; concrete
custom/generic codecs are checked at runtime descriptor construction. Manifest
assembly also rejects conflicting presentation/validation label keys and opposed
email/URL rule hints.

Presentation contains no values, defaults, examples, scripts or component names.
Password hints are input-only: nested password graphs cannot become HTTP,
notification, table, presence or server-event outputs, and credential body
examples/default parameters are rejected. Ordinary public request passwords
remain valid. This explicit marker does not discover sensitive fields by name;
continue separate request/response DTOs and explicit response projections.
OpenAPI publishes `x-foundry-presentation` and marks password fields `writeOnly`.

## Validation and compatibility

Descriptors call the existing `validateRequest`; they do not interpret a second
rule schema. Always inspect `complete` and `skipped`. Request preparation and
server-only rules remain incomplete on the client even when `issues` is empty.
No implicit transforms, parsing into floating point, mutation retries or state
management are added.

Manifest **version 6** carries presentation metadata. Regenerate saved manifests,
OpenAPI and TypeScript together using the same framework/tool revision. Older
formats are rejected explicitly. Existing direct client calls remain available.
The [form controller continuation](client-forms.md) implements state and optional
frontend adapters separately; consult the master for its acceptance status.

## Starter adoption

After a reviewed framework revision is published, pin that same revision for the
runtime and generator. Regenerate Go output, manifest version 6, OpenAPI and the
TypeScript client together, then rebuild the existing ESM package. A package that
exports the generated module directly also exports these new descriptor APIs.
Review explicit schema-type imports if a name collides with a new SDK declaration.

Add domain presentation tags and locale messages in the application's Go
declarations; keep separate request and response views. Extend the installed
package's tests with typed field lookup, actual descriptor calls and validation
incompleteness. The framework's
[acceptance record](../evidence/client-descriptors-20260930.json) covers its own
consumers; it does not replace the starter's upgrade checks or close B08.
