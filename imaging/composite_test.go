package imaging

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

func solidResult(t *testing.T, e *Engine, w, h int, c color.NRGBA) Result {
	t.Helper()
	result, err := e.Create(t.Context(), w, h, c, NewPlan())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestInsertionPositionClippingOpacityAndSourceOwnership(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	red, blue := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}
	layer := solidResult(t, e, 2, 2, red)
	base := NewPlan().Insert(layer, Placement{Position: BottomRight, X: -1, Y: -1})
	copy := layer.Bytes()
	for i := range copy {
		copy[i] = 0
	}
	result, err := e.Create(t.Context(), 4, 4, blue, base)
	if err != nil {
		t.Fatal(err)
	}
	img := resultImage(t, result)
	for y := range 4 {
		for x := range 4 {
			want := blue
			if x >= 1 && x <= 2 && y >= 1 && y <= 2 {
				want = red
			}
			assertPixel(t, img, x, y, want)
		}
	}
	for _, test := range []struct {
		placement Placement
		want      color.NRGBA
	}{
		{Placement{Position: TopLeft, Opacity: value.Set(0.0)}, blue},
		{Placement{Position: TopLeft, Opacity: value.Set(0.5)}, color.NRGBA{R: 128, B: 128, A: 255}},
		{Placement{Position: TopLeft, X: -1, Y: -1}, red},
		{Placement{Position: BottomRight, X: 10}, blue},
	} {
		result, err := e.Create(t.Context(), 4, 4, blue, NewPlan().Insert(layer, test.placement))
		if err != nil {
			t.Fatal(err)
		}
		assertPixel(t, resultImage(t, result), 0, 0, test.want)
		if result.Info().Width != 4 || result.Info().Height != 4 {
			t.Fatal("insertion expanded canvas")
		}
	}
	assertPixel(t, resultImage(t, layer), 0, 0, red)
	if len(base.steps) != 1 || len(base.Invert().steps) != 2 {
		t.Fatal("composite plan was mutated")
	}
}

func TestBlendModesAndPartialAlpha(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	layer := solidResult(t, e, 1, 1, color.NRGBA{R: 204, G: 204, B: 204, A: 255})
	for _, test := range []struct {
		mode BlendMode
		want uint8
	}{{BlendNormal, 204}, {BlendMultiply, 51}, {BlendScreen, 217}, {BlendOverlay, 102}, {BlendDarken, 64}, {BlendLighten, 204}, {BlendDifference, 140}, {BlendExclusion, 166}} {
		result, err := e.Create(t.Context(), 1, 1, color.NRGBA{R: 64, G: 64, B: 64, A: 255}, NewPlan().Insert(layer, Placement{Blend: test.mode}))
		if err != nil {
			t.Fatal(err)
		}
		assertPixel(t, resultImage(t, result), 0, 0, color.NRGBA{R: test.want, G: test.want, B: test.want, A: 255})
	}
	// With a transparent backdrop, multiply must retain the source color,
	// rather than multiplying it by invisible RGB and turning it black.
	alphaLayer := solidResult(t, e, 1, 1, color.NRGBA{R: 255, A: 128})
	transparent, err := e.Create(t.Context(), 1, 1, color.NRGBA{}, NewPlan().Insert(alphaLayer, Placement{Blend: BlendMultiply}))
	if err != nil {
		t.Fatal(err)
	}
	assertPixel(t, resultImage(t, transparent), 0, 0, color.NRGBA{R: 255, A: 128})
	partial, err := e.Create(t.Context(), 1, 1, color.NRGBA{B: 255, A: 128}, NewPlan().Insert(alphaLayer, Placement{}))
	if err != nil {
		t.Fatal(err)
	}
	assertPixel(t, resultImage(t, partial), 0, 0, color.NRGBA{R: 170, B: 85, A: 192})
}

func TestMasksMultiplyExistingAlphaAndClearOutside(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	maskPixels := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	maskPixels.SetNRGBA(0, 0, color.NRGBA{A: 128})
	maskPixels.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	layer, err := e.ProcessBytes(t.Context(), encodedPNG(t, maskPixels), NewPlan())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		mode  MaskMode
		alpha uint8
	}{{AlphaMask, 64}, {LuminanceMask, 0}} {
		result, err := e.Create(t.Context(), 4, 1, color.NRGBA{R: 255, A: 128}, NewPlan().Mask(layer, Placement{X: 1, Position: TopLeft}, test.mode))
		if err != nil {
			t.Fatal(err)
		}
		img := resultImage(t, result)
		for x, alpha := range []uint8{0, test.alpha, 128, 0} {
			got := color.NRGBAModel.Convert(img.At(x, 0)).(color.NRGBA)
			if got.A != alpha || alpha != 0 && got.R != 255 {
				t.Fatal("mask alpha/color", test.mode, x, got)
			}
		}
	}
}

