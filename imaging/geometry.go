package imaging

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/disintegration/gift"
)

// Position anchors an image or crop rectangle. The zero value is Center.
type Position uint8

const (
	Center Position = iota
	TopLeft
	Top
	TopRight
	Left
	Right
	BottomLeft
	Bottom
	BottomRight
)

func (p Position) point(outer, inner image.Rectangle) image.Point {
	x, y := (outer.Dx()-inner.Dx())/2, (outer.Dy()-inner.Dy())/2
	switch p {
	case TopLeft, Left, BottomLeft:
		x = 0
	case TopRight, Right, BottomRight:
		x = outer.Dx() - inner.Dx()
	}
	switch p {
	case TopLeft, Top, TopRight:
		y = 0
	case BottomLeft, Bottom, BottomRight:
		y = outer.Dy() - inner.Dy()
	}
	return outer.Min.Add(image.Pt(x, y))
}

// Resampling selects the reconstruction filter used by every resize in a plan.
// Lanczos is the default. NearestNeighbor is useful for pixel art.
type Resampling uint8

const (
	Lanczos Resampling = iota
	Cubic
	Linear
	Box
	NearestNeighbor
)

func (r Resampling) filter() gift.Resampling {
	switch r {
	case Cubic:
		return gift.CubicResampling
	case Linear:
		return gift.LinearResampling
	case Box:
		return gift.BoxResampling
	case NearestNeighbor:
		return gift.NearestNeighborResampling
	default:
		return gift.LanczosResampling
	}
}

func (p Plan) Resampling(r Resampling) Plan { p.resampling = r; return p }

// ResizeDown resizes each axis independently, without enlarging either axis.
func (p Plan) ResizeDown(width, height int) Plan {
	return p.append(step{kind: resizeDown, width: width, height: height})
}

// FitWidth preserves the aspect ratio while targeting one width.
func (p Plan) FitWidth(width int, upscale bool) Plan {
	return p.append(step{kind: resizeWidth, width: width, upscale: upscale})
}

// FitHeight preserves the aspect ratio while targeting one height.
func (p Plan) FitHeight(height int, upscale bool) Plan {
	return p.append(step{kind: resizeHeight, width: height, upscale: upscale})
}

// FillAt resizes to cover the target and crops at position. A target requiring
// enlargement fails when upscale is false, just as with Fill.
func (p Plan) FillAt(width, height int, upscale bool, position Position) Plan {
	return p.append(step{kind: resizeFill, width: width, height: height, upscale: upscale, position: position})
}

// CropAt takes a positioned rectangle without resampling. It must fit inside
// the input; use Canvas to extend the image instead.
func (p Plan) CropAt(width, height int, position Position) Plan {
	return p.append(step{kind: cropPositioned, width: width, height: height, position: position})
}

// Pad fits without enlargement into an exact canvas and fills unused pixels.
// background may be transparent.
func (p Plan) Pad(width, height int, background color.NRGBA, position Position) Plan {
	return p.append(step{kind: resizePad, width: width, height: height, background: background, position: position})
}

// Contain is Pad with enlargement enabled.
func (p Plan) Contain(width, height int, background color.NRGBA, position Position) Plan {
	return p.append(step{kind: resizePad, width: width, height: height, background: background, position: position, upscale: true})
}

// Canvas changes the canvas without resampling, clipping or extending around
// the anchor. background fills new pixels and may be transparent.
func (p Plan) Canvas(width, height int, background color.NRGBA, position Position) Plan {
	return p.append(step{kind: resizeCanvas, width: width, height: height, background: background, position: position})
}

// CanvasRelative adds signed deltas to the current canvas size.
func (p Plan) CanvasRelative(width, height int, background color.NRGBA, position Position) Plan {
	return p.append(step{kind: resizeCanvasRelative, width: width, height: height, background: background, position: position})
}

// RotateDegrees rotates clockwise, expanding the canvas to contain the image.
// Angles are finite in [-360,360]. Cubic interpolation is used, except exact
// quarter-turns, which retain pixels without interpolation.
func (p Plan) RotateDegrees(degrees float64, background color.NRGBA) Plan {
	return p.append(step{kind: rotateDegrees, number: degrees, background: background})
}

func (s step) rotationFilter() gift.Filter {
	angle := math.Mod(s.number+360, 360)
	switch angle {
	case 0:
		return gift.Rotate(0, s.background, gift.CubicInterpolation)
	case 90:
		return gift.Rotate270()
	case 180:
		return gift.Rotate180()
	case 270:
		return gift.Rotate90()
	default:
		return gift.Rotate(float32(-angle), s.background, gift.CubicInterpolation)
	}
}

// resizedSize is shared by allocation preflight and execution. It returns the
// resampled size before a fill crop or padding canvas is applied.
func (s step) resizedSize(bounds image.Rectangle) (int, int, error) {
	w, h := bounds.Dx(), bounds.Dy()
	switch s.kind {
	case resizeExact:
		return s.width, s.height, nil
	case resizeDown:
		return min(w, s.width), min(h, s.height), nil
	}
	var scale float64
	switch s.kind {
	case resizeWidth:
		scale = float64(s.width) / float64(w)
	case resizeHeight:
		scale = float64(s.width) / float64(h)
	default:
		scale = math.Min(float64(s.width)/float64(w), float64(s.height)/float64(h))
		if s.kind == resizeFill || s.kind == resizeSmart {
			scale = math.Max(float64(s.width)/float64(w), float64(s.height)/float64(h))
		}
	}
	if !s.upscale && scale > 1 {
		if s.kind == resizeFill || s.kind == resizeSmart {
			return 0, 0, invalid("fill requires upscaling to reach its declared size")
		}
		scale = 1
	}
	return max(1, int(math.Floor(float64(w)*scale+0.5))), max(1, int(math.Floor(float64(h)*scale+0.5))), nil
}

