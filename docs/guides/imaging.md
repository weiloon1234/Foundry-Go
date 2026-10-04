# Imaging

Milestone 18 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-18-verification-and-consumer-review)
records the checks and operational limits.
The [imaging expansion](../../blueprint/18-imaging-and-model-extensions.md#imaging-expansion--2026-10-03)
also passed its final portable/native, consumer and framework verification gates.
The [runtime-loading follow-up](../evidence/imaging-runtime-loading-20261004.json)
passed ordinary native builds, missing-library consumers and the full gate.

An image plan owns transformation policy and can be reused by an attachment
collection. In an ordinary configured application, enable the built-in engine:

```go
settings := application.DefaultSettings()
settings.Image.Enabled = true
// Optional per-application bounds and concurrency:
settings.Image.Config.MaxActive = 2

app, err := application.New(settings).HTTP(func(services application.Services) ([]http.RouteRegistration, error) {
    engine, err := services.Image()
    if err != nil { return nil, err }
    return profileRoutes(engine) // Your constructor receives the concrete engine.
}).Build(ctx)
```

`Services.Image()` returns that application's engine. The application closes it
after draining its borrowers; handlers should not close it themselves. A disabled
image service returns an explicit error. Consumers import Foundry packages; codec
libraries and processing infrastructure stay inside the framework. The executable
[configured upload route](../../tests/fixtures/consumer/bootstrap/routes.go) reads
a typed upload, enlarges/pads its image and saves the encoded result. Its
[HTTP integration test](../../tests/fixtures/consumer/bootstrap/bootstrap_test.go)
checks the persisted pixels and application-owned engine shutdown.

For standalone tools, construct an engine with
`imaging.New(imaging.DefaultConfig())` and close it when finished. `imaging.Module`
remains available for advanced foundation provider composition. Both paths use
the same immutable plans:

```go
plan := imaging.NewPlan().
    Fill(1200, 630, true).
    Format(imaging.WebP)
result, err := engine.Process(ctx, uploadReader, plan)
```

`Process` borrows the reader and never closes it. It rejects invalid byte counts
and stops after 100 consecutive empty reads with `io.ErrNoProgress`.
`ProcessBytes` provides the same
bounded pipeline directly on a byte slice, without copying it first. The input
must not be changed during the call.
`Result.Reader` provides read-only encoded output, `Bytes` makes an independent
copy, and `Info` reports the resulting format and dimensions. A failure returns
no partial result. Images do not serialize implicitly to JSON.

Plans are immutable values, safe to reuse across concurrent calls. Transform
order matters. Routine/debug plan formatting redacts embedded layer bytes.
Sizing operations are:

| Operation | Behavior |
| --- | --- |
| `Resize(w, h)` | Exact dimensions, including enlargement; may distort aspect ratio |
| `ResizeDown(w, h)` | Exact resize capped independently at each original axis |
| `Fit(w, h, upscale)` | Proportional fit inside a bounding box |
| `FitWidth(w, upscale)`, `FitHeight(h, upscale)` | Proportional resize targeting one axis |
| `Fill(w, h, upscale)`, `FillAt(w, h, upscale, position)` | Proportional resize to cover, then crop; `Fill` centers |
| `Crop(x, y, w, h)`, `CropAt(w, h, position)` | Extract pixels without resampling; rectangle must fit inside the image |
| `Pad(w, h, background, position)` | Fit without enlargement and pad to an exact canvas |
| `Contain(w, h, background, position)` | Fit with enlargement and pad to an exact canvas |
| `Canvas(w, h, background, position)` | Crop or expand canvas without resampling |
| `CanvasRelative(dw, dh, background, position)` | Add signed canvas deltas without resampling |

Fit and fill take an explicit upscale choice. A fill that would need forbidden
upscaling fails; a fit keeps the smaller original size. `Position` is one of
`Center` (zero/default), `TopLeft`, `Top`, `TopRight`, `Left`, `Right`,
`BottomLeft`, `Bottom`, `BottomRight`. Canvas and padding backgrounds use
`color.NRGBA` and support transparency.

`Resampling` selects `Lanczos` (default), `Cubic`, `Linear`, `Box`, or
`NearestNeighbor` for every resize in the plan, regardless of where that option
appears in the chain. These are interpolation filters, not AI super-resolution.
Use nearest-neighbor for pixel art. `Rotate` retains its exact clockwise
90/180/270-degree contract. `RotateDegrees(degrees, background)` accepts finite
angles in [-360,360], expands the canvas and uses cubic interpolation, with exact
quarter-turns retaining pixels. Horizontal/vertical flips are also available.

Effects include Gaussian `Blur`, `Grayscale`, channel-offset `Brightness`,
`Contrast`, `Invert`, `Gamma`, `Saturation`, `Hue`, `Sepia`, `Threshold`,
`Pixelate` and `Sharpen`. Gamma accepts [0.01,100]; values above one lighten.
Saturation/contrast use [-100,100], hue [-360,360] degrees, sepia/threshold
[0,100] percent. `Sharpen(sigma, amount, threshold)` uses an unsharp mask with
sigma in (0,100], amount in [0,10], threshold in [0,1]; `(1,1,0.02)` is a typical
starting point. Sharpening increases edge contrast; it does not recover detail.

## Create images, insert layers and apply masks

`engine.Create(ctx, width, height, background, plan)` starts with a solid canvas
under the same limits and lifecycle as `Process`. Its default output is PNG.
`Process` still accepts any `io.Reader`; paths/storage/uploads are opened by their
existing owners and supplied as readers.

```go
watermark, err := engine.Process(ctx, watermarkReader,
    imaging.NewPlan().Fit(160, 80, false).Format(imaging.PNG))
if err != nil {
    return err
}
plan := imaging.NewPlan().
    Contain(1200, 630, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, imaging.Center).
    Insert(watermark, imaging.Placement{
        Position: imaging.BottomRight, X: -16, Y: -16,
        Opacity: value.Set(0.75),
    }).Format(imaging.WebP)
result, err := engine.Process(ctx, photoReader, plan)
```

An inserted layer is an immutable `Result`, prepared once and reusable in direct
processing and attachment variant declarations. It must be a single frame in a
readable format (PNG is a convenient lossless choice). Layer bytes are private;
mutating a copy returned by `Bytes` never changes the plan. Insertions clip at the
canvas boundary. Positive X/Y offsets always move right/down; use negative
offsets to inset from right/bottom. Omitted opacity is 1, while `value.Set(0.0)`
makes the layer transparent.

`Placement.Blend` supports `BlendNormal` (default), `BlendMultiply`,
`BlendScreen`, `BlendOverlay`, `BlendDarken`, `BlendLighten`, `BlendDifference`,
and `BlendExclusion`. Modes combine straight RGB and then apply source-over alpha;
transparent backdrop pixels do not darken the source in multiply mode.

`Mask(result, placement, imaging.AlphaMask)` multiplies existing alpha by the
mask's alpha. `LuminanceMask` additionally multiplies by weighted RGB luminance.
Outside the positioned mask, pixels become transparent. Mask opacity is supported;
a non-normal blend mode rejects. Masking does not change RGB or canvas dimensions.

## Text and vector drawing

Load an application font once with `BuiltinFont(FontRegular)` (also `FontBold`,
`FontItalic`, `FontBoldItalic`, `FontMono`) or `ParseFont(ttfOrOtfBytes)`. Fonts
own a copy of their bytes, redact formatting and support concurrent plan reuse.
Custom fonts are application assets, not arbitrary uploaded fonts. Parsing is
limited to 8 MiB; collections, WOFF and bitmap/color glyph rendering are not
supported. Built-in fonts need no files, downloads or additional dependencies.

```go
font, err := imaging.BuiltinFont(imaging.FontBold)
if err != nil {
    return err
}
plan := imaging.NewPlan().
    Contain(320, 160, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, imaging.Center).
    Rectangle(8, 100, 304, 52, imaging.ShapeStyle{
        Fill: color.NRGBA{R: 20, G: 40, B: 70, A: 255},
    }).
    Text("Foundry profile", imaging.TextOptions{
        Font: font, Size: 24,
        Color: color.NRGBA{R: 255, G: 255, B: 255, A: 255},
        Position: imaging.Bottom, Y: -20,
        MaxWidth: 280, Align: imaging.AlignCenter,
    }).Format(imaging.PNG)
```

Text is antialiased with alpha colors, fractional pixel sizes (1..512 pixels per
em) and kerning. `Position` anchors a block of line boxes; X/Y offsets follow the
same direction as image insertion. `MaxWidth` sets the line box width and wraps
at spaces, splitting long words as needed. Zero disables wrapping. An individual
glyph wider than a nonzero `MaxWidth` rejects. `AlignLeft` (default), `AlignCenter`
and `AlignRight` align lines inside that width, or inside the longest unwrapped
line. Explicit newlines and blank lines are preserved, CRLF is normalized and
tabs become four spaces. `LineHeight` is an em multiplier (0.5..4); zero selects
1.2. Text is limited to 4096 Unicode code points, including expanded tabs.
Line-box dimensions also obey the engine's width/height/pixel limits.

Missing glyphs reject by default; `MissingGlyph: imaging.ReplaceMissingGlyph`
explicitly chooses the font's replacement glyph. Portable text is left-to-right
outline layout. It does not perform bidirectional layout, complex script shaping,
font fallback or color emoji. Use a font that covers the required characters.

`Rectangle(x,y,w,h,style)`, `Ellipse(cx,cy,rx,ry,style)`, `Circle(cx,cy,r,style)`,
`Polygon([]imaging.Point,style)` and `Line(x1,y1,x2,y2,width,color)` use floating
pixel coordinates. `ShapeStyle` supplies `Fill`, `Stroke` and `StrokeWidth`.
Transparent paint or a zero stroke width disables that paint; fill is applied
before stroke. Strokes are centered with round joins and caps, and overlapping
stroke segments do not accumulate opacity. Drawing clips at the canvas edge.

For arbitrary paths, `NewPath().MoveTo(...).LineTo(...).QuadraticTo(...).
CubicTo(...).Close()` builds an immutable path, passed to `plan.Draw(path,style)`.
Each contour starts with `MoveTo`; start another contour after `Close`. Filling
implicitly closes open contours and uses nonzero winding; reverse an inner
contour to make a hole. Stroke closure requires `Close`. Polygon points are copied.
Coordinates must be finite within ±65535 and strokes within 0..4096 pixels.
Paths allow 1024 commands; stroking subdivides curves with a bounded vertex budget.

Fonts, text, paths and layers count toward aggregate input limits. Drawing admits
coverage/rasterization buffers, and text reserves 8 MiB for outline/layout scratch
before processing; these are conservative workspace allowances, not process heap
quotas. Text and drawing share the same plan, cancellation ownership and attachment
variant APIs as image resizing and insertion. The executable example is
[profile labels](../../tests/fixtures/consumer/profiles/image_labels.go).

## Animation and capabilities

Animation rejects by default. `Frames(imaging.FirstFrame)` explicitly takes one
image; for APNG it retains the PNG default image, which may be a separate poster.
`Frames(imaging.PreserveAnimation)` preserves GIF/APNG/WebP/AVIF frames, timing and looping,
composites source frame rectangles with their disposal/blending rules, then applies
the entire plan to each displayed canvas. Insertions, text and shapes work on every
frame. Output must be GIF, PNG (APNG) or WebP; unsupported outputs reject instead of
silently discarding animation. TIFF pages are not animations. Static inputs stay
static even with the preserve policy.

```go
plan := imaging.NewPlan().Frames(imaging.PreserveAnimation).
    Fit(320, 320, false).Format(imaging.PNG)
result, err := engine.Process(ctx, uploadReader, plan)

capability, known := engine.Capabilities().ForFormat(imaging.PNG)
// capability.Read, Write, ReadAnimation, WriteAnimation are separate flags.
```

Capabilities are owned snapshots and report the engine's actual implemented
features. The portable backend reports no color management, metadata preservation
or smart cropping. Native snapshots reflect the loaded libvips operations; native
writers are probed with a small image, including their actual codec availability
and a separate ICC round trip before advertising `WriteMetadata`.
GIF output retains exact colors when the animation fits one 256-entry palette,
otherwise uses dithering, with binary transparency and centisecond timing;
conversion to GIF rounds frame delays to the nearest centisecond. Loop counts or
delays that GIF cannot represent reject. PNG animation retains fractional delays
and 8-bit RGBA canvases. APNG's optional separate poster is not an animation frame;
preservation emits its actual first animation frame as the output default image.
The `.apng` extension parses as `imaging.PNG`, with the PNG media type.
APNG reduces timing fractions when needed to fit its 16-bit fields; fractions
that still cannot be represented fail explicitly.

WebP output defaults to exact lossless 8-bit RGBA and also supports lossy color.
Both modes round delays to the
nearest millisecond. It supports durations through 16,777,215 ms and total play
counts through 65,535 (zero repeats forever); larger values reject. Its maximum
encoded width/height is 16,384 for lossless and 16,383 for lossy. The reader supports lossy/lossless frame bitstreams,
alpha, offsets, blending and disposal. It uses the declared animation background
for the initial canvas and disposal; some viewers ignore that background hint.
`FirstFrame` renders the first frame on the full canvas. Lossy input converts BT.601 video-range samples with centered bilinear
chroma interpolation. Compositing uses encoded RGB channels without ICC
conversion, consistent with the portable renderer.
Output uses complete replacement canvases so transparency never leaves stale
pixels from the preceding frame.

`Limits.Frames` bounds frame counts, and `Limits.Pixels` bounds the aggregate
source and output canvas pixels across frames. Retained frames, palette conversion,
disposal canvases and temporary encoded-frame buffers are admitted before decoding.
PNG controls, frame rectangles, sequence numbers and chunk CRCs are checked first.
WebP validates RIFF lengths/padding, frame rectangles and the coded VP8/VP8L
size of every frame before pixel decoding; its chunk count is bounded at 65,536,
including nested chunks. The WebP decode admission includes padded VP8 macroblocks; the first-frame
decode allowance also includes its logical canvas and bounded frame metadata.
Cancellation or a failed frame returns no partial animation. See the executable
[animated avatar](../../tests/fixtures/consumer/profiles/animated_images.go).

An automatically discovered libvips runtime adds the capabilities described below.

## Formats and metadata

| Format | Input | Output |
| --- | --- | --- |
| JPEG | Yes | Quality 85 by default; `JPEGQuality(1..100)` |
| PNG | Yes; APNG requires an explicit frame policy | Lossless PNG/APNG; default, uncompressed, fastest or best compression |
| WebP | Static/animated lossy or lossless; animation requires a frame policy | Static/animated lossless (default) or lossy with exact alpha |
| GIF | Yes; animation requires an explicit frame policy | Static/animated, dithered palette |
| BMP | Supported Windows BMP encodings | BMP |
| TIFF | Supported baseline encodings; multiple pages require `FirstFrame` | Deflate TIFF |
| ICO | Embedded PNG or uncompressed Windows DIB; largest representation | One PNG representation, at most 256×256 |
| AVIF | Still/sequence, 8/10/12-bit, alpha and grids; sequences require a frame policy | Still 8-bit 4:2:0 AVIF; default speed 10 and quality 60; configurable color/alpha quality and speed |

TIFF/ICO variants
unsupported by their underlying codec fail rather than being guessed. Filenames,
extensions and caller-provided MIME types never determine the input codec.

`JPEGQuality` on any non-JPEG output fails, including WebP; `AVIFQuality` requires
an explicit AVIF output. For lossy WebP, use
`.Format(imaging.WebP).WebPMode(imaging.WebPLossy).WebPQuality(80)`.
`WebPQuality(1..100)` defaults to 75 and requires explicit lossy mode; even 100
uses lossy 4:2:0 color. Alpha remains exact. `WebPMode(imaging.WebPLossless)`
(the default) preserves 8-bit RGB, including fully transparent pixels.
`WebPMethod(0..6)` selects encoding effort in either mode, default 4. Explicit
zero selects the fastest search. All three controls require explicit WebP output
and apply to every animation frame. Encoders run with one codec worker per engine
operation. The [consumer plans](../../tests/fixtures/consumer/profiles/image_encoding.go)
use these controls without importing a codec.
`PNGCompression` selects `PNGDefaultCompression`, `PNGNoCompression`,
`PNGBestSpeed` or `PNGBestCompression`. Every APNG frame uses the selected level;
pixels and timing stay unchanged. It requires `.Format(imaging.PNG)` explicitly,
even when selecting the default level. Uncompressed output still obeys byte limits.

`AVIFSpeed(0..10)` selects search effort: 0 is slowest/most thorough and 10 is
fastest (the default). Explicit zero is preserved. `AVIFAlphaQuality(1..100)`
sets alpha independently; otherwise alpha follows `AVIFQuality`. Both require
explicit AVIF output. Quality 100 uses the lowest quantizer, but RGB conversion
and 4:2:0 chroma subsampling still apply; it does not promise an exact RGB round
trip. Slower synchronous encoding retains engine capacity until it exits.

```go
pngPlan := imaging.NewPlan().Format(imaging.PNG).
    PNGCompression(imaging.PNGBestCompression)
avifPlan := imaging.NewPlan().Format(imaging.AVIF).
    AVIFQuality(65).AVIFSpeed(6).AVIFAlphaQuality(100)
```

See the executable [encoding plans](../../tests/fixtures/consumer/profiles/image_encoding.go).

`Background(color.NRGBA{...})` (opaque) flattens the result over a solid color
before encoding, so transparent pixels do not turn black in JPEG output; it
applies to every output format when set. GIF first-frame output
uses the logical canvas with transparent space outside the first frame. Normal
pixel transforms use 8-bit non-premultiplied RGBA. Untouched decoded images may
retain higher precision where the output codec supports it.

EXIF orientation is applied before user transforms by default; use
`Orientation(IgnoreOrientation)` explicitly to ignore it. AVIF clean-aperture
cropping, rotation and mirroring follow the same policy. Re-encoding strips
EXIF/GPS, XMP, comments and ICC metadata. This is not color-profile conversion.
The portable backend supports this stripping policy. The optional native backend
adds explicit ICC conversion and metadata preservation as described below.

`Inspect(bytes, limits)` reads container metadata without decoding pixel data.
Successful inspection alone does not prove compressed image contents are valid.
Extended WebP checks the coded VP8/VP8L dimensions against its VP8X canvas before
pixel allocation; conflicting headers are rejected.
The processing pipeline inspects first, validates every planned output and
intermediate resampling allocation and admits each phase's working memory, then
decodes and transforms. Brightness, contrast and grayscale operate on 8-bit
pixel rows (per-channel lookup tables), adjusting pipeline-owned images in place.

Limits cover input bytes, encoded output bytes, width, height, pixels, frames and
admitted working memory. All limits are positive; there is no unbounded mode.
The memory budget is an admission calculation for image buffers and codec work,
not an OS/process heap quota. Defaults allow up to 50 MiB input and
output, 12000-pixel dimensions, 25 million pixels, 64 container images and
640 MiB admitted work per operation; the tightest bound wins. A 32-transform
limit also applies. Container frame/page counts and aggregate image areas are
checked before decoding. Input bytes include the source and all declared layer
results, conservatively counting repeated layer references separately. Readers
stop after the remaining input allowance plus one overflow-detection byte. Layer
decoding is admitted with the current canvas retained, before any source pixels
are decoded. Resampling intermediates can be larger than either
input or final output and are included in the pixel/workspace checks.

`WorkingBytes` is checked per pipeline phase, because each phase replaces the
previous buffers: the retained encoded input plus the live pixel buffers of that
phase. Per-pixel costs were measured on the engine's codecs:

| Phase | Bytes per pixel |
| --- | --- |
| Decode | JPEG 20 (progressive coefficient blocks), WebP 12, PNG/TIFF 9 (16-bit decode), GIF 6, AVIF separately below, others 8 |
| Decoded image | 8 for PNG/TIFF/AVIF, otherwise 4 |
| Transform | input image + 4 per output pixel; resize/fit/blur add 8 per peak-canvas pixel, fill/padding add 12, sharpen adds 16 |
| Encode | final image + WebP 128 lossless or 192 lossy (padded to 16×16 blocks), plus 16 MiB fixed workspace, AVIF 6, ICO 8, others 2; plus the owned `OutputBytes` allowance and 4 more with `Background` |

Resampling weights and filter pixel rows/columns are admitted in addition to
canvas scratch. Their cost can dominate on very thin images; nearest-neighbor
does not allocate resampling weights.

AVIF admission counts coded frame headers (including hidden pictures), grid
canvases and selected sequence samples before calling the decoder. It reserves
128 bytes per largest permitted coded pixel, 16 per potentially retained decoded
pixel, gathered compressed bytes, three input-sized metadata/copy buffers and
2 MiB of metadata workspace. This conservative AV1 allowance can reject large
AVIF files before the general 25-megapixel limit. A representative alpha decode on Go
1.27.1, darwin/arm64 (Apple M4 Max), allocated about 1.60 MiB at 64×64 and
8.64 MiB at 1024×768; these are allocation measurements, not peak RSS or a
worst-case proof. Resize after decoding cannot
reduce the decoder's peak. Width/height and decoded sample counts must match
the inspected container. Metadata has independent bounds of 8,192 boxes/entries,
1,024 items and 16 tracks. `FirstFrame` for a sequence selects its first displayed
frame and still admits the sequence decoder's work. Arbitrary edit lists and
composition offsets are rejected. Zero-duration AVIF samples are rejected so a
codec fallback to a still image cannot masquerade as successful animation.
AVIF animation can be converted to GIF/APNG/WebP; AVIF encoding remains still-only.
The decoder applies matrix/range conversion; ICC conversion, HDR tone mapping
and gain-map reconstruction are not part of this portable path.

With the defaults, a 25-megapixel photo (the `Pixels` limit) is admitted for
decoding in the JPEG/PNG/WebP/GIF/BMP/TIFF/ICO codecs and for ordinary pipelines such as fit,
rotate, adjustments or JPEG/PNG/AVIF re-encoding; its JPEG decode phase is the
largest at about 500 MiB. A full-size 25-megapixel lossless WebP encode needs
an admission allowance above 3 GiB and is rejected unless `WorkingBytes` is raised; resize first.
Allow additional process memory for codec pools and garbage collection: `WorkingBytes`
is admission accounting, not a heap quota. The WebP codec pools reusable buffers;
closing an engine does not force process-wide garbage collection. The
default two concurrent operations admit at most 1.25 GiB of image work.

Each engine admits two operations by default. Further work waits in FIFO order
for at most `min(Timeout, 5s)` and then fails as retryable overload
(`fault.Overloaded`); there is no unbounded queue. Filters run without their own parallel fan-out.
Context cancellation is observed while reading, between codec/filter calls, on
pixel-adjustment rows, and while publishing output. Synchronous codec calls and
an uncooperative reader may delay cancellation; the engine retains their actual
capacity until they exit. `Close(ctx)` cancels work and bounds the caller's wait;
`Done()` closes only after actual exit. A module waits for that exit before
finishing shutdown.

Dependency review used the standard image codecs plus
[Go's supplementary image codecs](https://pkg.go.dev/golang.org/x/image),
[GIFT transforms](https://github.com/disintegration/gift),
[vpx portable WebP encoding](https://github.com/gen2brain/vpx), and
[gav1d AVIF codec](https://github.com/gen2brain/gav1d). The reviewed versions are installed and aligned across the framework and
consumer; focused round trips for all eight output formats passed.

## Automatically discovered libvips runtime

Consumers import only Foundry and build normally; no custom build tag, libvips
headers or pkg-config is required. `DefaultConfig()` selects `AutoBackend`.
Enabling the configured image service is enough:

```go
settings.Image.Enabled = true
```

On first engine construction, Foundry looks for a compatible libvips 8.x runtime
(version 8.18 or later). When installed, its available codecs and operations join
the portable capabilities automatically. For example, `brew install vips` supplies
the runtime on macOS. The executable
[native consumer](../../tests/fixtures/consumer/profiles/native_images.go) uses
ordinary application configuration and `Services.Image()`.

When the runtime is missing, unloadable or incompatible:

- Application startup continues with portable imaging and emits one warning for
  the configured image service. A standalone `imaging.New` does not log.
- Portable operations continue to work. Calling a native-only operation or format
  returns an error matching `imaging.ErrNativeUnavailable`, with no partial image result.
- `engine.NativeError()` gives the discovery reason. `engine.Capabilities()` and
  `ForFormat` report actual usable features; installed codecs can vary.

Use `settings.Image.Config.Backend = imaging.LibvipsBackend` to require the native
runtime and fail construction/startup if it cannot load. Use `PortableBackend`
to disable discovery deliberately and suppress the missing-runtime warning.
These selections use the same Foundry API.

The native bridge requires a cgo-enabled binary on macOS, Linux, FreeBSD or Windows;
ordinary native Go builds enable cgo when a C compiler is available.
`CGO_ENABLED=0` builds remain supported: portable imaging works and native requests
return `imaging.ErrNativeUnavailable`. Cross-compiling the native bridge requires the matching
C toolchain. No libvips library is linked into the executable at build time.

Discovery uses the OS shared-library loader (`libvips.so.42`,
`libvips.42.dylib`, or `libvips-42.dll`), plus standard Homebrew library locations
on macOS. Windows uses the application/system safe DLL search directories.
For a custom installation, set `FOUNDRY_VIPS_LIBRARY` to an absolute shared-library
path before starting the process; an unusable explicit path does not fall back to
another installation. Its transitive native dependencies must also be loadable.
Library discovery and capability probes run once per process. Restart after
installing/upgrading the runtime or changing its path. Closing an engine drains
its operations; it does not unload a library shared by other engines.

| Additional format | Read | Write |
| --- | --- | --- |
| `HEIF` / `HEIC` | HEIF images supported by the installed codecs | HEVC, quality 85; `HEIFQuality(1..100)` |
| `JPEG2000` | JP2 or JPEG 2000 codestream | Lossless by default; `JPEG2000Quality(1..100)` selects lossy |
| `JPEGXL` | JPEG XL | Lossless by default; `JPEGXLQuality(1..100)` selects lossy |
| `SVG` | Rasterization of a self-contained SVG document | No SVG output; select a raster output |

`HEIC` aliases the canonical `HEIF` format (`image/heif`, `.heif`); `.heic` and
`image/heic` are accepted aliases. All quality controls require their explicit
output format. A quality of 100 is not a general lossless guarantee. Native
rendering uses 8-bit RGBA for the shared transform pipeline; it does not promise
high-bit-depth or HDR preservation. Additional native formats are still outputs;
multipage input requires `FirstFrame`, and native animations cannot be preserved.
Portable GIF/APNG/WebP/AVIF animation remains available in a native engine.

`engine.Inspect(ctx, bytes)` uses that engine's backend, bounds and lifecycle.
The package-level `imaging.Inspect(bytes, limits)` stays portable.
`engine.ValidatePlan(plan)` checks declarations against its actual capabilities.
Attachments use the configured engine for inspection, media acceptance and
variants; a manager rejects plans and input formats its engine cannot support.
Upload dimension rules can borrow the same engine through
`imagingvalidation.DimensionsWithEngine[foundryhttp.UploadedFile](engine, constraints)`
or `NewEngineMeasurer`. The existing `Dimensions(limits, constraints)` remains
portable. Both inspect detected bytes and ignore the client's MIME/filename hints.

Color and metadata operations are explicit:

```go
plan := imaging.NewPlan().
    ToSRGB().
    SmartFill(512, 512, false, imaging.CropAttention).
    Metadata(imaging.PreserveColorProfile).
    Format(imaging.JPEG)
```

- `ToSRGB()` uses an embedded ICC profile to convert pixels before transforms.
  Without a profile, libvips uses the image's declared color interpretation.
- `SmartFill` shares `Fill` geometry, upscaling rules and resampling. It selects
  the crop with `CropAttention` or `CropEntropy`; it does not recognize identities.
- `StripMetadata` removes metadata by default. `PreserveColorProfile` retains
  only ICC. `PreserveMetadata` retains EXIF (including GPS), XMP, IPTC and ICC
  where the output supports them. Check `FormatCapability.WriteMetadata`.
  Writers without ICC round-trip support reject preservation; for example, the
  libvips 8.18 JPEG 2000 writer currently strips profiles.
  EXIF orientation and dimensions are normalized and old EXIF thumbnails removed.
  XMP/IPTC payloads remain opaque, including application-specific original-image tags.
- ICC conversion replaces the old profile with sRGB when retaining a profile.
  Preserving a non-RGB profile also requires conversion to match the renderer's
  RGB output. Ordinary RGB profile preservation keeps the profile and its pixels.

ICC and metadata processing currently require still images. Their native loaders
cover JPEG, PNG, WebP, TIFF, AVIF and the additional formats above; portable-only
source encodings may reject these operations. Libvips always applies HEIF/AVIF
container orientation, so `IgnoreOrientation` rejects on those native paths.
Native AVIF metadata output cannot select independent `AVIFAlphaQuality`.
These combinations fail explicitly rather than silently dropping the request.

SVG input is limited to 10 MiB, 65,536 elements and 128 levels. External references,
embedded images, scripts, foreign objects and external styles reject; insert
raster content through separately admitted `Result` layers. SVG processing uses
buffer loaders with no filename/base URI, generic ImageMagick or PDF fallback.

Native admission reserves an encoded-input copy, 64 bytes per source pixel plus
16 MiB throughout processing, and 128 bytes per encoded output pixel plus 16 MiB
for native output. Smart cropping adds 64 bytes per peak pixel. These conservative
allowances can reject large native images below the ordinary `Pixels` limit;
configure `WorkingBytes` for the deployment. They are not an OS memory quota:
codec internals, native worker pools and allocators also consume process memory.

Operations retain capacity until their C calls exit. Cancellation is checked at
native evaluation/output boundaries; individual codec calls can delay it. Each
operation releases its images, owned buffers and dependent cache entries, and
flushes thread-local libvips state. Closing an engine does not call process-wide
`vips_shutdown`, which cannot be followed by another initialization. Engines do
not change global libvips cache or concurrency settings; images request one vips
worker, while codec-internal threading follows the installed codec.

`make test-imaging-portable` checks no-cgo builds and explicit unavailable-backend
errors. `make test-imaging-native` requires the native codecs and runs the native
engine and configured consumer checks. PostgreSQL native attachment tests use the
same private test configuration and isolated retained schemas as ordinary tests.
