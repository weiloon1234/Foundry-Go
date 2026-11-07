# Application HTTP errors

<a id="agent-entry"></a>
## Agent entry

- Declare: [Public structured failures](#recipe-error-details)
- Handle: [Client error narrowing](#recipe-client-narrowing)

## Overview

Further reading: [Localized messages](validation-messages.md).

Declare a public error once and reuse the value in the endpoint and handler. The declaration owns a semantic `ErrorCode`, a status from 400 through
599, and static public text. This is the same metadata the router exports.

```go
var SeatUnavailable = foundryhttp.DefineError(
    "seats.unavailable", 409, "The selected seat is no longer available.",
)

var Reserve = foundryhttp.ConfigureEndpoint(foundryhttp.DefineEndpoint(
    foundryhttp.DefineRoute(
        foundryhttp.RouteSpec{ID: "seats.reserve", Method: foundryhttp.POST, Access: foundryhttp.Public},
        foundryhttp.StaticPath("/seats/reserve"),
    ),
    foundryhttp.EmptyQuery(),
    foundryhttp.EmptyBody(),
    foundryhttp.EmptyResponse(204),
), foundryhttp.Errors(SeatUnavailable))
```

The [consumer example](../../tests/fixtures/consumer/httpendpoints/errors.go)
registers a thin handler calling a service. At the HTTP boundary, return `SeatUnavailable`
directly, or `SeatUnavailable.WithCause(err)` to retain an internal cause.
`errors.Is(err, SeatUnavailable)` checks the declaration and ordinary
`errors.As`/`errors.Is` can inspect wrapped causes. A cause's text never supplies
the public response message. Declarations and their wrapping errors format only
the public code. Wrapping and joining retain the existing outer/first
classification rules. Prefer declaration values over string comparisons.

A typed endpoint exposes only its declared application errors. An undeclared
custom error, including the same code with different metadata, becomes a safe
internal failure and is diagnosed in the request logger. The same check covers
middleware registered on that route. Built-in errors remain available through
[the shared error catalog](http-requests.md); they need no `Errors` entry.
`ErrorCode("seats.unavailable")` alone does not create a declaration.

`foundryhttp.InspectEndpoint(endpoint)` and `Router.Endpoints()` include application error
metadata. `Router.ErrorDefinitions()` returns a deterministic catalog containing
built-in and endpoint-declared errors. Returned metadata is owned; editing a
snapshot cannot change runtime behavior. Duplicate codes in one endpoint and
conflicting definitions across endpoints reject router assembly. Multiple
endpoints can reuse the same declaration. [OpenAPI and TypeScript exporters](client-contracts.md) publish these registered error contracts.

In locale-enabled applications, a declared error's public message can be
translated. `SeatUnavailable.MessageDefinition()` returns its parameter-free
catalog signature, `http.error.seats.unavailable`; add it to
`application.FeatureDeclarations.Messages` and supply translations in `Catalog`,
as for the built-in `http.error.<code>` keys described in
[validation messages](validation-messages.md). Locales without a translation keep
the declared message. Code, status and exported metadata never change.

Zero/invalid declarations reject assembly and produce safe internal failures if
passed directly to `WriteError`. Public text must be nonblank, valid UTF-8,
NUL-free and at most 16KiB. Built-in codes are reserved. Declare only text that
is appropriate to expose, including for explicitly declared 5xx failures.
Arbitrary internal Go errors still use the generic internal-error response.
[Localized error messages](localization.md) resolve declared message keys through the request locale and shared catalog.

Raw handlers may call `WriteError` explicitly with a valid **plain** declaration. Global
wrappers outside `Router` and raw handlers have no typed endpoint contract;
their responses do not acquire inferred endpoint error metadata. Model and
private-cause values never become fields in the public failure envelope.

The ordinary HTTP adapter preserves HEAD behavior, correlation and policy
headers, safe cache defaults, and ownership of custom error methods. Error
classification contains panic and `runtime.Goexit` before committing a response.
It waits for the custom method to finish and never formats an application cause.

<a id="recipe-error-details"></a>
## Typed details

Use a concrete DTO for a failure that needs public structured data. Its root
must be directly a non-nullable object. Alias, scalar, array, map, dynamic
and nullable roots are refused. A details type cannot be the error envelope
itself. Nested fields retain the normal DTO rules.

[Source and imports in the matching GitHub tree](../../tests/fixtures/consumer/clientcontracts/member_create.go).

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/member_create.go#http-application-errors-member-exists-details -->
```go
//
//foundry:dto
type MemberExistsDetails struct {
	ExistingMemberID model.ID[models.User] `json:"existing_member_id"`
	Existing         MemberConflictState   `json:"existing"`
}

//foundry:dto
type MemberConflictState struct {
	Name  string              `json:"name"`
	State localization.Status `json:"state"`
}

var MemberExists = foundryhttp.DefineErrorDetails(
	"member_exists", 409, "The member already exists.", MemberExistsDetailsJSON(),
)
var RegistrationClosed = foundryhttp.DefineError(
	"registration_closed", 409, "Registration is closed.",
)
```

Run the normal [generator](typed-api-workflow.md) to produce the descriptor.
The exact declarations belong to the endpoint's error contract:

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/member_create.go#declare-error-endpoint -->
```go
var MemberCreate = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "members.create", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/members")),
	foundryhttp.EmptyQuery(), foundryhttp.JSONBody(CreateMemberJSON()), foundryhttp.JSONResponse(201, MemberCreatedJSON()),
	foundryhttp.Errors(MemberExists, RegistrationClosed),
)
```

Map service errors at the HTTP boundary. The fixture entries are public;
applications check current resource authorization before disclosing details.

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/member_create.go#map-domain-errors -->
```go
func HandleMemberCreate(_ context.Context, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, CreateMember]) (MemberCreated, error) {
	id, err := createMember(in.Body.Name)
	var existing *existingMember
	if errors.As(err, &existing) {
		// These fixture entries are public. Real handlers check current resource
		// authorization before exposing an existing member's identity or state.
		return MemberCreated{}, MemberExists.New(MemberExistsDetails{ExistingMemberID: existing.ID, Existing: MemberConflictState{Name: existing.Name, State: existing.State}}).WithCause(err)
	}
	if errors.Is(err, registrationClosed) {
		return MemberCreated{}, RegistrationClosed.WithCause(err)
	}
	return MemberCreated{ID: id}, err
}
```

`MemberExists.New` requires the declared payload type; the declaration itself is not an error.
Do not mutate a bound payload before the response is written.

`MemberExists.Matches(err)` identifies this exact declaration and schema through
wrappers. It does not extract the payload. `errors.Is` and `errors.As` still find
the cause. Formatting and server diagnostics never expose payload or cause text;
the response contains only declared public text and successfully encoded details.
`Validate`, `Description` and `MessageDefinition` work as for plain declarations.

Services should return domain errors. Map them to these declarations at the HTTP
boundary, as `HandleMemberCreate` does with `errors.As`. This keeps a service usable
from commands, jobs or MCP without depending on HTTP status codes or requiring a
transport payload extraction API. A model-owned identifier prevents mixing Go
owners; it does **not** authorize disclosure. Check current resource and tenant
authorization before exposing the existing record's identifier or state.

Details reach the wire only when the matched typed endpoint declares that exact
code, status, message, details type and schema. An undeclared or mismatched error,
an unmatched request, or a raw route fails closed to `internal_error` without
details. Encoding failures, panics, and whole-envelope size or depth overruns do
the same and are reported as server failures. Encoding runs once and is independent
of request cancellation, so a canceled context does not turn a valid 409 into 500.
Outer classification still wins: `http.Conflict.WithCause(detailed)` returns
409 `conflict` without details.

Router assembly refuses details on status 401, details on HEAD endpoints,
password output in details, and conflicting definitions or schemas for one code.
A plain declaration and a detailed declaration for the same code conflict.
Assembly also refuses response byte, node or depth limits below the declared
error's static floor: its envelope with empty details and the depth required by
the details schema. Larger payloads and localized messages can still exceed the
limits at runtime. The encoder reserves envelope work from the shared Steps budget.
Manifest readers repeat the relevant checks, including for hand-made documents.
Status 401 cannot carry details because auth transports may discard its body and
retry. Keep authentication failures separate from business conflicts.

<a id="recipe-client-narrowing"></a>
## Client narrowing

A client whose manifest includes detailed errors exports correlated unions at
`Operations["membersCreate"]["error"]`. The operation guard checks operation
membership, status and code; narrowing the code then selects the payload.
Plain variants have no `details` property. Full and portal entries use the same
`APIError` class and their own projected operations and codes. A portal with no
detailed errors generates no `isOperationError`, so code shared across portals
cannot assume that export exists.

<!-- compiled-source: tests/fixtures/consumer/clientcontracts/testdata/error-details.ts#http-application-errors-example-2 -->
```typescript
export async function createMember(api: sdk.API, name: string) {
  try {
    return (await api.membersCreate({ body: { name } })).id;
  } catch (error) {
    if (sdk.isOperationError("membersCreate", error) && error.code === sdk.ErrorCode.MemberExists) {
      return error.response.details.existing_member_id;
    }
    throw error;
  }
}
```

The [compiled client example](../../tests/fixtures/consumer/clientcontracts/testdata/error-details.ts)
imports `sdk` from the generated entry. Forms return the same narrowable error in
failed submissions. Missing or malformed required details produce
`ResponseContractError`; tolerant enum decoding preserves newer server cases as
`UnknownEnumValue`. The guard validates the current received envelope each time.
`APIError` remains extensible for application metadata, causes and stack traces.

Idempotent rejection replay is deferred. A detailed 409 from inside the
[idempotent runner](idempotent-operations.md) rolls back the business write, outbox entry
and claim. It consumes no retained-outcome quota. A same-key retry executes
against current state and can return different current details. Only a successful
committed result is stored for replay; do not treat a rejection as a durable
cached answer.
