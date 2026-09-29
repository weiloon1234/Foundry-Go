# Typed validation

See [request-aware validation messages](validation-messages.md) for automatic HTTP
rendering, built-in translations, typed overrides and generated client presentation.

See [expanded rules and concurrent validation](validation-expanded.md) for the
full rule catalog, typed model lists and bounded concurrent database/API checks.

Foundry generates validation fields from `//foundry:dto` structs using the same
JSON names, promotion rules and field types as the DTO's transport descriptor.
Applications compose ordinary Go rule values and inject dependencies explicitly.
Validation leaves input values unchanged.

The [consumer HTTP validation fixture](../../tests/fixtures/consumer/httpvalidation/validation.go)
adds rules to an existing typed endpoint:

```go
var Update = httpendpoints.Update.WithBodyValidation(
    httpdto.UpdateUserValidationFields().Email.Rules(
        validation.Optional(validation.NonBlank[string]()),
    ),
    httpdto.UpdateUserValidationFields().Nickname.Rules(
        validation.Optional(validation.Nullable(validation.MinLength[string](2))),
    ),
)
```

`Email` remains `value.Optional[string]`; `Nickname` remains
`value.Optional[value.Nullable[string]]`. A rule for another value or DTO type
fails compilation. The application does not repeat JSON names or write selectors.
Models remain separate from request/response DTOs.

## Execution and HTTP responses

`WithBodyValidation`, `WithQueryValidation` and `WithPathValidation` add rules
in declaration order. `WithValidation` accepts a rule for the complete concrete
`http.Input[P, Q, B]`, including request-level custom checks. These methods return
independent endpoint declarations. Empty or invalid rule declarations fail
registration; they do not silently disable validation.

Foundry decodes input, executes rules, and calls the handler only after validation
passes. Body diagnostics include paths such as `/body/email`; query and path
rules use their corresponding source prefixes. Request-level checks can report
at the root. Business transactions remain explicit domain operations.

Rejected rules produce HTTP 422 with the shared `contract.Issue` fields `path`,
`code` and optional `message`/`label`. Messages and labels are approved static
declaration text.
`issues_truncated` reports when the issue cap stopped further checks. Decoding
errors remain 400; validation work/value bounds return 400, request cancellation
returns 408, and rule infrastructure or panic failures return 500 without field
details. A validation failure is distinct from an incomplete validation attempt.

The ordinary HTTP error writer also recognizes wrapped `*validation.Errors`.
An explicitly classified internal error keeps its cause private, even when the
cause contains validation errors. Neither arbitrary error text nor submitted
values become public messages.

## Using rules outside HTTP

`Rule[T].Check(ctx, input, limits)` is usable from services, commands and jobs.
It returns nil only after every applicable check passes. Inspect
`*validation.Errors` with `errors.As`; `Issues()` returns an independent slice
and `Truncated()` describes the issue cap. `LimitError`, cancellation and internal
failures remain separate outcomes.

`All` checks children in order and accumulates bounded diagnostics. `Bail` stops
its own sequence after the first rejection; an enclosing `All` may continue
with other fields. `Each` validates slice elements in order and includes their
indices in issue paths. `EachKey` and `EachValue` validate map keys and values in
ascending key order with entry paths such as `/labels/en`; they accept native
string or integer keys and reject key types with custom codecs. Configure
traversal, depth, issue and text limits through `validation.Limits`; HTTP uses
`EndpointLimits.Validation`.

A tree of framework rules runs inline in the caller's goroutine. Valid input
allocates no issue storage and no JSON Pointer path strings; paths are built only
for rejected values. A tree containing an application callback (`Custom`,
`Dynamic`, `Hook`, a lookup, `Provide` or image measurement) runs in one owned
goroutine per `Check`, as before.

For explicit adapters, `DefineField[T, V]` is the typed constructor used by
generation. Generated DTO fields are the normal application entry point.

## Presence, nulls and native values

- `Present[T]` requires an `Optional[T]` to have been supplied, including a zero
  or null inner value. `Optional` skips omission and validates present values.
- `Nullable` skips explicit null; `NotNull` rejects it. Compose wrappers for
  patches instead of inferring wire presence from ordinary Go zero values.
- `NonBlank` checks trimmed text without changing it. `MinLength` and `MaxLength`
  count Unicode code points. `Matches` uses Go regular-expression semantics.
- `Min` and `Max` compare native or named numbers in their original type; integer
  comparisons do not pass through floating point. Floating-point bounds and
  values must be finite. `DecimalMin` and `DecimalMax` preserve exact decimals.
