package imaging

import (
	"context"
	"image"
	"image/color"
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

func adjustPixels(ctx context.Context, img image.Image, s step) (image.Image, error) {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	factor := math.Pow((100+s.number)/100, 2)
	for y := range b.Dy() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := range b.Dx() {
			c := color.NRGBAModel.Convert(img.At(x+b.Min.X, y+b.Min.Y)).(color.NRGBA)
			switch s.kind {
			case brightness:
				c.R = clamp(float64(c.R) + s.number)
				c.G = clamp(float64(c.G) + s.number)
				c.B = clamp(float64(c.B) + s.number)
			case contrast:
				adjust := func(v uint8) uint8 { return clamp(((float64(v)/255-0.5)*factor + 0.5) * 255) }
				c.R, c.G, c.B = adjust(c.R), adjust(c.G), adjust(c.B)
			case grayscale:
				v := uint8((uint32(c.R)*2126 + uint32(c.G)*7152 + uint32(c.B)*722) / 10000)
				c.R, c.G, c.B = v, v, v
			}
			out.SetNRGBA(x, y, c)
		}
	}
	return out, nil
}
func clamp(v float64) uint8 { return uint8(math.Max(0, math.Min(255, v))) }
