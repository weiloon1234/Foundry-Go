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

func TestProfileLabelUsesFoundryFontsAndDrawing(t *testing.T) {
	engine, err := imaging.New(imaging.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	font, err := imaging.BuiltinFont(imaging.FontBold)
	if err != nil {
		t.Fatal(err)
	}
	plan := profiles.ProfileLabelPlan("Foundry profile", font)
	variant := attachments.DefineVariant("profile-label", plan)
	if err := variant.Validate(); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Create(t.Context(), 160, 80, color.NRGBA{R: 255, A: 255}, plan)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(result.Reader())
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(img.At(8, 100)).(color.NRGBA); got != (color.NRGBA{R: 20, G: 40, B: 70, A: 255}) {
		t.Fatal("label background", got)
	}
	white := 0
	for y := 105; y < 145; y++ {
		for x := 20; x < 300; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.R > 200 && c.G > 200 && c.B > 200 {
				white++
			}
		}
	}
	if white < 100 {
		t.Fatal("label text missing", white)
	}
}
