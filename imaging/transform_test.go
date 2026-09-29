package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"testing"
)

// referenceAdjust is the per-pixel formula the row implementation must match.
func referenceAdjust(c color.NRGBA, s step) color.NRGBA {
	switch s.kind {
	case brightness:
		c.R, c.G, c.B = clamp(float64(c.R)+s.number), clamp(float64(c.G)+s.number), clamp(float64(c.B)+s.number)
	case contrast:
		factor := math.Pow((100+s.number)/100, 2)
		adjust := func(v uint8) uint8 { return clamp(((float64(v)/255-0.5)*factor + 0.5) * 255) }
		c.R, c.G, c.B = adjust(c.R), adjust(c.G), adjust(c.B)
	case grayscale:
		v := uint8((uint32(c.R)*2126 + uint32(c.G)*7152 + uint32(c.B)*722) / 10000)
		c.R, c.G, c.B = v, v, v
	}
	return c
}

func TestColorAdjustmentsMatchPerPixelFormula(t *testing.T) {
	pattern := func() *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, 37, 11))
		for y := range 11 {
			for x := range 37 {
				img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 23), B: uint8(x*y + 3), A: uint8(255 - x)})
			}
		}
		return img
	}
	for _, s := range []step{{kind: brightness, number: 37}, {kind: brightness, number: -80}, {kind: contrast, number: 45}, {kind: contrast, number: -60}, {kind: grayscale}} {
		source := pattern()
		want := pattern()
		got, err := adjustPixels(t.Context(), source, s)
		if err != nil {
			t.Fatal(err)
		}
		for y := range 11 {
			for x := range 37 {
				expected := referenceAdjust(want.NRGBAAt(x, y), s)
				if actual := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA); actual != expected {
					t.Fatal("row adjustment differs from the per-pixel formula", s.kind, x, y, actual, expected)
				}
			}
		}
	}
	// Other decoded color models convert once, with the same result for opaque pixels.
	gray := image.NewGray(image.Rect(0, 0, 9, 9))
	for i := range gray.Pix {
		gray.Pix[i] = uint8(i * 3)
	}
	got, err := adjustPixels(t.Context(), gray, step{kind: brightness, number: 20})
	if err != nil {
		t.Fatal(err)
	}
	for y := range 9 {
		for x := range 9 {
			expected := referenceAdjust(color.NRGBAModel.Convert(gray.At(x, y)).(color.NRGBA), step{kind: brightness, number: 20})
			if actual := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA); actual != expected {
				t.Fatal("converted adjustment differs", x, y, actual, expected)
			}
		}
	}
	if gray.Pix[1] != 3 {
		t.Fatal("adjustment mutated a non-owned source model")
	}
}

func TestBackgroundFlattensTransparencyBeforeJPEG(t *testing.T) {
	transparent := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	var input bytes.Buffer
	if err := encode(&input, transparent, PNG, 0, 0, 1<<20); err != nil {
		t.Fatal(err)
	}
	e := testEngine(t, DefaultConfig())
	for _, test := range []struct {
		plan  Plan
		white bool
	}{{NewPlan().Format(JPEG), false}, {NewPlan().Format(JPEG).Background(color.NRGBA{R: 255, G: 255, B: 255, A: 255}), true}} {
		result, err := e.ProcessBytes(t.Context(), input.Bytes(), test.plan)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := jpeg.Decode(result.Reader())
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, _ := decoded.At(4, 4).RGBA()
		if white := r>>8 > 240 && g>>8 > 240 && b>>8 > 240; white != test.white {
			t.Fatal("background flattening changed", test.white, r>>8, g>>8, b>>8)
		}
	}
}

func TestAVIFQualityAndBackgroundValidation(t *testing.T) {
	for _, plan := range []Plan{NewPlan().AVIFQuality(80), NewPlan().Format(JPEG).AVIFQuality(80), NewPlan().Format(AVIF).AVIFQuality(101), NewPlan().Format(AVIF).AVIFQuality(-1), NewPlan().Background(color.NRGBA{R: 1, A: 128})} {
		if plan.Validate() == nil {
			t.Fatal("invalid output plan accepted", plan)
		}
	}
	if err := NewPlan().Format(AVIF).AVIFQuality(100).Background(color.NRGBA{A: 255}).Validate(); err != nil {
		t.Fatal(err)
	}
	e := testEngine(t, DefaultConfig())
	input := pngInput(t, 24, 24)
	lossless, err := e.ProcessBytes(t.Context(), input, NewPlan().Format(AVIF).AVIFQuality(100))
	if err != nil {
		t.Fatal(err)
	}
	compact, err := e.ProcessBytes(t.Context(), input, NewPlan().Format(AVIF).AVIFQuality(5))
	if err != nil {
		t.Fatal(err)
	}
	if compact.Size() >= lossless.Size() {
		t.Fatal("AVIF quality was not applied", compact.Size(), lossless.Size())
	}
}
