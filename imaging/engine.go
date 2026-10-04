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

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"

	"github.com/weiloon1234/Foundry-Go/internal/workscope"
)

// Engine owns admission and cancellation, not input readers. Close requests
// cancellation and waits for actual operation exit; Done is never optimistic.
// Codec/transform calls are synchronous. Cancellation is observed between codec
// operations and rows; a codec already running retains its capacity until exit.
type Engine struct {
	config       Config
	calls        *workscope.Group
	capabilities Capabilities
	nativeErr    error
}

func New(config Config) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	formats := portableFormats()
	capabilities := Capabilities{Formats: formats[:]}
	var nativeErr error
	if config.Backend != PortableBackend {
		native, err := nativeCapabilities()
		if err != nil {
			if config.Backend == LibvipsBackend {
				return nil, err
			}
			nativeErr = err
		} else {
			capabilities = native
		}
	}
	calls, err := workscope.New(config.MaxActive, config.Timeout)
	if err != nil {
		return nil, err
	}
	return &Engine{config: config, calls: calls, capabilities: capabilities, nativeErr: nativeErr}, nil
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

// Process reads at most the remaining InputBytes+1 after accounting for layers,
// checks the complete plan before decoding pixels, and publishes no output on
// failure. It does not close source.
func (e *Engine) Process(ctx context.Context, source io.Reader, plan Plan) (Result, error) {
	if err := e.Validate(); err != nil {
		return Result{}, err
	}
	if source == nil {
		return Result{}, invalid("image input requires a reader")
	}
	if err := e.ValidatePlan(plan); err != nil {
		return Result{}, err
	}
	retained, err := plan.retainedInput(0, e.config.Limits)
	if err != nil {
		return Result{}, err
	}
	inputBudget := e.config.Limits.InputBytes - retained
	var result Result
	err = e.calls.Run(ctx, "process image", func(ctx context.Context) error {
		data, err := io.ReadAll(io.LimitReader(workscope.Reader(ctx, source), inputBudget+1))
		if err != nil {
			return err
		}
		if int64(len(data)) > inputBudget {
			return limited()
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
	if err := e.ValidatePlan(plan); err != nil {
		return Result{}, err
	}
	var result Result
	err := e.calls.Run(ctx, "process image", func(ctx context.Context) error {
		if _, err := plan.retainedInput(int64(len(data)), e.config.Limits); err != nil {
			return err
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
	info, err := e.inspect(ctx, data)
	if err != nil {
		return Result{}, err
	}
	if p.frames == RejectAnimation && (info.Animated || (info.Format == TIFF || info.native) && info.Images > 1) {
		return Result{}, invalid("multiple image frames require an explicit frame policy")
	}
	format, err := p.encodingFormat(info.Format)
	if err != nil {
		return Result{}, err
	}
	if p.frames == PreserveAnimation && (info.Format == TIFF || info.native) && info.Images > 1 {
		return Result{}, invalid("this native or multipage input cannot preserve animation")
	}
	if err := e.validateOutput(p, format); err != nil {
		return Result{}, err
	}
	if p.frames == PreserveAnimation && info.Animated {
		return e.processAnimation(ctx, data, info, p, format)
	}
	if p.nativePixels(info) {
		if info.Animated && (p.srgb || p.metadata != StripMetadata) {
			return Result{}, invalid("ICC conversion and metadata preservation require a still image")
		}
		if (info.Format == HEIF || info.Format == AVIF) && p.orientation == IgnoreOrientation {
			return Result{}, invalid("libvips cannot ignore HEIF container orientation")
		}
		info, err = inspectNative(ctx, data, info.Format, l)
		if err != nil {
			return Result{}, err
		}
	}
	if _, err := p.admit(ctx, info, format, int64(len(data)), l, e.capabilities.Backend); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var img image.Image
	var source nativeSource
	if p.nativePixels(info) {
		if info.Animated && p.frames != FirstFrame {
			return Result{}, invalid("native color and metadata require a single frame")
		}
		source, err = openNative(ctx, data, info, l)
		if err != nil {
			return Result{}, err
		}
		defer source.close()
		img, err = source.decode(ctx, p, l)
	} else {
		img, err = decodeChecked(ctx, data, info, l)
	}
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	img, err = applyImageOrientation(ctx, img, p, info, l)
	if err != nil {
		return Result{}, err
	}
	return e.transformAndEncode(ctx, img, p, format, source)
}

func (e *Engine) transformImage(ctx context.Context, img image.Image, p Plan) (image.Image, error) {
	l := e.config.Limits
	var err error
	for _, s := range p.steps {
		s.resampling = p.resampling
		img, err = applyStep(ctx, img, s, l, e.capabilities.Backend)
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.flatten {
		img = flatten(img, p.background)
	}
	return img, nil
}

func (e *Engine) transformAndEncode(ctx context.Context, img image.Image, p Plan, format Format, source nativeSource) (Result, error) {
	l := e.config.Limits
	img, err := e.transformImage(ctx, img, p)
	if err != nil {
		return Result{}, err
	}
	output := boundedOutput{maximum: l.OutputBytes, ctx: ctx}
	if p.nativeOutput(format) {
		if source != nil {
			err = source.encode(ctx, &output, img, p, format, l)
		} else {
			err = encodeNative(ctx, &output, img, p, format, l)
		}
	} else {
		err = p.encode(&output, img, format, l.OutputBytes)
	}
	if err != nil {
		return Result{}, err
	}
	if output.err != nil {
		return Result{}, output.err
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
func (p Plan) admit(ctx context.Context, info inspection, format Format, input int64, l Limits, backend Backend) (image.Rectangle, error) {
	sourceBytes := input
	input, err := p.retainedInput(input, l)
	if err != nil {
		return image.Rectangle{}, err
	}
	if p.nativePixels(info) {
		// The native source owns its encoded copy and codec buffers through output
		// metadata transfer. Reserve them throughout every subsequent phase.
		info.native = true
		input += sourceBytes + info.decodeWorkspace()
	} else if err := l.admit(input, info.decodeWorkspace()); err != nil {
		return image.Rectangle{}, err
	}
	if err := l.admit(input, 0); err != nil {
		return image.Rectangle{}, err
	}
	bounds := image.Rect(0, 0, info.Width, info.Height)
	current := info.Format.decodedBytes()
	if p.orientation == ApplyOrientation && info.avif != nil && !info.avif.crop.Empty() {
		next := image.Rect(0, 0, info.avif.crop.Dx(), info.avif.crop.Dy())
		if err := l.admit(input, int64(bounds.Dx())*int64(bounds.Dy())*current+int64(next.Dx())*int64(next.Dy())*transformBytes); err != nil {
			return image.Rectangle{}, err
		}
		bounds, current = next, transformBytes
	}
	if p.orientation == ApplyOrientation && orientationFilter(info.Orientation) != nil {
		next := bounds
		if info.Orientation >= 5 {
			next = image.Rect(0, 0, bounds.Dy(), bounds.Dx())
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
		if err := ctx.Err(); err != nil {
			return image.Rectangle{}, err
		}
		before := int64(bounds.Dx()) * int64(bounds.Dy())
		s.resampling = p.resampling
		auxiliary, err := s.auxiliaryBytes(bounds)
		if err != nil {
			return image.Rectangle{}, err
		}
		if err := s.admitLayer(ctx, l, input, before*current, backend); err != nil {
			return image.Rectangle{}, err
		}
		var peak int64
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
		if err := l.admit(input, before*current+after*transformBytes+peak*s.scratch()+auxiliary); err != nil {
			return image.Rectangle{}, err
		}
		if err := s.preflightDrawing(ctx, l); err != nil {
			return image.Rectangle{}, err
		}
		current = transformBytes
	}
	if format == ICO && (bounds.Dx() > 256 || bounds.Dy() > 256) {
		return image.Rectangle{}, invalid("ICO dimensions must not exceed 256")
	}
	if format == WebP {
		maximum := 16384
		if p.encoding.webpLossy() {
			maximum = 16383
		}
		if bounds.Dx() > maximum || bounds.Dy() > maximum {
			return image.Rectangle{}, invalid("image dimensions exceed the selected WebP mode")
		}
	}
	final := int64(bounds.Dx()) * int64(bounds.Dy())
	encoding := final*current + p.encodeWorkspace(format, bounds) + l.OutputBytes
	if p.flatten {
		encoding += final * transformBytes
	}
	if err := l.admit(input, encoding); err != nil {
		return image.Rectangle{}, err
	}
	return bounds, nil
}
func decodeChecked(ctx context.Context, data []byte, info inspection, l Limits) (image.Image, error) {
	var img image.Image
	var err error
	if info.webp != nil {
		if info.webp.animated {
			var sequence imageSequence
			sequence, err = decodeWebPSequence(ctx, info.webp, true)
			if err == nil {
				img = sequence.frames[0]
			}
		} else {
			img, err = decodeWebPFrame(ctx, info.webp.frames[0])
		}
	} else if info.avif != nil {
		img, err = decodeAVIF(data, info.avif)
	} else {
		img, err = decode(data, info.Format, l)
	}
	if err != nil {
		return nil, invalid("image pixel decoding failed")
	}
	if img == nil || img.Bounds().Dx() != info.Width || img.Bounds().Dy() != info.Height {
		return nil, invalid("decoded image dimensions differ from its header")
	}
	return img, nil
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
	case ICO:
		return decodeIcon(data, l)
	default:
		return nil, unsupported()
	}
}

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
	err     error // First failure survives codecs that ignore io.Writer errors.
	data    []byte
	maximum int64
	ctx     context.Context
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if b.ctx != nil {
		if err := b.ctx.Err(); err != nil {
			b.err = err
			return 0, err
		}
	}
	if int64(len(p)) > b.maximum-int64(len(b.data)) {
		b.err = limited()
		return 0, b.err
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
