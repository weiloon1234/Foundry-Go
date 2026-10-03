package imaging

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"slices"
)

type frameDelay struct{ numerator, denominator uint32 }

// Round half up to the target container's clock without floating-point drift.
func (d frameDelay) ticks(rate, maximum uint32) (uint32, error) {
	if d.denominator == 0 {
		return 0, unsupported()
	}
	ticks := (uint64(d.numerator)*uint64(rate) + uint64(d.denominator)/2) / uint64(d.denominator)
	if ticks > uint64(maximum) {
		return 0, unsupported()
	}
	return uint32(ticks), nil
}

func (d frameDelay) reduced() frameDelay {
	a, b := d.numerator, d.denominator
	for b != 0 {
		a, b = b, a%b
	}
	if a != 0 {
		d.numerator, d.denominator = d.numerator/a, d.denominator/a
	}
	return d
}

type imageSequence struct {
	frames []image.Image
	delays []frameDelay
	plays  uint32 // Zero loops forever; otherwise the total number of plays.
}

func (e *Engine) processAnimation(ctx context.Context, data []byte, info inspection, p Plan, format Format) (Result, error) {
	l := e.config.Limits
	if p.srgb || p.metadata != StripMetadata {
		return Result{}, invalid("color and metadata processing requires FirstFrame for animated input")
	}
	if !format.animationEncoding() {
		return Result{}, invalid("output format cannot preserve animation")
	}
	final, err := p.admit(ctx, info, format, int64(len(data)), l, e.config.Backend)
	if err != nil {
		return Result{}, err
	}
	sourcePixels := int64(info.Width) * int64(info.Height)
	outputPixels := int64(final.Dx()) * int64(final.Dy())
	if outputPixels*int64(info.Images) > l.Pixels {
		return Result{}, limited()
	}
	// Retain decoded source frames, transformed frames and GIF palettes, plus
	// compositor/restore canvases and one encoded PNG/WebP frame while assembling
	// animation. The ordinary phase preflight accounts for the active transform.
	reserved := int64(info.Images)*(sourcePixels*8+outputPixels*5) + sourcePixels*12 + l.OutputBytes + int64(len(data))
	if reserved >= l.WorkingBytes {
		return Result{}, limited()
	}
	remaining := l
	remaining.WorkingBytes -= reserved
	if _, err := p.admit(ctx, info, format, int64(len(data)), remaining, e.config.Backend); err != nil {
		return Result{}, err
	}
	sequence, err := decodeSequence(ctx, data, info, l)
	if err != nil {
		return Result{}, err
	}
	if len(sequence.frames) != info.Images || len(sequence.delays) != info.Images {
		return Result{}, invalid("decoded animation differs from its header")
	}
	for i, img := range sequence.frames {
		if img.Bounds().Dx() != info.Width || img.Bounds().Dy() != info.Height {
			return Result{}, invalid("decoded animation dimensions differ from its header")
		}
		img, err = applyImageOrientation(ctx, img, p, info, l)
		if err != nil {
			return Result{}, err
		}
		sequence.frames[i], err = e.transformImage(ctx, img, p)
		if err != nil {
			return Result{}, err
		}
	}
	output := boundedOutput{maximum: l.OutputBytes, ctx: ctx}
	switch format {
	case PNG:
		err = encodeAPNG(ctx, &output, sequence, p.encoding, l)
	case WebP:
		err = encodeWebPSequence(ctx, &output, sequence, p, l)
	case GIF:
		err = encodeGIFSequence(ctx, &output, sequence)
	default:
		err = unsupported()
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
	return Result{data: output.data, info: Info{Format: format, Width: final.Dx(), Height: final.Dy(), Images: len(sequence.frames), Animated: format == PNG || format == WebP || len(sequence.frames) > 1, Orientation: 1}}, nil
}

func decodeSequence(ctx context.Context, data []byte, info inspection, l Limits) (imageSequence, error) {
	switch info.Format {
	case WebP:
		return decodeWebPSequence(ctx, info.webp, false)
	case AVIF:
		return decodeAVIFSequence(data, info.avif)
	case GIF:
		return decodeGIFSequence(ctx, data)
	case PNG:
		return decodeAPNG(ctx, data, l)
	default:
		return imageSequence{}, invalid("input format cannot preserve animation")
	}
}

func decodeGIFSequence(ctx context.Context, data []byte) (imageSequence, error) {
	decoded, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return imageSequence{}, invalid("invalid GIF animation")
	}
	sequence := imageSequence{}
	if decoded.LoopCount < 0 {
		sequence.plays = 1
	} else if decoded.LoopCount > 0 {
		sequence.plays = uint32(decoded.LoopCount) + 1
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, decoded.Config.Width, decoded.Config.Height))
	if len(decoded.Image) > 0 {
		draw.Draw(canvas, canvas.Bounds(), image.NewUniform(gifBackground(decoded, decoded.Image[0])), image.Point{}, draw.Src)
	}
	for i, frame := range decoded.Image {
		if err := ctx.Err(); err != nil {
			return imageSequence{}, err
		}
		var previous *image.NRGBA
		disposal := byte(0)
		if len(decoded.Disposal) > 0 {
			disposal = decoded.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			previous = cloneCanvas(canvas)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		sequence.frames = append(sequence.frames, cloneCanvas(canvas))
		sequence.delays = append(sequence.delays, frameDelay{uint32(decoded.Delay[i]), 100})
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.NewUniform(gifBackground(decoded, frame)), image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			canvas = previous
		}
	}
	return sequence, nil
}

