package imaging

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"slices"

	"github.com/HugoSmits86/nativewebp"
	"github.com/gen2brain/gav1d/avif"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"

	"github.com/weiloon1234/Foundry-Go/internal/workscope"
)

// Engine owns admission and cancellation, not input readers. Close requests
// cancellation and waits for actual operation exit; Done is never optimistic.
// Codec/transform calls are synchronous. Cancellation is observed between codec
// operations and rows; a codec already running retains its capacity until exit.
type Engine struct {
	config Config
	calls  *workscope.Group
}

func New(config Config) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	calls, err := workscope.New(config.MaxActive, config.Timeout)
	if err != nil {
		return nil, err
	}
	return &Engine{config: config, calls: calls}, nil
}
func (e *Engine) Validate() error {
	if e == nil || e.calls == nil {
		return invalid("invalid image engine")
	}
	return e.config.Validate()
}
func (e *Engine) Limits() Limits {
	if e == nil {
		return Limits{}
	}
	return e.config.Limits
}
func (e *Engine) Close(ctx context.Context) error {
	if e == nil {
		return invalid("invalid image engine")
	}
	return e.calls.Close(ctx)
}
func (e *Engine) Done() <-chan struct{} {
	if e == nil {
		var g *workscope.Group
		return g.Done()
	}
	return e.calls.Done()
}
func (*Engine) Format(s fmt.State, _ rune) { safeFormat(s, "image engine") }

// Result owns encoded output. Reader exposes read-only access; Bytes returns an
// independent copy. Routine formatting never exports image bytes.
type Result struct {
	data []byte
	info Info
}

func (r Result) Info() Info               { return r.info }
func (r Result) Size() int64              { return int64(len(r.data)) }
func (r Result) Bytes() []byte            { return slices.Clone(r.data) }
func (r Result) Reader() io.Reader        { return bytes.NewReader(r.data) }
func (Result) Format(s fmt.State, _ rune) { safeFormat(s, "image result") }
func (Result) MarshalJSON() ([]byte, error) {
	return nil, invalid("image result requires explicit byte export")
}