- `Compare` binds two fields with the same request and value types to a
  `Rule[Pair[V]]`. `Same` and `Different` provide scalar comparisons; custom pair
  rules support other concrete types. Rejections use the first field's location,
  and metadata retains the second field's declared name.

Generated selectors follow the actual JSON-winning Go field, including a field
promoted past an ignored shadow. A field promoted through an embedded pointer
adds an outer `Optional` to preserve the pointer's absence. When promoted fields
share a Go name, generated `Field_…` aliases retain their distinct Go paths;
inspect their generated comments and concrete types through gopls. Properties
must have an accessible Go selector and a type the generating package can name.

## Custom rules and metadata

`Custom[T]` takes a `Spec` and `func(context.Context, T) (bool, error)`. Return
`(false, nil)` to reject input and an error when execution fails. Callbacks must
finish their work and observe cancellation; Foundry waits for their resource
ownership to end. Panics and `runtime.Goexit` become internal failures. Panics
in selectors and value methods are also internal failures. In a tree without
application callbacks those run inline, so `runtime.Goexit` there ends the
calling goroutine like any other Go function.

`Dynamic[T]` chooses the rejection message at check time. Its callback returns
the zero `Rejection` to accept, `validation.Reject(text)` for literal public
text, or `validation.RejectWith(generatedMessage, args)` for a typed catalog
message with check-time arguments such as a remaining quota. The spec supplies
the stable code and declared message; `Reject("")` uses that declared message.

`Hook[T]` runs an application callback after the preceding rules in its
sequence and receives a `*Report`. `report.Add("items", "3", "sku")` records an
issue at `/items/3/sku` relative to the hook's input; `AddRejection` adds a
check-time message. Place a hook after field rules in `Bail` when it needs them
to pass first. Reports are valid only during the call, respect the issue cap and
are not safe for concurrent use. Returned errors are execution failures.

Reuse a custom rule value when applying the same semantic ID to multiple fields.
Different definitions with one ID are rejected. Built-in `foundry.*` IDs are
reserved. `WithMessage` overrides a leaf's public text while retaining its rule
behavior and definition identity.

`Description()` returns owned metadata from the same rule tree used by `Check`.
Endpoints include this tree in `EndpointInfo.Validation`. Custom rules and Go
regular expressions are marked server-only. Future client generation must retain
that distinction and preserve exact integer and decimal bound representations.

## Conditions, enums and collections

`When(condition, rules...)` runs a branch when its typed condition passes.
`Unless` runs it when the condition rejects. The condition's diagnostic stays
private; cancellation and infrastructure errors still fail the whole check.
Conditions and branches share the work budget. Reuse pure condition rules so
validation does not cause domain side effects.

The independent `validationrules` consumer composes generated fields:

```go
fields := RegistrationValidationFields()
business := fields.Kind.Rules(validation.OneOf(Business))

rules := validation.All(
    fields.Kind.Rules(validation.Enum(Business.EnumDescriptor())),
    fields.Email.Rules(validation.Email[string]()),
    validation.When(business, fields.Company.Rules(validation.Bail(
        validation.Present[string](),
        validation.Optional(validation.NonBlank[string]()),
    ))),
    validation.Unless(business, fields.Company.Rules(validation.Absent[string]())),
    fields.Tags.Rules(
        validation.MinItems[[]string](1),
        validation.MaxItems[[]string](8),
        validation.Distinct[[]string](),
        validation.Each[[]string](validation.NonBlank[string]()),
    ),
)
```

`Company` is `value.Optional[string]`. `Absent` rejects every supplied value,
including an empty string or a nullable inner value. It specifically requires
omission. `Pointer` skips nil pointers and validates non-nil values; `NotNil`
requires a non-nil pointer. Ordinary pointers do not distinguish JSON omission
from null.

`OneOf` and `NotOneOf` retain native scalar types and exact integer values.
`DistinctIgnoringCase` compares text elements after Unicode case folding and is
server-only.
They own their membership lists and reject duplicate or invalid declarations.
`Enum` uses the existing generated descriptor, including its actual JSON cases.
`MinItems` and `MaxItems` accept ordinary or named slices; nil and empty slices
have length zero. `Distinct` checks scalar elements with native equality and
bounded work. It reports at the collection path. `Each` reports element paths.
None of these rules normalize input.

## Format rules and wire representations

