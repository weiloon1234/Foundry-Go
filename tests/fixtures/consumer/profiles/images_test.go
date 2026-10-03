package profiles_test

import (
	"context"
	"image/color"
	"image/png"
	"testing"

	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/imaging"
)

func TestProfileCardUsesOnlyFoundryImageAPIs(t *testing.T) {
	engine, err := imaging.New(imaging.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	watermark, err := engine.Create(t.Context(), 8, 8, color.NRGBA{G: 255, A: 255}, imaging.NewPlan())
	if err != nil {
		t.Fatal(err)
	}
	photo, err := engine.Create(t.Context(), 120, 80, color.NRGBA{R: 255, A: 255}, imaging.NewPlan())
	if err != nil {
		t.Fatal(err)
	}
	plan := profiles.ProfileCardPlan(watermark)
	result, err := engine.Process(t.Context(), photo.Reader(), plan)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(result.Reader())
	if err != nil || result.Info().Width != 1200 || result.Info().Height != 630 {
		t.Fatal("profile card output", result.Info(), err)
	}
	for _, test := range []struct {
		x, y int
		want color.NRGBA
	}{{0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255}}, {600, 315, color.NRGBA{R: 255, A: 255}}, {1176, 606, color.NRGBA{R: 64, G: 255, B: 64, A: 255}}} {
		if got := color.NRGBAModel.Convert(img.At(test.x, test.y)).(color.NRGBA); got != test.want {
			t.Fatalf("pixel (%d,%d): got %v, want %v", test.x, test.y, got, test.want)
		}
	}
	// The same declaration is accepted by the existing attachment API.
	variant := attachments.DefineVariant("profile-card", plan)
	if err := variant.Validate(); err != nil {
		t.Fatal(err)
	}
}
