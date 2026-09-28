package imaging

import (
	"bytes"
	"context"
	"fmt"
	"image"
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
func (e *Engine) ProcessBytes(ctx context.Context, data []byte, plan Plan) (Result, error) {
	return e.Process(ctx, bytes.NewReader(data), plan)
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
	bounds := image.Rect(0, 0, info.Width, info.Height)
	if p.orientation == ApplyOrientation && info.Orientation >= 5 {
		bounds = image.Rect(0, 0, info.Height, info.Width)
	}
	if err := l.dimensions(bounds.Dx(), bounds.Dy()); err != nil {
		return Result{}, err
	}
	for _, s := range p.steps {
		var peak int64
		bounds, peak, err = s.dimensions(bounds, l)
		if err != nil {
			return Result{}, err
		}
		if err := l.dimensions(bounds.Dx(), bounds.Dy()); err != nil {
			return Result{}, err
		}
		if peak > l.Pixels {
			return Result{}, limited()
		}
		if err := l.workspace(int64(len(data)), peak); err != nil {
			return Result{}, err
		}
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
	output := boundedOutput{maximum: l.OutputBytes, ctx: ctx}
	if err := encode(&output, img, format, p.quality, l.OutputBytes); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return Result{data: output.data, info: Info{Format: format, Width: img.Bounds().Dx(), Height: img.Bounds().Dy(), Images: 1, Orientation: 1}}, nil
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
func encode(w io.Writer, img image.Image, f Format, quality int, maximum int64) error {
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
		return avif.Encode(w, img, avif.EncodeOptions{Quality: 60, Speed: 10})
	case ICO:
		return encodeIcon(w, img, maximum)
	default:
		return unsupported()
	}
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
