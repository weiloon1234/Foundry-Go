package imaging

import (
	"context"
	"image"
	"image/color"
	"math"

	"github.com/weiloon1234/Foundry-Go/value"
)

// BlendMode combines straight RGB channels before source-over alpha composition.
type BlendMode uint8

const (
	BlendNormal BlendMode = iota
	BlendMultiply
	BlendScreen
	BlendOverlay
	BlendDarken
	BlendLighten
	BlendDifference
	BlendExclusion
)

// Placement positions a layer relative to the canvas. Positive offsets move
// right/down, including at right/bottom anchors; use negative offsets for inset.
// Omitted opacity is 1; an explicitly supplied zero is fully transparent.
type Placement struct {
	Position Position
	X, Y     int
	Opacity  value.Optional[float64]
	Blend    BlendMode
}

func (p Placement) Validate() error {
	if p.Position > BottomRight || p.Blend > BlendExclusion || p.X < -65535 || p.X > 65535 || p.Y < -65535 || p.Y > 65535 {
		return invalid("invalid image placement")
	}
	if opacity, ok := p.Opacity.Get(); ok && (math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity < 0 || opacity > 1) {
		return invalid("image opacity must be between 0 and 1")
	}
	return nil
}

func (p Placement) opacity() float64 {
	if opacity, ok := p.Opacity.Get(); ok {
		return opacity
	}
	return 1
}

// Insert composites an immutable processed Result onto this image. Prepare the
// layer once with Engine.Process, then reuse it in concurrent plans. Pixels
// outside the canvas are clipped; layer bytes and decoding count toward limits.
func (p Plan) Insert(layer Result, placement Placement) Plan {
	return p.append(step{kind: insert, layer: layer, placement: placement})
}

type MaskMode uint8

const (
	AlphaMask MaskMode = iota
	LuminanceMask
)

// Mask multiplies existing alpha by mask alpha, and for LuminanceMask also by
// weighted RGB luminance. Pixels outside the positioned mask become transparent.
// Placement opacity is supported; blend modes other than BlendNormal reject.
func (p Plan) Mask(layer Result, placement Placement, mode MaskMode) Plan {
	return p.append(step{kind: mask, layer: layer, placement: placement, maskMode: mode})
}

// retainedInput counts every declared layer, font, text and path, conservatively
// including repeated references, against the aggregate encoded-input allowance.
func (p Plan) retainedInput(input int64, l Limits) (int64, error) {
	for _, s := range p.steps {
		input += int64(len(s.layer.data))
		input += int64(len(s.path.commands)) * 56
		if s.text != nil {
			input += int64(len(s.text.text)) + int64(len(s.text.options.Font.data))
		}
	}
	if input > l.InputBytes {
		return 0, limited()
	}
	return input, nil
}

func (s step) admitLayer(ctx context.Context, l Limits, input, current int64, backend Backend) error {
	if s.kind != insert && s.kind != mask {
		return nil
	}
	info, err := inspectForBackend(ctx, s.layer.data, l, backend)
	if err != nil {
		return err
	}
	if info.Animated || info.Images != 1 {
		return invalid("image layer requires a single frame")
	}
	pixels := int64(info.Width) * int64(info.Height)
	// Retain the current canvas while decoding, then allow both current and
	// converted destination while compositing. No layer is decoded in preflight.
	if info.native {
		input += int64(len(s.layer.data))
	}
	return l.admit(input, current*2+max(info.decodeWorkspace(), pixels*info.Format.decodedBytes()))
}

func composite(ctx context.Context, img image.Image, s step, l Limits, backend Backend) (image.Image, error) {
	info, err := inspectForBackend(ctx, s.layer.data, l, backend)
	if err != nil {
		return nil, err
	}
	var layer image.Image
	if info.native {
		source, openErr := openNative(ctx, s.layer.data, info, l)
		if openErr != nil {
			return nil, openErr
		}
		defer source.close()
		layer, err = source.decode(ctx, NewPlan(), l)
	} else {
		layer, err = decodeChecked(ctx, s.layer.data, info, l)
	}
	if err != nil {
		return nil, err
	}
	out := ownedNRGBA(img)
	origin := s.placement.Position.point(out.Bounds(), layer.Bounds()).Add(image.Pt(s.placement.X, s.placement.Y))
	area := image.Rectangle{Min: origin, Max: origin.Add(layer.Bounds().Size())}.Intersect(out.Bounds())
	rows := area
	if s.kind == mask {
		rows = out.Bounds()
	}
	opacity := s.placement.opacity()
	for y := rows.Min.Y; y < rows.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := rows.Min.X; x < rows.Max.X; x++ {
			point := image.Pt(x, y)
			dst := out.NRGBAAt(x, y)
			var src color.NRGBA
			if point.In(area) {
				at := point.Sub(origin).Add(layer.Bounds().Min)
				src = color.NRGBAModel.Convert(layer.At(at.X, at.Y)).(color.NRGBA)
			}
			if s.kind == mask {
				factor := float64(src.A) / 255 * opacity
				if s.maskMode == LuminanceMask {
					factor *= (0.2126*float64(src.R) + 0.7152*float64(src.G) + 0.0722*float64(src.B)) / 255
				}
				dst.A = uint8(math.Round(float64(dst.A) * factor))
				out.SetNRGBA(x, y, dst)
			} else {
				out.SetNRGBA(x, y, blendPixel(dst, src, opacity, s.placement.Blend))
			}
		}
	}
	return out, nil
}

func blendPixel(dst, src color.NRGBA, opacity float64, mode BlendMode) color.NRGBA {
	as, ab := float64(src.A)/255*opacity, float64(dst.A)/255
	a := as + ab*(1-as)
	if as == 0 {
		return dst
	}
	channel := func(back, source uint8) uint8 {
		b, s := float64(back)/255, float64(source)/255
		mixed := s
		switch mode {
		case BlendMultiply:
			mixed = b * s
		case BlendScreen:
			mixed = b + s - b*s
		case BlendOverlay:
			if b <= 0.5 {
				mixed = 2 * b * s
			} else {
				mixed = 1 - 2*(1-b)*(1-s)
			}
		case BlendDarken:
			mixed = math.Min(b, s)
		case BlendLighten:
			mixed = math.Max(b, s)
		case BlendDifference:
			mixed = math.Abs(b - s)
		case BlendExclusion:
			mixed = b + s - 2*b*s
		}
		return uint8(math.Round(math.Max(0, math.Min(1, ((1-as)*ab*b+(1-ab)*as*s+as*ab*mixed)/a)) * 255))
	}
	return color.NRGBA{R: channel(dst.R, src.R), G: channel(dst.G, src.G), B: channel(dst.B, src.B), A: uint8(math.Round(a * 255))}
}
