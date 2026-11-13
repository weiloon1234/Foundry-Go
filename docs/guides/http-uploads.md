# Typed multipart uploads

<a id="agent-entry"></a>
## Agent entry

- [Declare upload inputs](#recipe-multipart-input)
- [Compose an upload endpoint](#recipe-multipart-endpoint)
- [Match extension and content](#recipe-file-kinds)
- [Validate image kinds and dimensions](#recipe-image-kinds)
- [Keep separate image messages](#recipe-image-messages)

<a id="recipe-multipart-input"></a>
## Declare multipart input

The runtime and generator
use handwritten Go request structs as their source of truth. Run the ordinary
Foundry generator to produce the descriptor and typed validation fields.

<!-- compiled-source: tests/fixtures/consumer/httpuploads/forms.go#multipart-input -->
```go
type Gallery []foundryhttp.UploadedFile

type Settings struct {
	Caption string                                 `json:"caption"`
	Note    value.Optional[value.Nullable[string]] `json:"note,omitzero"`
}

//foundry:multipart
type ProfileInput struct {
	Title      value.Optional[string]
	Attachment foundryhttp.UploadedFile `form:"document"`
	Photos     Gallery
	Tags       []string                 `form:"tags[]"`
	Settings   value.Optional[Settings] `form:",json"`
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

<a id="recipe-multipart-endpoint"></a>
## Endpoint and validation

Compose `MultipartBody(ProfileInputDescriptor())` into an ordinary typed endpoint.
The handler receives `request.Body` as ProfileInput and returns its separately
declared response DTO. The [independent consumer](../../tests/fixtures/consumer/httpuploads/forms.go)
illustrates descriptor assembly, validation and an injected domain service.

<!-- compiled-source: tests/fixtures/consumer/httpuploads/forms.go#multipart-endpoint -->
```go
func Router(directory string, service Service) (*foundryhttp.Router, error) {
	form := foundryhttp.MultipartDirectory(ProfileInputDescriptor(), directory)
	fields := ProfileInputValidationFields()
	rules := validation.All(
		validation.Rules(fields.Title, validation.Optional(validation.MaxLength[string](80))),
		validation.Rules(fields.Attachment,
			validation.FileMinSize[foundryhttp.UploadedFile](1),
			validation.FileMaxSize[foundryhttp.UploadedFile](1<<20),
			validation.FileContentTypes[foundryhttp.UploadedFile]("text/plain", "image/png"),
			validation.FileExtensions[foundryhttp.UploadedFile]("txt", "png"),
		),
		validation.Rules(fields.Photos, validation.Each[Gallery](validation.FileMaxSize[foundryhttp.UploadedFile](1<<20))),
	)
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "uploads.profile", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/profile"))
	endpoint := foundryhttp.ValidateBody(foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(form), foundryhttp.JSONResponse(201, UploadReplyJSON())), rules)
	return foundryhttp.NewRouter(endpoint.Handle(func(ctx context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, ProfileInput]) (UploadReply, error) {
		return service.Save(ctx, input.Body)
	}))
}
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

A larger per-request ceiling also needs [admission before capture](#admit-large-multipart-requests-before-capture) and independent disk capacity.

Use DefaultEndpointLimits and modify its Multipart limits for the route. The
HTTP kernel's `MaxBodyBytes` remains an additional ceiling; to accept files larger
than that server-wide default on one upload route only, declare the route's own
ceiling with `BodyLimit` and a matching `Timeout` for slow transfers:

```go
limits := foundryhttp.DefaultEndpointLimits()
limits.Multipart.Bytes, limits.Multipart.FileBytes = 520<<20, 512<<20
upload := foundryhttp.ConfigureEndpoint(foundryhttp.ConfigureEndpoint(foundryhttp.ConfigureEndpoint(foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(form), foundryhttp.EmptyResponse(204)), foundryhttp.Limits(limits)), foundryhttp.BodyLimit(520 << 20)), foundryhttp.Timeout(10 * time.Minute))
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
ContentType() reports the framework's 512-byte content sniff, sharing ISO base
media brand detection with attachment publication. A complete leading `ftyp`
box must fit in that prefix; truncated boxes cannot establish a media brand.
Client MIME hints do not select the detected upload type. Neither declares cross-system
atomicity or durable delivery; attachment/database compensation belongs to the
storage and model-extension integration.

<a id="recipe-file-kinds"></a>
## Match extension and content together

Use `FileKinds` when a file's extension must agree with its detected content.
Independent `FileExtensions` and `FileContentTypes` lists do not enforce that
relationship. For example, both lists can accept a PNG renamed to `.pdf` when
PNG and PDF are allowed.

<!-- compiled-source: tests/fixtures/consumer/profiles/upload_rules.go#document-kinds -->
```go
func DocumentRule() validation.Rule[foundryhttp.UploadedFile] {
	return validation.Bail(
		validation.FileMinSize[foundryhttp.UploadedFile](1),
		validation.FileKinds[foundryhttp.UploadedFile](validation.PDFKind(), validation.TextKind(), validation.ZIPContainerKind("docx")),
		validation.FileMaxSize[foundryhttp.UploadedFile](5<<20),
	)
}
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

For iPhone videos, declare `validation.FileKind{Extension: "mov", ContentType:
"video/quicktime"}` alongside `validation.FileKind{Extension: "mp4", ContentType:
"video/mp4"}` in `FileKinds`. The same ISO brand detector also recognizes
AVIF, HEIC/HEIF, M4A and 3GPP. Recognition checks a bounded signature, not the
validity of the complete media file.

Upload detection preserves generic ZIP and text/XML types, including charset
parameters. Attachment publication additionally specializes Office/ODF/EPUB
containers and SVG, and may use a compatible textual client hint when its policy
accepts that type. Those specializations do not change multipart `FileKinds`:
Office uploads still use `ZIPContainerKind`, and CSV uploads still use `CSVKind`.

<a id="recipe-image-kinds"></a>
## Check image kinds and dimensions with one inspection

Import `validation/imaging` as `imagingvalidation`. Use `KindsWithEngine` to share
one bounded read and container inspection between format and dimension checks:

<!-- compiled-source: tests/fixtures/consumer/profiles/upload_rules.go#image-kinds -->
```go
func ImageRule(engine *imaging.Engine) validation.Rule[foundryhttp.UploadedFile] {
	jpeg := imagingvalidation.JPEGKind()
	jpeg.Extension = "jpeg"
	return validation.Bail(
		validation.FileMinSize[foundryhttp.UploadedFile](1),
		validation.FileMaxSize[foundryhttp.UploadedFile](5<<20),
		imagingvalidation.KindsWithEngine[foundryhttp.UploadedFile](engine,
			validation.DimensionConstraints{MaxWidth: 10000, MaxHeight: 10000},
			imagingvalidation.JPEGKind(), jpeg, imagingvalidation.PNGKind(),
			imagingvalidation.WebPKind(), imagingvalidation.GIFKind(), imagingvalidation.AVIFKind()),
	)
}
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

<a id="recipe-image-messages"></a>
## Retain upload messages and client hints

`FileKinds` and the combined image rule are server-only leaves. Wrap them in
`WithMessage` and `WithTranslation` to retain the application's fallback and typed
translation arguments.

See [typed validation messages](validation-messages.md) for the message
declaration and translation arguments.

The combined image leaf uses one message for kind and dimension rejection and
reports `foundry.image_kinds`. `FileKinds` reports `foundry.file_kinds`. Existing
standalone rules retain their codes. The combined leaf's parameters retain both
kind declarations and dimension constraints. Review any application logic that
matches old custom issue codes or expects a separate dimension message.

To preserve separate kind and dimension messages, use `WithImageInspection`.
Its callback declares the rule tree once. Both leaves read the same observation
through the existing validation slot mechanism:

<!-- compiled-source: tests/fixtures/consumer/profiles/upload_rules.go#image-inspection-messages -->
```go
func ImageRuleWithMessages(engine *imaging.Engine) validation.Rule[foundryhttp.UploadedFile] {
	checked := validation.WithImageInspection[foundryhttp.UploadedFile](
		imagingvalidation.NewEngineMeasurer[foundryhttp.UploadedFile](engine),
		func(image validation.ImageRules[foundryhttp.UploadedFile, imaging.Format]) validation.Rule[foundryhttp.UploadedFile] {
			matches := validation.WithMessage(image.Kinds(imagingvalidation.PNGKind()), "Must be a PNG image.")
			dimensions := validation.WithMessage(image.Dimensions(validation.DimensionConstraints{MaxWidth: 10000, MaxHeight: 10000}), "Image dimensions are too large.")
			return validation.Bail(matches, dimensions)
		},
	)
	return validation.Bail(validation.FileMinSize[foundryhttp.UploadedFile](1), validation.FileMaxSize[foundryhttp.UploadedFile](5<<20), checked)
}
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

Compose the new rules with `validation.Optional` for omitted uploads,
`validation.Required` for required parts, and `validation.Each` for collections. A
present zero-byte upload remains distinct from an omitted value. Use
`validation.FileMinSize(1)` when empty uploads must reject.

The `foundry-files` skill should point to this guide's
[correlated file validation](#recipe-file-kinds) and
[single-inspection image validation](#recipe-image-kinds)
sections.

## Admit large multipart requests before capture

Use `NewUploadPool` and `MultipartAdmission` for a route that accepts large files.
The registered descriptor selects its pool. Content-Length never selects a pool
or reduces its reservation. Each request reserves one slot and the full
`EndpointLimits.Multipart.Bytes` allowance, including chunked requests.
`BodyLimit` must equal that encoded allowance, or endpoint registration fails.

`NewUploadPool` performs startup filesystem I/O. Call it before constructing
routes, retain the pool for the server's lifetime, and close it after HTTP drain.
`MultipartAdmission` applies `MultipartDirectory` with the pool's directory.
Changing that directory afterward fails validation. Existing descriptors without
admission retain their behavior.

The [independent upload consumer](../../tests/fixtures/consumer/httpuploads/large.go)
uses `http.UploadSettings`, available as `application.Settings.HTTP.Uploads`.
Load those settings through the generated configuration schema. The deployment
pipeline owns their values. Pass `settings.App.HTTP.Uploads` to `LargeRouter` in
that example. Its service receives an ordinary request-owned `UploadedFile`.
Production applications use their existing guarded endpoint adapter for the same
multipart declaration.

Admission precedes route middleware, authentication, decoding, and capture.
`ApplyMiddleware` preserves this ordering around a Router. The server performs
admission before global middleware and excludes admitted upload requests from
its general handler-slot pool. Exhaustion returns 503 with `Retry-After: 1` and
uses unread-body rejection. There is no upload admission queue. Direct requests
that bypass nginx encounter the same reservation.

Use the framework's Router and `ApplyMiddleware` chain for this guarantee.
An external native wrapper that reads a body before entering that chain cannot
be protected by downstream admission. HTTP capture prefixes overlapping an
admitted upload route fail server preparation. Middleware cannot change an
already admitted request to a different upload pool or body budget. If a native
wrapper hides route metadata from the kernel, the upload route refuses late
admission. Use `ApplyMiddleware` to preserve that metadata.

Reservations survive capture, validation, response writing, and slow publication.
Cancellation does not release a slot while a handler or capture still runs.
The actual exit owner closes captured readers and removes exact owned files.
A cleanup failure releases the active slot but conservatively retains the entire
encoded-byte reservation. `Usage` includes those bytes. `RetryCleanup` retries
only the tracked batch's owned names and releases bytes after removal succeeds.
It never scans arbitrary files or recursively deletes unexpected entries.

Give large and small capture separate writable, capacity-controlled volumes.
Keep small requests outside the large pool. For small routes needing an explicit
byte budget, bind a separate `UploadPool` on their independent volume. Reserve
small attachment and adapter capacity in those owners too. HTTP isolation alone
does not reserve downstream publication capacity.

A pool directory must be empty on startup. Its ownership marker prevents another
pool from treating active or retained files as free capacity. After a crash,
verify that every old borrower is dead before reconciling that exact directory.
Never remove a marker merely because a timeout elapsed. A second process or
rolling generation needs its own explicitly budgeted directory and capacity.
Do not share one request directory between live HTTP processes.

For a 512 MiB file, the proposed encoded allowance is 520 MiB. Use it only when
all declared fields, part headers, and framing fit within the remaining 8 MiB.
Keep part, file, reader, and field limits small. Four reservations then hold at
most 2,080 MiB of encoded capacity. The example deployment adds filesystem and
cleanup headroom with a 3 GiB request volume. These are configuration examples,
not measured capacity guarantees.

`Timeout` is a total route deadline from kernel admission. It includes capture,
publication, and response work, and extends native connection deadlines for a
longer route. Nginx idle timers measure gaps in progress, not total duration.
Use the [upload deployment profile](deployment.md#large-upload-ingress) to keep
those settings and the writable sandbox paths consistent.