func gifBackground(animation *gif.GIF, frame *image.Paletted) color.Color {
	for _, c := range frame.Palette {
		_, _, _, a := c.RGBA()
		if a == 0 {
			return color.Transparent
		}
	}
	if colors, ok := animation.Config.ColorModel.(color.Palette); ok && int(animation.BackgroundIndex) < len(colors) {
		return colors[animation.BackgroundIndex]
	}
	return color.Transparent
}

func cloneCanvas(canvas *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(canvas.Bounds())
	copy(out.Pix, canvas.Pix)
	return out
}

func encodeGIFSequence(ctx context.Context, out *boundedOutput, sequence imageSequence) error {
	if sequence.plays > 65536 {
		return invalid("GIF loop count cannot represent this animation")
	}
	encoded := gif.GIF{LoopCount: 0}
	if sequence.plays == 1 {
		encoded.LoopCount = -1
	} else if sequence.plays > 1 {
		encoded.LoopCount = int(sequence.plays) - 1
	}
	colors, err := sequencePalette(ctx, sequence.frames)
	if err != nil {
		return err
	}
	for i, img := range sequence.frames {
		if err := ctx.Err(); err != nil {
			return err
		}
		delay := sequence.delays[i]
		ticks, err := delay.ticks(100, 65535)
		if err != nil {
			return invalid("GIF frame delay cannot represent this animation")
		}
		frame := image.NewPaletted(img.Bounds(), colors)
		draw.FloydSteinberg.Draw(frame, frame.Bounds(), img, img.Bounds().Min)
		encoded.Image = append(encoded.Image, frame)
		encoded.Delay = append(encoded.Delay, int(ticks))
		encoded.Disposal = append(encoded.Disposal, gif.DisposalBackground)
	}
	return gif.EncodeAll(out, &encoded)
}

// Retain exact colors when the rendered animation already fits a GIF palette.
// On richer images use a fixed dither palette, retaining black and white while
// reserving transparency (dropping the final Plan9 entry would remove white).
func sequencePalette(ctx context.Context, frames []image.Image) (color.Palette, error) {
	var colors color.Palette
	seen := make(map[color.NRGBA]bool, 256)
	for _, img := range frames {
		bounds := img.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				if c.A < 128 {
					c = color.NRGBA{}
				} else {
					c.A = 255
				}
				if seen[c] {
					continue
				}
				if len(colors) == 256 {
					fallback := slices.Clone(palette.Plan9)
					fallback[1] = color.Transparent
					return fallback, nil
				}
				seen[c] = true
				colors = append(colors, c)
			}
		}
	}
	return colors, nil
}