String rules include `Email`, `URL`, `IP`, `IPv4`, `IPv6`, `UUID`, `Date`,
`Time`, `DateTime`, `LocalDateTime` and `JSON`. Named string types retain their
type. Email accepts a single bare mailbox without display names or comments;
URL requires an absolute HTTP/HTTPS URL with a hostname. These rules perform no
DNS or reachability checks. `DateFormat[S](layout)` accepts text written exactly
in a Go reference-time layout such as `"02/01/2006"`; the value must format back
to the same text, and the layout itself must round-trip. IP literals reject zones and CIDR suffixes. UUID
and temporal checks reuse Foundry's parsers; UUID representation permits the nil
UUID. JSON uses the shared strict parser and rejects duplicate object keys.

Metadata marks Go-specific parser rules and native rules on custom wire-coded
scalars as server-only. For example, a named string may hold `"YES"` while its
JSON codec emits `"yes"`; native `OneOf` cannot claim to operate on both values
identically. `Enum` instead exports the descriptor's validated wire cases.
Collection metadata accounts for custom container codecs and base64 byte slices.
Detection does not invoke codecs; enum declaration validation executes them at
construction inside the owned callback boundary. Client generation must retain
these distinctions and the exact numeric metadata.


## Text formats and temporal comparisons

`StartsWith` and `EndsWith` preserve exact case and the input's named string type.
`Alpha` follows Unicode Alphabetic characters; `AlphaNumeric` also accepts
Unicode Number characters. `Digits` accepts ASCII digits and preserves leading
zeros. Empty text passes these character-only rules; compose `NonBlank` when
the field must contain text. Unicode-table rules carry server-only metadata.

`Timezone` accepts UTC, installed IANA names and fixed `+/-HH:MM` offsets smaller
than 24 hours. It rejects empty and machine-dependent `Local`. The shared
`temporal.ParseTimeZone` returns an ordinary `*time.Location` for use with
`DateTime.LocalIn` and `LocalDateTime.In`. `temporal.LoadTimeZone` and query
timezone descriptors accept explicit named zones only. Named zones use Go's
configured timezone database; there is no implicit process-local fallback.

For concrete temporal fields, `Before`, `BeforeOrEqual`, `After` and
`AfterOrEqual` take same-type bounds. `BeforeField`, `BeforeOrEqualField`,
`AfterField` and `AfterOrEqualField` compare generated fields directly:

```go
fields := AvailabilityWindowValidationFields()
rules := validation.BeforeField(fields.Start, fields.End)
```

Both fields retain `temporal.DateTime`. Mixing a date, local wall time or instant
fails compilation. Failures identify the first field; metadata retains both wire
names. Date and local date-time zeros represent absence and reject. Midnight and
the zero UTC instant remain valid values. Optional and nullable fields use their
explicit wrappers and value-level bound rules.

Instants compare in UTC without monotonic readings; calendar/local values compare
in their own calendar order without choosing an application timezone. Native
`time.Time` is also supported within the framework's year range. Nanoseconds are
retained in comparisons and metadata; a future client must not reduce these
checks to JavaScript Date's millisecond precision. Bound rules capture a declared
value and do not read the current clock.

Relative rules read the clock at every check:

```go
future := validation.AfterNow[temporal.DateTime](services.Time())
dueDate := validation.AfterOrEqualToday[temporal.Date](services.Time())
```

`AfterNow`, `AfterOrEqualNow`, `BeforeNow` and `BeforeOrEqualNow` accept
`time.Time`, `temporal.DateTime` and `temporal.LocalDateTime`; local values
compare with the current wall time in the service's timezone. `AfterToday`,
`AfterOrEqualToday`, `BeforeToday` and `BeforeOrEqualToday` also accept
`temporal.Date` and compare calendar dates with today in that timezone; instants
are first converted into it. Pass the application's `temporal.Service` so a
frozen test clock and the configured `TimeZone` apply. A zero `temporal.Service`
makes the declaration invalid instead of silently reading the host clock in UTC.
A clock failure is an execution failure. These
rules are server-only and have their own message keys, such as
`validation.after_now`.


## Required, prohibited and empty content

For presence-sensitive input, use the existing `Optional` and `Nullable` field
types. The rule retains those types and distinguishes their states:

```go
fields := PresenceInputValidationFields()

rules := validation.All(
    fields.Label.Rules(validation.Required(validation.MaxLength[string](40))),
    fields.Nickname.Rules(validation.RequiredNullable(validation.MinLength[string](2))),
    fields.Count.Rules(validation.Required(validation.Min(0))),
    fields.Active.Rules(validation.Required[bool]()),
    fields.Forbidden.Rules(validation.ProhibitedNullable[string]()),
)
```

