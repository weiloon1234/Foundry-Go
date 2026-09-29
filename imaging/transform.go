package imaging

import (
	"context"
	"image"
	"math"

	"github.com/disintegration/gift"
)

func orientationFilter(o uint8) gift.Filter {
	switch o {
	case 2:
		return gift.FlipHorizontal()
	case 3:
		return gift.Rotate180()
	case 4:
		return gift.FlipVertical()
	case 5:
		return gift.Transpose()
	case 6:
		return gift.Rotate270() // gift angles are counterclockwise.
	case 7:
		return gift.Transverse()
	case 8:
		return gift.Rotate90()
	default:
		return nil
	}
}

func applyStep(ctx context.Context, img image.Image, s step, l Limits) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, _, err := s.dimensions(img.Bounds(), l)
	if err != nil {
		return nil, err
	}
	var filter gift.Filter
	switch s.kind {
	case resizeExact, resizeFit:
		filter = gift.Resize(b.Dx(), b.Dy(), gift.LanczosResampling)
	case resizeFill:
		filter = gift.ResizeToFill(b.Dx(), b.Dy(), gift.LanczosResampling, gift.CenterAnchor)
	case crop:
		filter = gift.Crop(image.Rect(s.x, s.y, s.x+s.width, s.y+s.height).Add(img.Bounds().Min))
	case blur:
		filter = gift.GaussianBlur(float32(s.number))
	case grayscale:
		return adjustPixels(ctx, img, s)
	case rotate:
		switch s.number {
		case 90:
			filter = gift.Rotate270()
		case 180:
			filter = gift.Rotate180()
		case 270:
			filter = gift.Rotate90()
		}
	case flipHorizontal:
		filter = gift.FlipHorizontal()
	case flipVertical:
		filter = gift.FlipVertical()
	case brightness, contrast:
		return adjustPixels(ctx, img, s)
	default:
		return nil, invalid("invalid image transform")
	}
	return drawFilter(ctx, img, filter, l)
}

func drawFilter(ctx context.Context, img image.Image, filter gift.Filter, l Limits) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b := filter.Bounds(img.Bounds())
	if err := l.dimensions(b.Dx(), b.Dy()); err != nil {
		return nil, err
	}
	out := image.NewNRGBA(b)
	// Engine admission bounds concurrency. A filter must not independently fan
	// out to every core for every admitted upload.
	filter.Draw(out, img, &gift.Options{Parallelization: false})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// adjustPixels applies brightness, contrast or grayscale directly to 8-bit
// NRGBA pixel rows. Pipeline-owned NRGBA images are adjusted in place; other
// decoded models are converted once. Brightness and contrast are per-channel
// lookup tables with the same rounding as the per-pixel formulas.
func adjustPixels(ctx context.Context, img image.Image, s step) (image.Image, error) {
	out := ownedNRGBA(img)
	var table [256]uint8
	factor := math.Pow((100+s.number)/100, 2)
	for v := range table {
		switch s.kind {
		case brightness:
			table[v] = clamp(float64(v) + s.number)
		case contrast:
			table[v] = clamp(((float64(v)/255-0.5)*factor + 0.5) * 255)
		}
	}
	b := out.Bounds()
	width := b.Dx() * 4
	for y := range b.Dy() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := out.Pix[y*out.Stride : y*out.Stride+width]
		for i := 0; i < len(row); i += 4 {
			if s.kind == grayscale {
				v := uint8((uint32(row[i])*2126 + uint32(row[i+1])*7152 + uint32(row[i+2])*722) / 10000)
				row[i], row[i+1], row[i+2] = v, v, v
				continue
			}
			row[i], row[i+1], row[i+2] = table[row[i]], table[row[i+1]], table[row[i+2]]
		}
	}
	return out, nil
}

// ownedNRGBA returns img when it already is NRGBA (every decoded or
// transformed image is owned by the pipeline) or converts it once.
func ownedNRGBA(img image.Image) *image.NRGBA {
	if nrgba, ok := img.(*image.NRGBA); ok {
		return nrgba
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	converter := gift.New()
	converter.SetParallelization(false)
	converter.Draw(out, img)
	return out
}
func clamp(v float64) uint8 { return uint8(math.Max(0, math.Min(255, v))) }
