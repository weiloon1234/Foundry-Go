package imaging

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"
)

func TestShapesFillStrokeAndRoundJoins(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	red, blue := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}
	result, err := e.Create(t.Context(), 30, 30, color.NRGBA{}, NewPlan().Rectangle(4, 4, 20, 20, ShapeStyle{Fill: blue, Stroke: red, StrokeWidth: 4}))
	if err != nil {
		t.Fatal(err)
	}
	img := resultImage(t, result)
	assertPixel(t, img, 0, 0, color.NRGBA{})
	assertPixel(t, img, 15, 15, blue)
	assertPixel(t, img, 4, 15, red)
	assertPixel(t, img, 2, 15, red)
	assertPixel(t, img, 26, 15, color.NRGBA{})
	// Adjacent segments, round joins and caps share a coverage mask, so a
	// translucent stroke never becomes more opaque where outlines overlap.
	path := NewPath().MoveTo(4, 15).LineTo(15, 15).LineTo(15, 4)
	result, err = e.Create(t.Context(), 30, 30, color.NRGBA{}, NewPlan().Draw(path, ShapeStyle{Stroke: color.NRGBA{R: 255, A: 128}, StrokeWidth: 6}))
	if err != nil {
		t.Fatal(err)
	}
	img = resultImage(t, result)
	for y := 0; y < 30; y++ {
		for x := 0; x < 30; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a > 128*257 {
				t.Fatalf("stroke overlap changed opacity at %d,%d: %d", x, y, a)
			}
		}
	}
	assertPixel(t, img, 15, 15, color.NRGBA{R: 255, A: 128})
	assertPixel(t, img, 2, 15, color.NRGBA{R: 255, A: 128})
}

func TestVectorPathsHolesCurvesAndClipping(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	paint := color.NRGBA{G: 255, A: 255}
	path := NewPath().MoveTo(-4, -4).LineTo(24, -4).LineTo(24, 24).LineTo(-4, 24).Close().
		MoveTo(5, 5).LineTo(5, 15).LineTo(15, 15).LineTo(15, 5).Close()
	result, err := e.Create(t.Context(), 20, 20, color.NRGBA{}, NewPlan().Draw(path, ShapeStyle{Fill: paint}))
	if err != nil {
		t.Fatal(err)
	}
	img := resultImage(t, result)
	assertPixel(t, img, 0, 0, paint)
	assertPixel(t, img, 19, 19, paint)
	assertPixel(t, img, 10, 10, color.NRGBA{})
	for _, plan := range []Plan{
		NewPlan().Ellipse(10, 10, 8, 6, ShapeStyle{Fill: paint}),
		NewPlan().Circle(10, 10, 6, ShapeStyle{Fill: paint}),
		NewPlan().Draw(NewPath().MoveTo(2, 10).QuadraticTo(10, -2, 18, 10).CubicTo(18, 18, 2, 18, 2, 10).Close(), ShapeStyle{Fill: paint, Stroke: paint, StrokeWidth: 1}),
	} {
		result, err := e.Create(t.Context(), 20, 20, color.NRGBA{}, plan)
		if err != nil {
			t.Fatal(err)
		}
		img := resultImage(t, result)
		assertPixel(t, img, 10, 10, paint)
		assertPixel(t, img, 0, 0, color.NRGBA{})
	}
}

func TestDrawingSnapshotsAndLimits(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	points := []Point{{2, 2}, {18, 2}, {10, 18}}
	plan := NewPlan().Polygon(points, ShapeStyle{Fill: color.NRGBA{R: 255, A: 255}})
	points[0] = Point{math.NaN(), math.NaN()}
	if _, err := e.Create(t.Context(), 20, 20, color.NRGBA{}, plan); err != nil {
		t.Fatal(err)
	}
	base := NewPath().MoveTo(1, 1).LineTo(10, 1)
	a, b := base.LineTo(10, 10).Close(), base.LineTo(1, 10).Close()
	if a.commands[2].points[0] == b.commands[2].points[0] {
		t.Fatal("path builders alias")
	}
	for _, bad := range []Plan{
		NewPlan().Draw(Path{}, ShapeStyle{}),
		NewPlan().Draw(NewPath().LineTo(1, 1), ShapeStyle{}),
		NewPlan().Draw(NewPath().MoveTo(1, 1).Close().LineTo(2, 2), ShapeStyle{}),
		NewPlan().Circle(1, 1, -1, ShapeStyle{}),
		NewPlan().Rectangle(0, 0, 0, 10, ShapeStyle{}),
		NewPlan().Line(0, 0, 1, 1, math.NaN(), color.NRGBA{}),
		NewPlan().Polygon([]Point{{1, 1}, {2, 2}}, ShapeStyle{}),
		NewPlan().Draw(NewPath().MoveTo(math.Inf(1), 0), ShapeStyle{}),
	} {
		if err := bad.Validate(); err == nil {
			t.Fatal("invalid drawing accepted")
		}
	}
	path := NewPath().MoveTo(0, 0)
	for range MaxPathCommands {
		path = path.LineTo(1, 1)
	}
	if err := path.Validate(); err == nil {
		t.Fatal("path command limit ignored")
	}
	config := DefaultConfig()
	config.Limits.WorkingBytes = 10000
	config.Limits.OutputBytes = 1000
	if _, err := testEngine(t, config).Create(t.Context(), 20, 20, color.NRGBA{}, plan); err == nil {
		t.Fatal("drawing workspace limit ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := e.Create(ctx, 20, 20, color.NRGBA{}, plan); err == nil {
		t.Fatal("drawing cancellation ignored")
	}
	if _, err := e.Create(t.Context(), 20, 20, color.NRGBA{}, plan); err != nil {
		t.Fatal("drawing capacity not reusable", err)
	}
}

func TestCurveSubdivisionBudget(t *testing.T) {
	path := NewPath().MoveTo(0, 0)
	for range 50 {
		path = path.CubicTo(60000, 60000, -60000, -60000, 1, 1)
	}
	if _, err := flattenPath(path); err == nil {
		t.Fatal("aggregate curve subdivision limit ignored")
	}
}

func alphaBounds(img image.Image) image.Rectangle {
	var bounds image.Rectangle
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0 {
				bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return bounds
}