`Required` accepts `Optional[T]`; `RequiredNullable` accepts
`Optional[Nullable[T]]`. Both reject omission and empty content; the nullable
variant also rejects explicit null. Supplied numeric zero and false pass. Empty
means whitespace-only text or a collection with length zero. Ordinary structs
remain supplied values; domain constraints such as a valid date or non-nil model
identity are separate. Native values are inspected without invoking custom codecs.

Optional additional T rules run only after the presence/nonempty check succeeds
and bail at their first rejection. To override just the required message, use
`Required[T]().WithMessage(...)` or the nullable equivalent as a leaf and compose
subsequent checks with `Bail`/`Optional`/`Nullable`.

`Prohibited` accepts omission or empty content. `ProhibitedNullable` also accepts
null. Both reject supplied zero or false. `Absent` remains stricter: every supplied
value rejects, including null and empty text. Validation does not unset, trim or
otherwise alter values.

`Empty[T]` and `NonEmpty[T]` check native content without claiming wire presence.
They support concrete text, collection, numeric, boolean and struct values.
Unwrap Optional/Nullable/Pointer states with their typed adapters; ambiguous
wrappers, pointers and dynamic interfaces are rejected as declarations rather
than silently treating their zero values as absent. Nested optional states can
compose `Present`, `Optional` and the required rule for the inner value.

`When` and `Unless` retain typed conditional presence. The consumer's
`RequiredWithRules` composes generated trigger fields: when either optional count
or active flag is supplied, the label becomes required. Zero and false activate
that condition. No string field lookup or conversion to text is used.

Metadata carries the same empty-content category used at runtime and flags custom
wire encodings as server-only. These are checks of the decoded native value;
dynamic documents/custom transport representations need their own content rules.
Plain `omitempty` fields still cannot prove wire presence from their Go zero
value. Use explicit wrappers; additional ordinary-field presence metadata remains
milestone work.

## Field display names

Use a generated typed field's `WithLabel` method when a public display name
differs from its JSON name:

```go
fields := httpdto.UpdateUserValidationFields()
rule := fields.Email.WithLabel("Email address").Rules(
    validation.Optional(validation.NonBlank[string]()),
)
```

An HTTP rejection keeps its exact `/body/email` path and adds
`"label": "Email address"` beside its existing code and message. Labels are
static declaration text, never submitted input. They do not replace wire names
or interpolate values into messages. The same label is included in validation
metadata, including both field labels for a comparison. `WithLabelKey` and `Errors.LocalizeLabels` integrate with the delivered
[localization catalog](localization.md). `Errors.Localize` also renders shared rule messages; locale-enabled HTTP assembly does this automatically.

`WithLabel` returns an independent field and preserves its request/value types.
Scalar collection items inherit the collection field label. Nested fields use
their own labels; an unlabeled nested field does not inherit its parent's name.
Invalid or oversized labels fail declaration validation, and labels count toward
the existing aggregate metadata limit. Wire decoding errors omit the label.

## Numbers and decimals

`DecimalMin`, `DecimalMax` and `DecimalBetween` bound exact decimals.
`DecimalGreaterThan`, `DecimalGreaterOrEqual`, `DecimalLessThan` and
`DecimalLessOrEqual` compare two decimal fields through `Compare`.
`DecimalMaxPlaces(n)` limits significant fractional digits; decimals are
canonical, so `1.50` has one place. `DecimalMultipleOf(step)` requires an exact
integer quotient for a positive step such as `0.25`. None of these convert
through floating point.

## Image dimensions

Import `validation/imaging` as `imagingvalidation`:

```go
avatar := validation.Bail(
    validation.FileMaxSize[foundryhttp.UploadedFile](5 << 20),
    validation.FileContentTypes[foundryhttp.UploadedFile]("image/png", "image/jpeg"),
    imagingvalidation.Dimensions[foundryhttp.UploadedFile](imaging.DefaultLimits(),
        validation.DimensionConstraints{MinWidth: 128, MaxWidth: 4096, RatioWidth: 1, RatioHeight: 1}),
)
```

The rule reads at most `Limits.InputBytes` of the captured file and uses
`imaging.Inspect`; pixels are not decoded. Orientations that rotate by 90 degrees
swap width and height. Absent, oversized, unsupported or malformed files reject;
open/read failures are execution failures. `validation.Dimensions` accepts any
`ImageMeasurer` for other file sources.

