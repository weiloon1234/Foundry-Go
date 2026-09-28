# Imaging

Milestone 18 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-18-verification-and-consumer-review)
records the checks and operational limits.

An image plan owns transformation policy and can be reused by an attachment
collection. Construct an engine with `imaging.New(imaging.DefaultConfig())` or
register `imaging.Module` to give the application ownership of its shutdown.

```go
plan := imaging.NewPlan().
    Fill(1200, 630, true).
    Format(imaging.WebP)
result, err := engine.Process(ctx, uploadReader, plan)
```

`Process` borrows the reader and never closes it. It rejects invalid byte counts
and stops after 100 consecutive empty reads with `io.ErrNoProgress`.
`ProcessBytes` provides the same
bounded pipeline for a byte slice. The input must not be changed during the call.
`Result.Reader` provides read-only encoded output, `Bytes` makes an independent
copy, and `Info` reports the resulting format and dimensions. A failure returns
no partial result. Images do not serialize implicitly to JSON.

Plans are immutable values. Available transforms are exact `Resize`, aspect-ratio
`Fit`, centered `Fill`, exact `Crop`, clockwise 90/180/270-degree `Rotate`, flips,
Gaussian `Blur`, `Grayscale`, channel-offset `Brightness`, and `Contrast`.
Resizing uses Lanczos3. Fit and fill take an explicit upscale choice. A fill that
would need forbidden upscaling fails; a fit keeps the smaller original size.
Crop rectangles must fit entirely inside the image. Transform order matters.

| Format | Input | Output |
| --- | --- | --- |
| JPEG | Yes | Quality 85 by default; `JPEGQuality(1..100)` |
| PNG | Yes | Lossless |
| WebP | Static lossy/lossless | Lossless |
| GIF | Yes; animation requires `Frames(FirstFrame)` | One frame, up to 256 colors |
| BMP | Supported Windows BMP encodings | BMP |
| TIFF | Supported baseline encodings; multiple pages require `FirstFrame` | Deflate TIFF |
| ICO | Embedded PNG or uncompressed Windows DIB; largest representation | One PNG representation, at most 256×256 |
| AVIF | Explicitly unsupported | Still 8-bit AVIF, quality 60, speed 10 |

AVIF encoding matches the Rust reference's default format capability; its default
build does not enable the native AVIF decoder. Animated WebP is rejected because
the selected decoder does not support it. PNG animation is rejected by default;
an explicit first-frame policy decodes its default PNG image. TIFF/ICO variants
unsupported by their underlying codec fail rather than being guessed. Filenames,
extensions and caller-provided MIME types never determine the input codec.

`JPEGQuality` on any non-JPEG output fails, including WebP. GIF first-frame output
uses the logical canvas with transparent space outside the first frame. Normal
pixel transforms use 8-bit non-premultiplied RGBA. Untouched decoded images may
retain higher precision where the output codec supports it.

EXIF orientation is applied before user transforms by default; use
`Orientation(IgnoreOrientation)` explicitly to ignore it. Re-encoding strips
EXIF/GPS, XMP, comments and ICC metadata. This is not color-profile conversion.
Metadata preservation is intentionally unsupported, so private camera/location
metadata cannot accidentally survive an attachment transformation.

`Inspect(bytes, limits)` reads container metadata without decoding pixel data.
Successful inspection alone does not prove compressed image contents are valid.
The processing pipeline inspects first, validates every planned output and
intermediate resampling allocation, then decodes and transforms.

Limits cover input bytes, encoded output bytes, width, height, pixels, frames and
admitted working memory. All limits are positive; there is no unbounded mode.
The memory budget is a conservative admission calculation for image buffers and
codec work, not an OS/process heap quota. Defaults allow up to 50 MiB input and
output, 12000-pixel dimensions, 25 million pixels, 64 container images and
512 MiB admitted work per operation; the tightest bound wins. A 32-transform
limit also applies. Container frame/page counts and aggregate image areas are
checked before decoding. Resampling intermediates can be larger than either
input or final output and are included in the pixel/workspace checks.

Each engine admits two operations by default. Excess work fails immediately;
there is no unbounded queue. Filters run without their own parallel fan-out.
Context cancellation is observed while reading, between codec/filter calls, on
pixel-adjustment rows, and while publishing output. Synchronous codec calls and
an uncooperative reader may delay cancellation; the engine retains their actual
capacity until they exit. `Close(ctx)` cancels work and bounds the caller's wait;
`Done()` closes only after actual exit. A module waits for that exit before
finishing shutdown.

Dependency review used the standard image codecs plus
[Go's supplementary image codecs](https://pkg.go.dev/golang.org/x/image),
[GIFT transforms](https://github.com/disintegration/gift),
[nativewebp lossless encoding](https://github.com/HugoSmits86/nativewebp), and
[gav1d AVIF encoding](https://github.com/gen2brain/gav1d). The reviewed versions are installed and aligned across the framework and
consumer; focused round trips for all eight output formats passed.
