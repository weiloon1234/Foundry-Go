# Typed forms and request lifecycle

T03 adds URL-encoded bodies and opt-in preparation/authorization. Native verification passed; milestone acceptance is owned by the [master](../../blueprint/00-master-architecture-and-parity.md#typed-api-delivery).
The [public consumer](../../tests/fixtures/consumer/requestflow/forms.go) contains
the complete declarations, handlers and shared rules below.

## Declare one concrete form

```go
//foundry:form
type Submission struct {
    Name string `form:"name"`
    Title value.Optional[string] `form:"title"`
    Tags []string `form:"tags[]"`
}
```

Generation produces `SubmissionDescriptor()` and `SubmissionValidationFields()`.
The descriptor is `http.Query[Submission]`: the existing typed URL scalar bindings
are reused by `http.FormBody(SubmissionDescriptor())`. Pass that body to
`http.DefineEndpoint` with your route, independent query descriptor and response.
Generated validation fields retain both the DTO and field value types. Share
ordinary `validation.Rule[string]` values across form, query, JSON and multipart
fields without repeating the domain rule definitions.

The body media type is exactly `application/x-www-form-urlencoded`. UTF-8 charset
and identity content encoding are supported; unsupported media, charset or
compression returns 415. Percent decoding is strict; `+` means a space. Duplicate
scalar fields reject, while repeated fields retain order. Unknown names produce
redacted diagnostics. A query field never satisfies a body field with the same
name. Errors retain `/query/...` and `/body/...` source paths.

Omission leaves `value.Optional` unset. `title` and `title=` supply empty text;
`title=null` supplies the text "null". Nullable text has no implicit form encoding:
use JSON when omission/null/value are all needed. Structural decoding failures
return 400; decoded values rejected by domain rules return 422.

`EndpointLimits.Form` has separate byte, pair and issue caps. A byte overflow
returns 413, including chunked input; pair overflow returns 400. The parser has a
flat grammar, so wire nesting depth is zero. Dots/brackets are literal declared
names. `MergeQueries` and `EmbedQuery` compose concrete Go fields explicitly;
they never interpret arbitrary PHP-style nested keys. Kernel body limits remain
an additional ceiling. An unused zero Form budget is allowed for old explicit
non-form limit literals; a form endpoint requires positive form limits.

## Normalize, validate, authorize

```go
type Request = http.Input[http.NoPath, Search, Submission]

func prepare(ctx context.Context, input Request) (Search, Submission, error) {
    body := input.Body
    body.Name = strings.TrimSpace(body.Name)
    if !body.Title.IsSet() {
        body.Title = value.Set("Member")
    }
    return input.Query, body, nil
}

endpoint = endpoint.WithPreparation(prepare).
    WithBodyValidation(fields.Name.Rules(nameRules)).
    WithAuthorization(func(ctx context.Context, input Request) error {
        return authorizeSubmission(ctx, input.Body)
    })
```

Existing credential, signature, CSRF and transport admission runs first, then
path/query/body decoding. Preparation returns only query and body; the adapter
retains path identity. Validation uses prepared values, request authorization
follows validation, then resource binding and the handler execute. Preparation
cannot repair invalid syntax or a missing codec-required field. Declare a field
Optional if preparation supplies its default.

Built-in `Prohibited`/`Absent` rules also check the original decoded input before
preparation. Their conditions evaluate the original input on this preflight and
the prepared input during normal validation. Thus normalization cannot erase a
forbidden supplied field. This is not an element identity mapping across arbitrary
collection transforms. Use explicit domain rules for application-specific policy.

Preparation and authorization callbacks must not perform business writes. Their
input is borrowed: copy referenced maps/slices before changing them. Preserve
Optional/Nullable state unless an explicit application transformation changes it.
Callbacks return ordinary errors; use declared safe HTTP errors for intentional
denials. Unexpected errors, panic and Goexit produce internal errors. Cancellation
waits for callback exit before releasing owned multipart files. Callbacks should
observe their context to finish promptly.

Both setters return independent endpoint values and replace their own prior hook.
An explicitly nil callback fails registration. Without hooks, existing behavior
is unchanged. Configure hooks before applying signing or resource-binding adapters.

## Concrete guard actors

```go
secured := http.RequireAuthentication(endpoint, transport, accountGuard).
    WithPreparation(prepare).
    WithAuthorization(func(ctx context.Context, actor Account, input Request) error {
        return accountPolicy(ctx, actor, input.Body)
    })
```

The route must declare Guarded access. A named guard chooses its concrete model
at assembly; no context cast is needed. `http.OptionalAuthentication` instead
passes `value.Optional[Account]` to its authorization hook and handler. Missing
credentials may be anonymous; invalid supplied credentials fail before decoding.
Authentication is not loaded a second time for authorization. A resource-specific
policy belongs after resource binding, where the loaded resource is available.

## Generated clients

Manifest formats 3 and later record form fields and the optional preparation stage. OpenAPI
uses the exact form media type, explicit scalar/repeated properties and
`x-foundry-request-preparation`. Regenerate older manifests and SDKs together.
The TypeScript SDK uses URLSearchParams escaping, retains repetition and bounds
wire bytes/pairs before transport. It does not stringify nested objects.

For endpoints with preparation, `validateRequest` still checks structural codecs
but reports `complete: false` with `request_preparation` skipped. Server validation
is authoritative after normalization; a client cannot reproduce an arbitrary Go
callback. Form and query scalar budgets are independent of the JSON body budget.

Runtime, generator, independent consumer, strict TypeScript, compiler/editor,
fuzz and race checks form the T03 acceptance gate. The [acceptance evidence](../evidence/typed-api-t03.json) records commands,
source hashes, outcomes and measured hook overhead.