func TestCompositePreflightAndValidation(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	layer := solidResult(t, e, 16, 16, color.NRGBA{R: 255, A: 255})
	for _, plan := range []Plan{
		NewPlan().Insert(Result{}, Placement{}),
		NewPlan().Insert(layer, Placement{Position: Position(99)}),
		NewPlan().Insert(layer, Placement{Blend: BlendMode(99)}),
		NewPlan().Insert(layer, Placement{X: 65536}),
		NewPlan().Insert(layer, Placement{Opacity: value.Set(math.NaN())}),
		NewPlan().Insert(layer, Placement{Opacity: value.Set(-0.1)}),
		NewPlan().Mask(layer, Placement{}, MaskMode(99)),
		NewPlan().Mask(layer, Placement{Blend: BlendScreen}, AlphaMask),
	} {
		if plan.Validate() == nil {
			t.Fatal("invalid compositing declaration accepted")
		}
	}
	config := DefaultConfig()
	config.Limits.OutputBytes = 512
	config.Limits.WorkingBytes = 5000
	small := testEngine(t, config)
	if _, err := small.ProcessBytes(t.Context(), layer.Bytes(), NewPlan()); err != nil {
		t.Fatal("baseline image should fit", err)
	}
	if result, err := small.ProcessBytes(t.Context(), layer.Bytes(), NewPlan().Insert(layer, Placement{})); err == nil || result.Size() != 0 {
		t.Fatal("combined layer decode and retained canvas escaped memory limits")
	}
	config = DefaultConfig()
	config.Limits.InputBytes = layer.Size()*2 - 1
	if _, err := testEngine(t, config).ProcessBytes(t.Context(), layer.Bytes(), NewPlan().Insert(layer, Placement{})); err == nil {
		t.Fatal("aggregate input bytes not bounded")
	}
	// A failed plan releases admission for the next healthy call.
	if _, err := small.Create(t.Context(), 1, 1, color.NRGBA{}, NewPlan()); err != nil {
		t.Fatal("failed composite retained capacity", err)
	}
}

func TestImmutableCompositePlanConcurrentReuse(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	layer := solidResult(t, e, 2, 2, color.NRGBA{G: 255, A: 128})
	plan := NewPlan().Insert(layer, Placement{Position: BottomRight}).Canvas(8, 8, color.NRGBA{}, Center)
	var wg sync.WaitGroup
	results := make(chan Result, 12)
	errors := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			result, err := e.Create(t.Context(), 4, 4, color.NRGBA{R: 255, A: 255}, plan)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		})
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	var expected []byte
	for result := range results {
		if expected == nil {
			expected = result.Bytes()
		} else if !bytes.Equal(expected, result.Bytes()) {
			t.Fatal("concurrent plan or layer mutated")
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := e.Create(cancelled, 4, 4, color.NRGBA{}, plan); err == nil || result.Size() != 0 {
		t.Fatal("cancelled create published output")
	}
}

func TestPlanFormattingDoesNotExposeLayerBytes(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	layer := solidResult(t, e, 2, 2, color.NRGBA{R: 255, A: 255})
	plan := NewPlan().Insert(layer, Placement{})
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if got := fmt.Sprintf(format, plan); got != "image plan" {
			t.Fatal("plan formatting exposed its declaration or layer data")
		}
	}
}

func TestCompositeInputReadUsesRemainingAggregateBudget(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	layer := solidResult(t, e, 2, 2, color.NRGBA{R: 255, A: 255})
	config := DefaultConfig()
	config.Limits.InputBytes = layer.Size() + 10
	small := testEngine(t, config)
	reader := bytes.NewReader(make([]byte, 2048))
	if result, err := small.Process(t.Context(), reader, NewPlan().Insert(layer, Placement{})); err == nil || result.Size() != 0 {
		t.Fatal("oversized aggregate input accepted")
	}
	if reader.Len() != 2048-11 {
		t.Fatal("reader consumed more than the remaining input allowance plus one")
	}
	reader = bytes.NewReader(make([]byte, 2048))
	if _, err := small.Process(t.Context(), reader, NewPlan().Insert(layer, Placement{}).Insert(layer, Placement{})); err == nil || reader.Len() != 2048 {
		t.Fatal("over-budget layers read from the source")
	}
}
