//go:build foundry_vips && cgo

package profiles_test

import (
	"context"
	"testing"

	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/application"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/validation"
	imagingvalidation "github.com/weiloon1234/Foundry-Go/validation/imaging"
)

func TestConfiguredNativeImageService(t *testing.T) {
	app, err := application.New(profiles.NativeImageSettings()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	services := app.Resources()
	engine, err := services.Image()
	if err != nil {
		t.Fatal(err)
	}
	if engine.Capabilities().Backend != imaging.LibvipsBackend {
		t.Fatal("configured backend ignored")
	}
	rule := imagingvalidation.DimensionsWithEngine[foundryhttp.UploadedFile](engine, validation.DimensionConstraints{MaxWidth: 1024, MaxHeight: 1024})
	if err := rule.Validate(); err != nil {
		t.Fatal(err)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="80" height="40"><rect width="80" height="40" fill="#0080ff"/></svg>`)
	out, err := engine.ProcessBytes(t.Context(), svg, profiles.NativePortraitPlan())
	if err != nil || out.Info().Width != 64 || out.Info().Height != 64 || out.Info().Format != imaging.JPEG {
		t.Fatal("configured native portrait", err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.Done():
	default:
		t.Fatal("application did not close its native engine")
	}
}
