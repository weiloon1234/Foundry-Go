package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

func encodedPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func resultImage(t *testing.T, result Result) image.Image {
	t.Helper()
	img, err := png.Decode(result.Reader())
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func assertPixel(t *testing.T, img image.Image, x, y int, want color.NRGBA) {
	t.Helper()
	got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	if got != want {
		t.Fatalf("pixel (%d,%d): got %v, want %v", x, y, got, want)
	}
}

func TestSizingModesAndOneAxisResize(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	input := pngInput(t, 8, 4)
	for _, test := range []struct {
		name string
		plan Plan
		w, h int
	}{
		{"downsize independent axes", NewPlan().ResizeDown(20, 2), 8, 2},
		{"width", NewPlan().FitWidth(3, false), 3, 2},
		{"height", NewPlan().FitHeight(3, false), 6, 3},
		{"width enlarge", NewPlan().FitWidth(16, true), 16, 8},
		{"height enlarge", NewPlan().FitHeight(8, true), 16, 8},
		{"width no enlarge", NewPlan().FitWidth(16, false), 8, 4},
		{"height no enlarge", NewPlan().FitHeight(8, false), 8, 4},
		{"pad", NewPlan().Pad(12, 12, color.NRGBA{}, Center), 12, 12},
		{"contain", NewPlan().Contain(12, 12, color.NRGBA{}, Center), 12, 12},
		{"relative canvas", NewPlan().CanvasRelative(2, -1, color.NRGBA{}, Center), 10, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := e.ProcessBytes(t.Context(), input, test.plan)
			if err != nil {
				t.Fatal(err)
			}
			if got := resultImage(t, result).Bounds().Size(); got != image.Pt(test.w, test.h) {
				t.Fatalf("dimensions: got %v, want %dx%d", got, test.w, test.h)
			}
		})
	}
}

func TestPaddingUpscalePositionAndAlphaPixels(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	red, green := color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255}
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, red)
	source.SetNRGBA(1, 0, green)
	input := encodedPNG(t, source)
	for _, test := range []struct {
		name string
		plan Plan
		rows []string
	}{
		{"pad does not enlarge", NewPlan().Pad(4, 4, color.NRGBA{}, Center), []string{"....", ".RG.", "....", "...."}},
		{"contain enlarges", NewPlan().Contain(4, 4, color.NRGBA{}, Center), []string{"....", "RRGG", "RRGG", "...."}},
		{"pad bottom right", NewPlan().Pad(4, 4, color.NRGBA{}, BottomRight), []string{"....", "....", "....", "..RG"}},
		{"fill bottom right", NewPlan().FillAt(4, 4, true, BottomRight), []string{"GGGG", "GGGG", "GGGG", "GGGG"}},
		{"canvas does not resample", NewPlan().Canvas(4, 4, color.NRGBA{}, TopLeft), []string{"RG..", "....", "....", "...."}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := e.ProcessBytes(t.Context(), input, test.plan.Resampling(NearestNeighbor))
			if err != nil {
				t.Fatal(err)
			}
			img := resultImage(t, result)
			for y, row := range test.rows {
				for x, ch := range row {
					want := color.NRGBA{}
					if ch == 'R' {
						want = red
					} else if ch == 'G' {
						want = green
					}
					assertPixel(t, img, x, y, want)
				}
			}
		})
	}
}

func TestAllCropAndCanvasAnchors(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	source, err := png.Decode(bytes.NewReader(pngInput(t, 6, 6)))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		position Position
		x, y     int
	}{{Center, 2, 2}, {TopLeft, 0, 0}, {Top, 2, 0}, {TopRight, 4, 0}, {Left, 0, 2}, {Right, 4, 2}, {BottomLeft, 0, 4}, {Bottom, 2, 4}, {BottomRight, 4, 4}} {
		for _, plan := range []Plan{NewPlan().CropAt(2, 2, test.position), NewPlan().Canvas(2, 2, color.NRGBA{}, test.position), NewPlan().CanvasRelative(-4, -4, color.NRGBA{}, test.position)} {
			result, err := e.ProcessBytes(t.Context(), encodedPNG(t, source), plan)
			if err != nil {
				t.Fatal(err)
			}
			img := resultImage(t, result)
			for y := range 2 {
				for x := range 2 {
					assertPixel(t, img, x, y, color.NRGBAModel.Convert(source.At(x+test.x, y+test.y)).(color.NRGBA))
				}
			}
		}
	}
}