// dimensions includes intermediate canvases and horizontal resampling scratch.
func (s step) dimensions(bounds image.Rectangle, limits Limits) (image.Rectangle, int64, error) {
	w, h := bounds.Dx(), bounds.Dy()
	if err := limits.dimensions(w, h); err != nil {
		return image.Rectangle{}, 0, err
	}
	peak := int64(w) * int64(h)
	switch s.kind {
	case resizeExact, resizeDown, resizeFit, resizeFill, resizeSmart, resizeWidth, resizeHeight, resizePad:
		var err error
		w, h, err = s.resizedSize(bounds)
		if err != nil {
			return image.Rectangle{}, 0, err
		}
		if err := limits.dimensions(w, h); err != nil {
			return image.Rectangle{}, 0, err
		}
		peak = max(peak, int64(w)*int64(h), int64(w)*int64(bounds.Dy()))
		if s.kind == resizeFill || s.kind == resizeSmart || s.kind == resizePad {
			w, h = s.width, s.height
		}
	case crop, cropPositioned:
		if s.x > w-s.width || s.y > h-s.height {
			return image.Rectangle{}, 0, invalid("crop is outside the image")
		}
		w, h = s.width, s.height
	case resizeCanvas:
		w, h = s.width, s.height
	case resizeCanvasRelative:
		w, h = w+s.width, h+s.height
	case rotate:
		if s.number != 180 {
			w, h = h, w
		}
	case rotateDegrees:
		next := s.rotationFilter().Bounds(bounds)
		w, h = next.Dx(), next.Dy()
	}
	if err := limits.dimensions(w, h); err != nil {
		return image.Rectangle{}, 0, err
	}
	return image.Rect(0, 0, w, h), max(peak, int64(w)*int64(h)), nil
}

// auxiliaryBytes covers GIFT's resampling weights and pixel rows/columns in
// addition to its full-canvas scratch. These dominate for very thin images.
// Operations run serially; the larger axis phase determines the peak.
func (s step) auxiliaryBytes(bounds image.Rectangle) (int64, error) {
	switch s.kind {
	case resizeExact, resizeDown, resizeFit, resizeFill, resizeSmart, resizeWidth, resizeHeight, resizePad:
		w, h, err := s.resizedSize(bounds)
		if err != nil {
			return 0, err
		}
		if s.resampling == NearestNeighbor {
			return 0, nil
		}
		support := float64(s.resampling.filter().Support())
		axis := func(source, target int) int64 {
			if source == target {
				return 0
			}
			// GIFT reserves 2*(ceil(scale*support)+2) weight slots per
			// output coordinate. One extra radius unit covers float32 rounding.
			radius := int64(math.Ceil(math.Max(1, float64(source)/float64(target))*support)) + 1
			return int64(target)*(24+2*(radius+2)*16) + int64(source+target)*16
		}
		return max(axis(bounds.Dx(), w), axis(bounds.Dy(), h)), nil
	case blur, sharpen:
		kernel := int64(math.Ceil(s.number*3))*2 + 1
		// A float32 kernel, conservatively doubled-capacity 16-byte
		// convolution weights, and two 16-byte pixel rows/columns.
		return kernel*36 + int64(max(bounds.Dx(), bounds.Dy()))*32, nil
	case drawPath:
		return maxPathVertices * 32, nil
	case drawText:
		return textWorkspaceBytes, nil
	default:
		return 0, nil
	}
}

func resizeOntoCanvas(ctx context.Context, img image.Image, s step, bounds image.Rectangle, l Limits) (image.Image, error) {
	w, h, err := s.resizedSize(img.Bounds())
	if err != nil {
		return nil, err
	}
	resized, err := drawFilter(ctx, img, gift.Resize(w, h, s.resampling.filter()), l)
	if err != nil {
		return nil, err
	}
	if s.kind == resizeSmart {
		return smartCropNative(ctx, resized, bounds.Dx(), bounds.Dy(), s.interest, l)
	}
	if s.kind == resizeFill {
		point := s.position.point(resized.Bounds(), bounds)
		return drawFilter(ctx, resized, gift.Crop(bounds.Add(point)), l)
	}
	return drawCanvas(ctx, resized, bounds, s.background, s.position)
}

func drawCanvas(ctx context.Context, img image.Image, bounds image.Rectangle, background color.NRGBA, position Position) (image.Image, error) {
	out := image.NewNRGBA(bounds)
	draw.Draw(out, bounds, image.NewUniform(background), image.Point{}, draw.Src)
	point := position.point(bounds, img.Bounds())
	target := image.Rectangle{Min: point, Max: point.Add(img.Bounds().Size())}.Intersect(bounds)
	for y := target.Min.Y; y < target.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := image.Rect(target.Min.X, y, target.Max.X, y+1)
		draw.Draw(out, row, img, row.Min.Sub(point).Add(img.Bounds().Min), draw.Over)
	}
	return out, ctx.Err()
}
