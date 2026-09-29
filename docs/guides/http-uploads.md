# Typed multipart uploads

The runtime and generator
use handwritten Go request structs as their source of truth. Run the ordinary
Foundry generator to produce the descriptor and typed validation fields.

```go
//foundry:multipart
type ProfileInput struct {
    Title      value.Optional[string]
    Attachment foundryhttp.UploadedFile `form:"document"`
    Photos     []foundryhttp.UploadedFile
    Settings   value.Optional[Settings] `form:",json"`
}

type Settings struct {
    Caption string `json:"caption"`
    Note value.Optional[value.Nullable[string]] `json:"note,omitzero"`
}
```

The generated `ProfileInputDescriptor()` returns
`foundryhttp.Multipart[ProfileInput]`. `ProfileInputValidationFields()` gives
compiler-checked field selectors with the same wire names. There is no handwritten
form-field map. `form:"-"` skips a field. Exported, non-embedded fields default to
snake-case names; an explicit form tag sets their exact case-sensitive names.

Text fields reuse ordinary query scalar codecs, including named numbers, booleans,
model IDs, enums, decimals, temporal values and custom text codecs. Literal `+`
and `%20` remain text; multipart fields are not URL-unescaped. Optional fields
preserve omission. Ordinary scalar slices use repeated same-named parts, keeping
request order. A named slice's own complete text codec takes precedence over
implicit repetition, as it does for query inputs.

UploadedFile fields require one file part. Optional UploadedFile fields can be
omitted. File slices accept repeated parts. A browser submits an HTML file input
with no selected file as a part with `filename=""` and no bytes; Optional and
repeated file fields treat exactly that part as absent (a slice of only blank
inputs stays nil), while a required field still receives it as an empty upload.
A named zero-byte file, or an unnamed part with content, remains a supplied
upload. `FileMinSize(1)` enforces nonempty contents. Pointers to files and
optional file collections are rejected by generation; these would make presence
or part cardinality ambiguous.

Use `form:"details,json"` for one structured JSON part. This includes a whole JSON
array when the Go value is a slice. Use `form:"records,json,repeat"` for repeated
JSON parts, each representing one slice element. JSON nullability follows the
explicit value type; omission still uses Optional. JSON parts must declare an
application/json content type. Their contracts reuse the same native shape,
custom codec, enum and key discovery as [JSON DTOs](http-dtos.md). Persistence
models and uploaded files do not implicitly become JSON objects.

## Endpoint and validation

Compose `MultipartBody(ProfileInputDescriptor())` into an ordinary typed endpoint.
The handler receives `request.Body` as ProfileInput and returns its separately
declared response DTO. The [independent consumer](../../tests/fixtures/consumer/httpuploads/forms.go)
illustrates descriptor assembly, validation and an injected domain service.

```go
fields := ProfileInputValidationFields()
form := ProfileInputDescriptor().WithTempDirectory(uploadDirectory)

endpoint := foundryhttp.DefineEndpoint(
    route,
    foundryhttp.EmptyQuery(),
    foundryhttp.MultipartBody(form),
    foundryhttp.JSONResponse(201, UploadReplyJSON()),
).WithBodyValidation(
    fields.Attachment.Rules(
        validation.FileMinSize[foundryhttp.UploadedFile](1),
        validation.FileMaxSize[foundryhttp.UploadedFile](1 << 20),
        validation.FileContentTypes[foundryhttp.UploadedFile]("image/*"),
        validation.FileExtensions[foundryhttp.UploadedFile]("jpg", "png"),
    ),
)
```

`FileValue` is the focused metadata interface used by these rules. FilePresent
checks for an actual handle; byte-size rules compare int64 counts exactly.
FileContentTypes uses detected MIME type, stripping MIME parameters for matching,
and supports major-type wildcards. FileExtensions checks the normalized final
filename extension without a leading dot. Extension matching does not validate
contents. The bounded HTTP sniff does not prove that an image or document is
valid; image decoding and dimensions belong to the imaging adapter.

Compose these rules with existing Optional, Required and Each adapters. Empty
uploads retain presence even when a size rule rejects them. File rule metadata
is marked server-only because client-declared types and arbitrary domain adapters
cannot promise the server detector's behavior. Validation failures return 422;
malformed fields return 400 and transport resource limits return 413. Infrastructure
and callback failures remain server failures. Public issues use declared form
paths, such as `/body/document`, without echoing uploaded values or filenames.

## Streaming and lifetime

`UploadedFile.Open(ctx)` returns an io.ReadSeekCloser positioned at byte zero.
Close each reader after use. The endpoint also closes retained readers and removes
its temporary files after response writing, including decoding, validation,
handler, cancellation and response-write failure paths. Metadata remains readable
after cleanup, while reopening or reading a retained handle fails.

Foundry stores incoming file bytes using a fixed scratch buffer. The consumer
receives no temporary filesystem path. Open uses a confined root; display names
never become local paths. UploadedFile cannot be implicitly JSON-serialized or
retained for a job after the request ends. A domain operation must stream it to
persistent storage before returning. Storage adapters arrive in milestone 11.

WithTempDirectory accepts an absolute server-owned directory; an empty value
uses the operating system's temporary directory. Declaration/route assembly
performs no filesystem I/O. Foundry creates a private per-request directory
lazily when the first file arrives. Cleanup removes owned entries and reports
unexpected filesystem failures without recursively deleting unrelated entries.

## Resource limits

Use DefaultEndpointLimits and modify its Multipart limits for the route. The
HTTP kernel's `MaxBodyBytes` remains an additional ceiling; to accept files larger
than that server-wide default on one upload route only, declare the route's own
ceiling with `WithBodyLimit` and a matching `WithTimeout` for slow transfers:

```go
limits := foundryhttp.DefaultEndpointLimits()
limits.Multipart.Bytes, limits.Multipart.FileBytes = 512<<20, 512<<20
upload := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(form), foundryhttp.EmptyResponse(204)).
    WithLimits(limits).WithBodyLimit(512 << 20).WithTimeout(10 * time.Minute)
```

Other routes keep the kernel default. Multipart Bytes
counts complete encoded input, including MIME framing and legal epilogues;
FileBytes, Files and Readers bound captured files and open readers. FieldBytes
and FieldsBytes bound buffered text/JSON parts individually and together. Parts
and Issues bound parsing work and diagnostics. JSON parts additionally honor the
ordinary body JSON complexity limits.

HeaderBytes bounds accepted normalized part headers after native MIME parsing.
The standard parser owns its separate pre-allocation header limits; HeaderBytes
is not advertised as an earlier allocation bound. Header and field limits do not
change how file-content bytes stream to disk. Metadata copies avoid retaining a
large MIME header through a short filename or declared media-type substring.

Incoming Content-Type is untrusted metadata available as ClientContentType().
ContentType() reports the bounded content sniff. Neither declares cross-system
atomicity or durable delivery; attachment/database compensation belongs to the
storage and model-extension integration.