func TestRotationAndResampling(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	input := pngInput(t, 5, 3)
	for _, rotation := range []Rotation{Rotate90, Rotate180, Rotate270} {
		old, err := e.ProcessBytes(t.Context(), input, NewPlan().Rotate(rotation))
		if err != nil {
			t.Fatal(err)
		}
		for _, angle := range []float64{float64(rotation), float64(rotation) - 360} {
			result, err := e.ProcessBytes(t.Context(), input, NewPlan().RotateDegrees(angle, color.NRGBA{}))
			if err != nil || !bytes.Equal(result.Bytes(), old.Bytes()) {
				t.Fatal("quarter turn resampled or changed direction", angle, err)
			}
		}
	}
	red, blue := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}
	rotated, err := e.Create(t.Context(), 3, 3, red, NewPlan().RotateDegrees(45, blue))
	if err != nil || rotated.Info().Width != 5 || rotated.Info().Height != 5 {
		t.Fatal("expanded rotation dimensions", rotated.Info(), err)
	}
	assertPixel(t, resultImage(t, rotated), 0, 0, blue)
	assertPixel(t, resultImage(t, rotated), 2, 2, red)

	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, red)
	source.SetNRGBA(1, 0, blue)
	nearest, err := e.ProcessBytes(t.Context(), encodedPNG(t, source), NewPlan().Resize(8, 4).Resampling(NearestNeighbor))
	if err != nil {
		t.Fatal(err)
	}
	assertPixel(t, resultImage(t, nearest), 3, 0, red)
	assertPixel(t, resultImage(t, nearest), 4, 0, blue)
	linear, err := e.ProcessBytes(t.Context(), encodedPNG(t, source), NewPlan().Resampling(Linear).Resize(8, 4))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(linear.Bytes(), nearest.Bytes()) {
		t.Fatal("resampling selection was ignored")
	}
}

func TestNewGeometryValidationAndPreflight(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	for _, plan := range []Plan{
		NewPlan().FitWidth(0, true), NewPlan().FitHeight(-1, false), NewPlan().ResizeDown(0, 4),
		NewPlan().Pad(0, 1, color.NRGBA{}, Center), NewPlan().Canvas(2, 2, color.NRGBA{}, Position(99)),
		NewPlan().CanvasRelative(-8, 0, color.NRGBA{}, Center), NewPlan().CropAt(9, 1, Center),
		NewPlan().RotateDegrees(math.NaN(), color.NRGBA{}), NewPlan().RotateDegrees(361, color.NRGBA{}),
		NewPlan().Resampling(Resampling(255)), NewPlan().CanvasRelative(65536, 0, color.NRGBA{}, Center),
	} {
		result, err := e.ProcessBytes(t.Context(), pngInput(t, 8, 4), plan)
		if err == nil || result.Size() != 0 {
			t.Fatal("invalid geometry accepted")
		}
	}
	config := DefaultConfig()
	config.Limits.Width, config.Limits.Height, config.Limits.Pixels = 64, 64, 4096
	small := testEngine(t, config)
	for _, plan := range []Plan{NewPlan().FitHeight(64, true), NewPlan().FillAt(2, 2, true, Right), NewPlan().Canvas(65, 1, color.NRGBA{}, Center)} {
		if _, err := small.ProcessBytes(t.Context(), pngInput(t, 40, 1), plan); err == nil {
			t.Fatal("oversized intermediate or output accepted")
		}
	}
}
