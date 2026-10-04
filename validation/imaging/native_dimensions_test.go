//go:build cgo && (darwin || linux || freebsd || windows)

package imaging_test

import (
	"context"
	"errors"
	"image/color"
	"os"
	"testing"

	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/validation"
	imagingvalidation "github.com/weiloon1234/Foundry-Go/validation/imaging"
)

func TestNativeDimensionsBorrowConfiguredEngine(t *testing.T) {
	config := imaging.DefaultConfig()
	config.Backend = imaging.LibvipsBackend
	engine, err := imaging.New(config)
	if err != nil {
		if os.Getenv("FOUNDRY_TEST_VIPS_REQUIRED") == "1" {
			t.Fatal(err)
		}
		t.Skip("libvips runtime unavailable")
	}
	t.Cleanup(func() { _ = engine.Close(context.Background()) })
	result, err := engine.Create(t.Context(), 32, 16, color.NRGBA{A: 255}, imaging.NewPlan().Format(imaging.HEIF))
	if err != nil {
		t.Fatal(err)
	}
	file := capturedFile{data: result.Bytes()} // MIME and extension deliberately claim PNG.
	constraints := validation.DimensionConstraints{Width: 32, Height: 16}
	rule := imagingvalidation.DimensionsWithEngine[capturedFile](engine, constraints)
	if err := rule.Check(t.Context(), file, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	var rejected *validation.Errors
	if err := imagingvalidation.Dimensions[capturedFile](config.Limits, constraints).Check(t.Context(), file, validation.DefaultLimits()); !errors.As(err, &rejected) {
		t.Fatal("portable inspection unexpectedly decoded native input", err)
	}
	if err := imagingvalidation.DimensionsWithEngine[capturedFile](nil, constraints).Validate(); err == nil {
		t.Fatal("nil engine accepted")
	}
	if err := engine.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := imagingvalidation.NewEngineMeasurer[capturedFile](engine).Measure(t.Context(), file); err == nil || ok {
		t.Fatal("closed engine accepted inspection")
	}
}
