package imaging

import (
	"image/color"
	"math"
	"slices"
)

// Drawing coordinates are measured in pixels from the top-left canvas corner.
// Finite coordinates in [-65535,65535] are accepted and clipped to the canvas.
type Point struct{ X, Y float64 }

const MaxPathCommands = 1024

type pathOperation uint8

const (
	pathMove pathOperation = iota
	pathLine
	pathQuad
	pathCubic
	pathClose
)

type pathCommand struct {
	op     pathOperation
	points [3]Point
}

// Path is immutable. Each builder returns an independent path. Closed contours
// use nonzero winding; reverse an inner contour's direction to make a hole.
type Path struct {
	commands []pathCommand
	invalid  bool
}

func NewPath() Path { return Path{} }
func (p Path) append(c pathCommand) Path {
	if len(p.commands) >= MaxPathCommands {
		p.invalid = true
		return p
	}
	p.commands = append(slices.Clone(p.commands), c)
	return p
}
func (p Path) MoveTo(x, y float64) Path {
	return p.append(pathCommand{op: pathMove, points: [3]Point{{x, y}}})
}
func (p Path) LineTo(x, y float64) Path {
	return p.append(pathCommand{op: pathLine, points: [3]Point{{x, y}}})
}
func (p Path) QuadraticTo(cx, cy, x, y float64) Path {
	return p.append(pathCommand{op: pathQuad, points: [3]Point{{cx, cy}, {x, y}}})
}
func (p Path) CubicTo(cx1, cy1, cx2, cy2, x, y float64) Path {
	return p.append(pathCommand{op: pathCubic, points: [3]Point{{cx1, cy1}, {cx2, cy2}, {x, y}}})
}
func (p Path) Close() Path { return p.append(pathCommand{op: pathClose}) }

func (p Path) Validate() error {
	if p.invalid || len(p.commands) == 0 || len(p.commands) > MaxPathCommands {
		return invalid("invalid image path")
	}
	open := false
	for _, c := range p.commands {
		for _, pt := range c.points {
			if !finiteRange(pt.X, -65535, 65535) || !finiteRange(pt.Y, -65535, 65535) {
				return invalid("invalid image drawing coordinate")
			}
		}
		switch c.op {
		case pathMove:
			open = true
		case pathLine, pathQuad, pathCubic, pathClose:
			if !open {
				return invalid("image path must begin with MoveTo")
			}
			if c.op == pathClose {
				open = false
			}
		default:
			return invalid("invalid image path command")
		}
	}
	return nil
}

func finiteRange(v, min, max float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= min && v <= max
}

// ShapeStyle draws the fill first, then a centered stroke with round joins and
// round caps. Transparent colors and a zero stroke width disable that paint.
type ShapeStyle struct {
	Fill        color.NRGBA
	Stroke      color.NRGBA
	StrokeWidth float64
}

func (s ShapeStyle) Validate() error {
	if !finiteRange(s.StrokeWidth, 0, 4096) {
		return invalid("invalid image stroke width")
	}
	return nil
}

// Draw adds an antialiased vector path. Open contours are closed for filling;
// strokes close only contours with an explicit Close command.
func (p Plan) Draw(path Path, style ShapeStyle) Plan {
	return p.append(step{kind: drawPath, path: path, style: style})
}

func (p Plan) Rectangle(x, y, width, height float64, style ShapeStyle) Plan {
	path := NewPath().MoveTo(x, y).LineTo(x+width, y).LineTo(x+width, y+height).LineTo(x, y+height).Close()
	path.invalid = width <= 0 || height <= 0
	return p.Draw(path, style)
}

// Ellipse uses a center and positive horizontal/vertical radii.
func (p Plan) Ellipse(x, y, radiusX, radiusY float64, style ShapeStyle) Plan {
	path := ellipsePath(x, y, radiusX, radiusY)
	path.invalid = radiusX <= 0 || radiusY <= 0
	return p.Draw(path, style)
}
func (p Plan) Circle(x, y, radius float64, style ShapeStyle) Plan {
	return p.Ellipse(x, y, radius, radius, style)
}

// Polygon snapshots points. At least three points are required.
func (p Plan) Polygon(points []Point, style ShapeStyle) Plan {
	path := NewPath()
	if len(points) < 3 || len(points)+1 > MaxPathCommands {
		path.invalid = true
		return p.Draw(path, style)
	}
	path = path.MoveTo(points[0].X, points[0].Y)
	for _, pt := range points[1:] {
		path = path.LineTo(pt.X, pt.Y)
	}
	return p.Draw(path.Close(), style)
}

func (p Plan) Line(x1, y1, x2, y2, width float64, c color.NRGBA) Plan {
	return p.Draw(NewPath().MoveTo(x1, y1).LineTo(x2, y2), ShapeStyle{Stroke: c, StrokeWidth: width})
}

func ellipsePath(x, y, rx, ry float64) Path {
	const k = 0.5522847498307936
	return NewPath().MoveTo(x+rx, y).
		CubicTo(x+rx, y+k*ry, x+k*rx, y+ry, x, y+ry).
		CubicTo(x-k*rx, y+ry, x-rx, y+k*ry, x-rx, y).
		CubicTo(x-rx, y-k*ry, x-k*rx, y-ry, x, y-ry).
		CubicTo(x+k*rx, y-ry, x+rx, y-k*ry, x+rx, y).Close()
}