// Process reads at most InputBytes+1, checks the complete declared plan before
// decoding pixels, and publishes no output on failure. It does not close source.
func (e *Engine) Process(ctx context.Context, source io.Reader, plan Plan) (Result, error) {
	if err := e.Validate(); err != nil {
		return Result{}, err
	}
	if source == nil {
		return Result{}, invalid("image input requires a reader")
	}
	if err := plan.Validate(); err != nil {
		return Result{}, err
	}
	var result Result
	err := e.calls.Run(ctx, "process image", func(ctx context.Context) error {
		data, err := io.ReadAll(io.LimitReader(workscope.Reader(ctx, source), e.config.Limits.InputBytes+1))
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		result, err = e.process(ctx, data, plan)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

// ProcessBytes runs the same bounded pipeline directly on data without copying
// it. data must not change during the call.
func (e *Engine) ProcessBytes(ctx context.Context, data []byte, plan Plan) (Result, error) {
	if err := e.Validate(); err != nil {
		return Result{}, err
	}
	if err := plan.Validate(); err != nil {
		return Result{}, err
	}
	var result Result
	err := e.calls.Run(ctx, "process image", func(ctx context.Context) error {
		if int64(len(data)) > e.config.Limits.InputBytes {
			return limited()
		}
		var err error
		result, err = e.process(ctx, data, plan)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

func (e *Engine) process(ctx context.Context, data []byte, p Plan) (Result, error) {
	l := e.config.Limits
	info, err := inspect(data, l)
	if err != nil {
		return Result{}, err
	}
	if p.frames == RejectAnimation && (info.Animated || info.Format == TIFF && info.Images > 1) {
		return Result{}, invalid("multiple image frames require an explicit first-frame policy")
	}
	format := p.output
	if format == "" {
		format = info.Format
	}
	if p.quality != 0 && format != JPEG {
		return Result{}, invalid("quality applies only to JPEG; WebP output is lossless")
	}
	bounds, err := p.admit(info, format, int64(len(data)), l)
	if err != nil {
		return Result{}, err
	}
	if format == ICO && (bounds.Dx() > 256 || bounds.Dy() > 256) {
		return Result{}, invalid("ICO dimensions must not exceed 256")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	img, err := decode(data, info.Format, l)
	if err != nil {
		return Result{}, invalid("image pixel decoding failed")
	}
	if img == nil || img.Bounds().Dx() != info.Width || img.Bounds().Dy() != info.Height {
		return Result{}, invalid("decoded image dimensions differ from its header")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if p.orientation == ApplyOrientation {
		if filter := orientationFilter(info.Orientation); filter != nil {
			img, err = drawFilter(ctx, img, filter, l)
			if err != nil {
				return Result{}, err
			}
		}
	}
	for _, s := range p.steps {
		img, err = applyStep(ctx, img, s, l)
		if err != nil {
			return Result{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if p.flatten {
		img = flatten(img, p.background)
	}
	output := boundedOutput{maximum: l.OutputBytes, ctx: ctx}
	if err := encode(&output, img, format, p.quality, p.avifQuality, l.OutputBytes); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return Result{data: output.data, info: Info{Format: format, Width: img.Bounds().Dx(), Height: img.Bounds().Dy(), Images: 1, Orientation: 1}}, nil
}

// admit checks every pipeline phase against the limits before any pixel is
// decoded and returns the final canvas. Each phase is admitted separately: the
// decoded image is replaced by the first transform, and only the final canvas
// remains while encoding.
func (p Plan) admit(info Info, format Format, input int64, l Limits) (image.Rectangle, error) {
	bounds := image.Rect(0, 0, info.Width, info.Height)
	current := info.Format.decodedBytes()
	if p.orientation == ApplyOrientation && orientationFilter(info.Orientation) != nil {
		next := bounds
		if info.Orientation >= 5 {
			next = image.Rect(0, 0, info.Height, info.Width)
		}
		if err := l.dimensions(next.Dx(), next.Dy()); err != nil {
			return image.Rectangle{}, err
		}
		pixels := int64(bounds.Dx()) * int64(bounds.Dy())
		if err := l.admit(input, pixels*(current+transformBytes)); err != nil {
			return image.Rectangle{}, err
		}
		bounds, current = next, transformBytes
	}
	for _, s := range p.steps {
		before := int64(bounds.Dx()) * int64(bounds.Dy())
		var peak int64
		var err error
		bounds, peak, err = s.dimensions(bounds, l)
		if err != nil {
			return image.Rectangle{}, err
		}
		if err := l.dimensions(bounds.Dx(), bounds.Dy()); err != nil {
			return image.Rectangle{}, err
		}
		if peak > l.Pixels {
			return image.Rectangle{}, limited()
		}
		after := int64(bounds.Dx()) * int64(bounds.Dy())
		if err := l.admit(input, before*current+after*transformBytes+peak*s.scratch()); err != nil {
			return image.Rectangle{}, err
		}
		current = transformBytes
	}
	final := int64(bounds.Dx()) * int64(bounds.Dy())
	encoding := final*(current+format.encodeBytes()) + l.OutputBytes
	if p.flatten {
		encoding += final * transformBytes
	}
	if err := l.admit(input, encoding); err != nil {
		return image.Rectangle{}, err
	}
	return bounds, nil
}
func decode(data []byte, f Format, l Limits) (image.Image, error) {
	r := bytes.NewReader(data)
	switch f {
	case JPEG:
		return jpeg.Decode(r)
	case PNG:
		return png.Decode(r)
	case GIF:
		frame, err := gif.Decode(r)
		if err != nil {
			return nil, err
		}
		cfg, err := gif.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		canvas := image.NewNRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Src)
		return canvas, nil
	case BMP:
		return bmp.Decode(r)
	case TIFF:
		return tiff.Decode(r)
	case WebP:
		return webp.Decode(r)
	case ICO:
		return decodeIcon(data, l)
	default:
		return nil, unsupported()
	}
}
func encode(w io.Writer, img image.Image, f Format, quality, avifQuality int, maximum int64) error {
	switch f {
	case JPEG:
		if quality == 0 {
			quality = 85
		}
		return jpeg.Encode(w, img, &jpeg.Options{Quality: quality})
	case PNG:
		return png.Encode(w, img)
	case GIF:
		return gif.Encode(w, img, &gif.Options{NumColors: 256})
	case BMP:
		return bmp.Encode(w, img)
	case TIFF:
		return tiff.Encode(w, img, &tiff.Options{Compression: tiff.Deflate, Predictor: true})
	case WebP:
		return nativewebp.Encode(w, img, nil)
	case AVIF:
		if avifQuality == 0 {
			avifQuality = DefaultAVIFQuality
		}
		return avif.Encode(w, img, avif.EncodeOptions{Quality: avifQuality, Speed: 10})
	case ICO:
		return encodeIcon(w, img, maximum)
	default:
		return unsupported()
	}
}

// DefaultAVIFQuality is used unless a plan selects AVIFQuality.
const DefaultAVIFQuality = 60

// flatten composites an image over an opaque background, so formats without
// alpha (such as JPEG) do not turn transparent pixels black.
func flatten(img image.Image, background color.NRGBA) image.Image {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Over)
	return out
}

type boundedOutput struct {
	data    []byte
	maximum int64
	ctx     context.Context
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.ctx != nil {
		if err := b.ctx.Err(); err != nil {
			return 0, err
		}
	}
	if int64(len(p)) > b.maximum-int64(len(b.data)) {
		return 0, limited()
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