## Advisory database rules

Import `validation/database` as `databasevalidation`. The adapter composes the
existing generated query and field descriptors:

```go
fields := models.UserFields()
available := databasevalidation.Unique(db, models.QueryUsers(), fields.Email)
active := databasevalidation.Exists(
    db,
    models.QueryUsers().Where(fields.Status.Eq(models.StatusActive)),
    fields.ID,
)

// An update may retain the current user's existing address.
availableForUpdate := databasevalidation.Unique(
    db,
    models.QueryUsers().Where(fields.ID.Ne(currentUser.ID)),
    fields.Email,
)
```

Each result is an ordinary `validation.Rule` with the field's concrete value
type. Bind it to generated DTO fields, or call `Check` directly. Wrong model
owners, values and IDs fail compilation. Nullable database fields use their
non-null value type; compose `Optional`/`Nullable` around the rule for patch input.
Natural keys and exact decimals retain their existing model codecs.

Lookups issue parameterized `SELECT EXISTS` through the shared query compiler.
They do not hydrate models, run model observers, or open implicit transactions.
Inject the pool or an existing transaction with the required lifetime. Query
filters and soft-delete visibility remain explicit; use `WithTrashed` when a
unique constraint includes deleted records. Pagination, ordering and eager
loading fail declaration validation instead of silently narrowing a lookup.

These checks are advisory observations. Database unique/foreign-key constraints
and authorization still govern the write, including concurrent changes after
validation. Values are compared as supplied; any domain normalization should
agree with persistence behavior. Query failures and cancellation are execution
failures, never proof that a value is available. Metadata exports stable rule
IDs and safe messages, marks these checks server-only, and excludes SQL,
credentials and scoped values.

The adapter uses `query.Lookup` for typed stored-field lookup and the base
`validation.Lookup[T]` interface for validation composition. This keeps ordinary
validation independent of SQL and permits other explicitly injected, typed
lookup implementations without a separate error or message system.
`databasevalidation.Lookup(db, source, field)` returns that typed lookup for use
with any base rule, including `validation.ExistsEach`.

## Check-time parameters

A rule tree is declared once, yet some scopes depend on the request: the row
being updated, or the tenant of the authenticated actor. Declare a typed
`validation.Slot` and provide its value per check instead of rebuilding rules:

```go
current := validation.NewSlot[model.ID[models.User]]()
fields := models.UserFields()
email := databasevalidation.UniqueIgnoring(db, models.QueryUsers(), fields.Email, fields.ID, current)

body := validation.DefineField("body", func(in UpdateRequest) UpdateUser { return in.Body })
endpoint := Update.WithValidation(validation.Provide(current,
    func(_ context.Context, in UpdateRequest) (model.ID[models.User], error) { return in.Path.User, nil },
    body.Rules(UpdateUserValidationFields().Email.Rules(validation.Optional(email))),
))
```

`Provide` derives the value from the check context and the input at its level,
here the path parameter, and makes it available to the rules below it. A derive
error is an execution failure. `slot.Value(ctx)` returns an error, never a zero
value, outside its `Provide`. `UniqueIgnoring` and `validation.Requires` record
that a rule reads a slot; `Validate`, and therefore endpoint registration,
rejects the tree until an enclosing `Provide` supplies it. Derive exclusions
from trusted route or actor data, never from an untrusted body field alone.

`databasevalidation.Scoped(db, scope, field)` derives the whole model scope at
every check, for example from typed request metadata:

```go
lookup := databasevalidation.Scoped(db, func(ctx context.Context) (models.UserQuery, error) {
    tenant, err := tenantOf(ctx) // application-owned typed accessor
    return models.QueryUsers().Where(fields.TenantID.Eq(tenant)), err
}, fields.ID)
member := validation.Exists(lookup)
```

The returned scope must satisfy the ordinary lookup restrictions; an invalid
scope or scope error fails execution and is never treated as a missing or
available value. A scoped lookup also serves `ExistsAll`, `UniqueAll`,
`ExistsEach` and `UniqueEach`.

## Transport and later integrations

[JSON/query presence](http-presence.md), typed collection rules and
[multipart upload validation](http-uploads.md) are delivered. Their runtime
metadata is shared with endpoint inspection. Bounded image inspection is available through [imaging](imaging.md), and
[localization](localization.md) provides catalogs and label translation. Generated
clients execute supported rules and explicitly report skipped server checks.
Request-locale rule messages are described in
[validation messages](validation-messages.md).
