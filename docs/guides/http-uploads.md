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
form := foundryhttp.MultipartDirectory(ProfileInputDescriptor(), uploadDirectory)

endpoint := foundryhttp.ValidateBody(foundryhttp.DefineEndpoint(
    route,
    foundryhttp.EmptyQuery(),
    foundryhttp.MultipartBody(form),
    foundryhttp.JSONResponse(201, UploadReplyJSON()),
), 
    validation.Rules(fields.Attachment,
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
persistent storage before returning. Use the [storage upload adapter](storage.md) to persist the request-owned file through an explicitly selected disk.

MultipartDirectory accepts an absolute server-owned directory; an empty value
uses the operating system's temporary directory. Declaration/route assembly
performs no filesystem I/O. Foundry creates a private per-request directory
lazily when the first file arrives. Cleanup removes owned entries and reports
unexpected filesystem failures without recursively deleting unrelated entries.

## Resource limits

Use DefaultEndpointLimits and modify its Multipart limits for the route. The
HTTP kernel's `MaxBodyBytes` remains an additional ceiling; to accept files larger
than that server-wide default on one upload route only, declare the route's own
ceiling with `BodyLimit` and a matching `Timeout` for slow transfers:

```go
limits := foundryhttp.DefaultEndpointLimits()
limits.Multipart.Bytes, limits.Multipart.FileBytes = 512<<20, 512<<20
upload := foundryhttp.ConfigureEndpoint(foundryhttp.ConfigureEndpoint(foundryhttp.ConfigureEndpoint(foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(form), foundryhttp.EmptyResponse(204)), foundryhttp.Limits(limits)), foundryhttp.BodyLimit(512 << 20)), foundryhttp.Timeout(10 * time.Minute))
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

## Match extension and content together

Use `FileKinds` when a file's extension must agree with its detected content.
Independent `FileExtensions` and `FileContentTypes` lists do not enforce that
relationship. For example, both lists can accept a PNG renamed to `.pdf` when
PNG and PDF are allowed.

```go
document := validation.Bail(
	validation.FileMinSize[foundryhttp.UploadedFile](1),
	validation.FileKinds[foundryhttp.UploadedFile](
		validation.PDFKind(),
		validation.TextKind(),
		validation.ZIPContainerKind("docx"),
	),
	validation.FileMaxSize[foundryhttp.UploadedFile](5 << 20),
)
```

Declare custom pairs with `validation.FileKind{Extension: "txt", ContentType:
"text/plain"}`. Extensions have no leading dot and match without regard to case.
MIME declarations must be exact types without parameters or wildcards. The rule
ignores parameters on detected input, such as `charset=utf-8`. Multiple kinds can
share an extension when they declare different detected types. Constructors copy
the declarations, so later changes to an application's slice do not change a rule.

The common declarations are `PDFKind`, `TextKind`, `CSVKind`, and
`ZIPContainerKind(extension)`. `CSVKind` recognizes plain text without parsing CSV.
`ZIPContainerKind("docx")`, `ZIPContainerKind("xlsx")`, and
`ZIPContainerKind("pptx")` recognize ZIP signatures under those extensions.
**They do not validate Office documents**, ZIP entries, or package structure.
Applications choose their allowed kinds and any further document validation.

## Check image kinds and dimensions with one inspection

Import `validation/imaging` as `imagingvalidation`. Use `KindsWithEngine` to share
one bounded read and container inspection between format and dimension checks:

```go
jpeg := imagingvalidation.JPEGKind()
jpeg.Extension = "jpeg"
imageRule := validation.Bail(
	validation.FileMinSize[foundryhttp.UploadedFile](1),
	validation.FileMaxSize[foundryhttp.UploadedFile](5 << 20),
	imagingvalidation.KindsWithEngine[foundryhttp.UploadedFile](engine,
		validation.DimensionConstraints{MaxWidth: 10000, MaxHeight: 10000},
		imagingvalidation.JPEGKind(), jpeg, imagingvalidation.PNGKind(),
		imagingvalidation.WebPKind(), imagingvalidation.GIFKind(),
		imagingvalidation.AVIFKind(),
	),
)
```

`Kinds(limits, constraints, kinds...)` uses the portable inspector.
`KindsWithEngine` borrows the application's engine and retains its backend,
resource limits, and admission controls. Neither rule closes the engine.
Both use the existing `Measurer.Inspect` path. `Measurer.Measure` also uses that
path, so existing dimension-only rules retain their behavior.

Each `imagingvalidation.Kind` declares a `validation.FileKind` and an
`imaging.Format`. The rule requires the extension, declared detected MIME, and
inspected format to match the same kind. JPEG, PNG, WebP, and GIF declarations
include detected MIME checks. `AVIFKind` leaves `ContentType` empty because the
bounded upload sniff cannot reliably identify AVIF. Its extension and inspected
format must still match. A custom image kind can also leave `ContentType` empty
when engine inspection supplies recognition. To retain an existing engine-only
image policy, clear `ContentType` on its selected kind declarations.

Supply all application dimension constraints in this call. Adding a separate
`DimensionsWithEngine` rule causes another inspection. Width and height use the
displayed orientation. Zero constraints omit application dimension bounds, while
engine input bytes, pixels, and other resource limits still apply.

Malformed, unsupported, oversized, or mismatched content rejects validation.
Open, read, and internal inspection failures remain execution errors. Container
inspection does not decode every pixel or prove that compressed pixels are valid.
Keep full decoding in the image-processing path when the application needs it.

## Retain upload messages and client hints

`FileKinds` and the combined image rule are server-only leaves. Wrap them in
`WithMessage` and `WithTranslation` to retain the application's fallback and typed
translation arguments. For example, inside an application's image policy:

```go
matches := imagingvalidation.KindsWithEngine[F](engine, constraints, kinds...)
matches = validation.WithTranslation(
	validation.WithMessage(matches, "Must be an image file."),
	ImageArgsMessage(), ImageArgs{Extensions: extensionsText},
)
```

The combined image leaf uses one message for kind and dimension rejection and
reports `foundry.image_kinds`. `FileKinds` reports `foundry.file_kinds`. Existing
standalone rules retain their codes. The combined leaf's parameters retain both
kind declarations and dimension constraints. Review any application logic that
matches old custom issue codes or expects a separate dimension message.

To preserve separate kind and dimension messages, use `WithImageInspection`.
Its callback declares the rule tree once. Both leaves read the same observation
through the existing validation slot mechanism:

```go
checked := validation.WithImageInspection[F](
	imagingvalidation.NewEngineMeasurer[F](engine),
	func(image validation.ImageRules[F, imaging.Format]) validation.Rule[F] {
		matches := validation.WithTranslation(
			validation.WithMessage(image.Kinds(kinds...), "Must be an image file."),
			ImageArgsMessage(), ImageArgs{Extensions: extensionsText},
		)
		return validation.Bail(matches, image.Dimensions(constraints))
	},
)
return validation.Bail(
	validation.FileMinSize[F](1), validation.FileMaxSize[F](maxBytes), checked,
)
```

This form retains the existing `foundry.image_dimensions` diagnostic and its
translation. Inspection occurs on entry to the scope, after the outer byte
checks. Keep scoped rules inside their `WithImageInspection` tree. Registration
rejects a leaf used outside that scope. Each check owns its observation, including
each element of a collection and each concurrent request.

The manifest derives browser extension hints from the same kind declarations,
including AVIF. Applications do not need a duplicate `FileExtensions` list to
keep upload-picker metadata. These hints do not authorize a browser to skip
server content checks. Regenerate the application's manifest and client outputs
after replacing its rule declarations.

Compose the new rules with `Optional` for omitted uploads, `Required` for required
parts, and `Each` for collections. A present zero-byte upload remains distinct
from an omitted value. Use `FileMinSize(1)` when empty uploads must reject.

The `foundry-files` skill should point to this guide's
[correlated file validation](#match-extension-and-content-together) and
[single-inspection image validation](#check-image-kinds-and-dimensions-with-one-inspection)
sections.
