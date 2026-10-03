package imaging

import (
	"context"
	"image"
)

// VP8 stores BT.601 video-range YCbCr. Go's image.YCbCr.RGBA uses full-range
// JPEG conversion, which would turn Y=235 white into gray. Convert explicitly,
// with centered bilinear 4:2:0 chroma interpolation and unassociated alpha.
// This is matrix/range conversion, not ICC profile color management.
func webpRGB(ctx context.Context, decoded image.Image) (*image.NRGBA, error) {
	var yuv *image.YCbCr
	var alpha *image.NYCbCrA
	switch img := decoded.(type) {
	case *image.YCbCr:
		yuv = img
	case *image.NYCbCrA:
		yuv, alpha = &img.YCbCr, img
	default:
		return nil, invalid("unexpected WebP pixel model")
	}
	bounds := yuv.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	chromaWidth, chromaHeight := (bounds.Dx()+1)/2, (bounds.Dy()+1)/2
	for y := 0; y < bounds.Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < bounds.Dx(); x++ {
			luma := int(yuv.Y[yuv.YOffset(x+bounds.Min.X, y+bounds.Min.Y)]) - 16
			cb := webpChroma(yuv.Cb, yuv.CStride, chromaWidth, chromaHeight, x, y) - 128
			cr := webpChroma(yuv.Cr, yuv.CStride, chromaWidth, chromaHeight, x, y) - 128
			// Standard BT.601 coefficients with 16-bit fixed-point precision.
			i := out.PixOffset(x, y)
			out.Pix[i] = webpColorByte(76309*luma + 104597*cr)
			out.Pix[i+1] = webpColorByte(76309*luma - 25675*cb - 53279*cr)
			out.Pix[i+2] = webpColorByte(76309*luma + 132201*cb)
			out.Pix[i+3] = 255
			if alpha != nil {
				out.Pix[i+3] = alpha.A[alpha.AOffset(x+bounds.Min.X, y+bounds.Min.Y)]
			}
		}
	}
	return out, nil
}

func webpColorByte(n int) byte {
	return byte(min(255, max(0, (n+1<<15)>>16)))
}

func webpChroma(plane []byte, stride, width, height, x, y int) int {
	cx, cy := x/2, y/2
	nx, ny := cx-1, cy-1
	if x&1 != 0 {
		nx = cx + 1
	}
	if y&1 != 0 {
		ny = cy + 1
	}
	nx, ny = max(0, min(width-1, nx)), max(0, min(height-1, ny))
	return (9*int(plane[cy*stride+cx]) + 3*int(plane[cy*stride+nx]) + 3*int(plane[ny*stride+cx]) + int(plane[ny*stride+nx]) + 8) / 16
}
