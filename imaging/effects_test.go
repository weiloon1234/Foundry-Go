package imaging

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestAdditionalEffectsHaveObservablePixels(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	for _, test := range []struct {
		name string
		in   color.NRGBA
		plan Plan
		want color.NRGBA
	}{
		{"invert alpha", color.NRGBA{R: 100, G: 150, B: 200, A: 128}, NewPlan().Invert(), color.NRGBA{R: 155, G: 105, B: 55, A: 128}},
		{"gamma identity", color.NRGBA{R: 100, G: 150, B: 200, A: 128}, NewPlan().Gamma(1), color.NRGBA{R: 100, G: 150, B: 200, A: 128}},
		{"threshold white", color.NRGBA{R: 200, G: 200, B: 200, A: 128}, NewPlan().Threshold(50), color.NRGBA{R: 255, G: 255, B: 255, A: 128}},
		{"threshold black", color.NRGBA{R: 40, G: 40, B: 40, A: 128}, NewPlan().Threshold(50), color.NRGBA{A: 128}},
		{"hue red to green", color.NRGBA{R: 255, A: 255}, NewPlan().Hue(120), color.NRGBA{G: 255, A: 255}},
		{"sepia identity", color.NRGBA{R: 100, G: 150, B: 200, A: 128}, NewPlan().Sepia(0), color.NRGBA{R: 100, G: 150, B: 200, A: 128}},
		{"saturation identity", color.NRGBA{R: 100, G: 150, B: 200, A: 128}, NewPlan().Saturation(0), color.NRGBA{R: 100, G: 150, B: 200, A: 128}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := e.Create(t.Context(), 2, 2, test.in, test.plan)
			if err != nil {
				t.Fatal(err)
			}
			assertPixel(t, resultImage(t, result), 0, 0, test.want)
		})
	}
	input := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	input.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	input.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	input.SetNRGBA(0, 1, color.NRGBA{B: 255, A: 255})
	input.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	result, err := e.ProcessBytes(t.Context(), encodedPNG(t, input), NewPlan().Pixelate(2))
	if err != nil {
		t.Fatal(err)
	}
	for y := range 2 {
		for x := range 2 {
			assertPixel(t, resultImage(t, result), x, y, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
		}
	}
	// Sharpen should increase a soft edge's contrast without resizing it.
	gradient := image.NewNRGBA(image.Rect(0, 0, 5, 1))
	for x, v := range []uint8{30, 60, 128, 196, 226} {
		gradient.SetNRGBA(x, 0, color.NRGBA{R: v, G: v, B: v, A: 255})
	}
	result, err = e.ProcessBytes(t.Context(), encodedPNG(t, gradient), NewPlan().Sharpen(1, 1, 0))
	if err != nil {
		t.Fatal(err)
	}
	sharpened := resultImage(t, result)
	if c := color.NRGBAModel.Convert(sharpened.At(1, 0)).(color.NRGBA); c.R >= 60 || c.A != 255 {
		t.Fatal("sharpen did not increase edge contrast", c)
	}
}

func TestAdditionalEffectsRejectUnboundedParameters(t *testing.T) {
	for _, plan := range []Plan{NewPlan().Gamma(0), NewPlan().Gamma(math.Inf(1)), NewPlan().Saturation(101), NewPlan().Hue(-361), NewPlan().Sepia(-1), NewPlan().Threshold(101), NewPlan().Pixelate(0), NewPlan().Sharpen(0, 1, 0), NewPlan().Sharpen(1, 11, 0), NewPlan().Sharpen(1, 1, math.NaN())} {
		if plan.Validate() == nil {
			t.Fatal("invalid effect accepted")
		}
	}
}
