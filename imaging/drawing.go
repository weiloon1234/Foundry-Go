package imaging

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"
)

const maxPathVertices = 8192

func (s step) preflightDrawing(ctx context.Context, l Limits) error {
	switch s.kind {
	case drawPath:
		if s.style.StrokeWidth > 0 && s.style.Stroke.A != 0 {
			_, err := flattenPath(s.path)
			return err
		}
	case drawText:
		layout, err := layoutText(ctx, s.text, l)
		if err != nil {
			return err
		}
		return visitGlyphs(ctx, s.text, layout, image.Point{}, nil)
	}
	return nil
}

func drawShape(ctx context.Context, img image.Image, s step) (image.Image, error) {
	out := ownedNRGBA(img)
	if s.style.Fill.A != 0 {
		z := vector.NewRasterizer(out.Bounds().Dx(), out.Bounds().Dy())
		rasterPath(z, s.path)
		if err := paintRaster(ctx, out, z, s.style.Fill); err != nil {
			return nil, err
		}
	}
	if s.style.Stroke.A != 0 && s.style.StrokeWidth > 0 {
		contours, err := flattenPath(s.path)
		if err != nil {
			return nil, err
		}
		z := vector.NewRasterizer(out.Bounds().Dx(), out.Bounds().Dy())
		r := s.style.StrokeWidth / 2
		for _, contour := range contours {
			for i, pt := range contour {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				// All outlines have the same winding, so overlapping joins
				// form one coverage mask instead of accumulating opacity.
				rasterPath(z, ellipsePath(pt.X, pt.Y, r, r))
				if i == 0 {
					continue
				}
				from := contour[i-1]
				dx, dy := pt.X-from.X, pt.Y-from.Y
				length := math.Hypot(dx, dy)
				if length == 0 {
					continue
				}
				x, y := -dy*r/length, dx*r/length
				z.MoveTo(float32(from.X+x), float32(from.Y+y))
				z.LineTo(float32(from.X-x), float32(from.Y-y))
				z.LineTo(float32(pt.X-x), float32(pt.Y-y))
				z.LineTo(float32(pt.X+x), float32(pt.Y+y))
				z.ClosePath()
			}
		}
		if err := paintRaster(ctx, out, z, s.style.Stroke); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func rasterPath(z *vector.Rasterizer, path Path) {
	open := false
	for _, c := range path.commands {
		a, b, d := c.points[0], c.points[1], c.points[2]
		switch c.op {
		case pathMove:
			if open {
				z.ClosePath()
			}
			z.MoveTo(float32(a.X), float32(a.Y))
			open = true
		case pathLine:
			z.LineTo(float32(a.X), float32(a.Y))
		case pathQuad:
			z.QuadTo(float32(a.X), float32(a.Y), float32(b.X), float32(b.Y))
		case pathCubic:
			z.CubeTo(float32(a.X), float32(a.Y), float32(b.X), float32(b.Y), float32(d.X), float32(d.Y))
		case pathClose:
			z.ClosePath()
			open = false
		}
	}
	if open {
		z.ClosePath()
	}
}

func paintRaster(ctx context.Context, out *image.NRGBA, z *vector.Rasterizer, c color.NRGBA) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	coverage := image.NewAlpha(out.Bounds())
	z.DrawOp = draw.Src
	z.Draw(coverage, coverage.Bounds(), image.Opaque, image.Point{})
	for y := range out.Bounds().Dy() {
		if err := ctx.Err(); err != nil {
			return err
		}
		for x := range out.Bounds().Dx() {
			a := coverage.AlphaAt(x, y).A
			if a == 0 {
				continue
			}
			out.SetNRGBA(x, y, blendPixel(out.NRGBAAt(x, y), c, float64(a)/255, BlendNormal))
		}
	}
	return nil
}

// flattenPath is used only for stroking. Curves subdivide to quarter-pixel
// flatness, with a depth and aggregate vertex cap checked before rasterization.
func flattenPath(path Path) ([][]Point, error) {
	var contours [][]Point
	var contour []Point
	count := 0
	push := func(p Point) error {
		count++
		if count > maxPathVertices {
			return limited()
		}
		contour = append(contour, p)
		return nil
	}
	var curve func(Point, Point, Point, Point, int) error
	curve = func(a, b, c, d Point, depth int) error {
		// Control-polygon deviation from a uniformly parameterized line also
		// detects reversals and collinear overshoot, unlike distance alone.
		flat := math.Max(math.Hypot(b.X-(2*a.X+d.X)/3, b.Y-(2*a.Y+d.Y)/3), math.Hypot(c.X-(a.X+2*d.X)/3, c.Y-(a.Y+2*d.Y)/3))
		if flat <= 0.25 {
			return push(d)
		}
		if depth >= 16 {
			return limited()
		}
		ab, bc, cd := midpoint(a, b), midpoint(b, c), midpoint(c, d)
		abc, bcd := midpoint(ab, bc), midpoint(bc, cd)
		middle := midpoint(abc, bcd)
		if err := curve(a, ab, abc, middle, depth+1); err != nil {
			return err
		}
		return curve(middle, bcd, cd, d, depth+1)
	}
	for _, command := range path.commands {
		a, b, c := command.points[0], command.points[1], command.points[2]
		switch command.op {
		case pathMove:
			if len(contour) > 0 {
				contours = append(contours, contour)
			}
			contour = nil
			if err := push(a); err != nil {
				return nil, err
			}
		case pathLine:
			if err := push(a); err != nil {
				return nil, err
			}
		case pathQuad:
			from := contour[len(contour)-1]
			c1 := Point{from.X + 2*(a.X-from.X)/3, from.Y + 2*(a.Y-from.Y)/3}
			c2 := Point{b.X + 2*(a.X-b.X)/3, b.Y + 2*(a.Y-b.Y)/3}
			if err := curve(from, c1, c2, b, 0); err != nil {
				return nil, err
			}
		case pathCubic:
			if err := curve(contour[len(contour)-1], a, b, c, 0); err != nil {
				return nil, err
			}
		case pathClose:
			if err := push(contour[0]); err != nil {
				return nil, err
			}
			contours = append(contours, contour)
			contour = nil
		}
	}
	if len(contour) > 0 {
		contours = append(contours, contour)
	}
	return contours, nil
}

func midpoint(a, b Point) Point { return Point{(a.X + b.X) / 2, (a.Y + b.Y) / 2} }
