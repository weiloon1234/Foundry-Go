package profiles_test

import (
	"context"
	"image/color"
	"image/png"
	"testing"

	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/imaging"
)

func TestAVIFUploadUsesExistingFoundryPlan(t *testing.T) {
	engine, err := imaging.New(imaging.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	capability, ok := engine.Capabilities().ForFormat(imaging.AVIF)
	if !ok || !capability.Read || !capability.ReadAnimation || capability.WriteAnimation {
		t.Fatal("AVIF capabilities")
	}
	input, err := engine.Create(t.Context(), 40, 20, color.NRGBA{R: 255, A: 128}, imaging.NewPlan().Format(imaging.AVIF))
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Process(t.Context(), input.Reader(), profiles.AnimatedAvatarPlan())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(result.Reader())
	if err != nil || img.Bounds().Dx() != 40 || img.Bounds().Dy() != 20 || result.Info().Animated {
		t.Fatal("AVIF upload", err)
	}
	_, _, _, alpha := img.At(0, 0).RGBA()
	if alpha != 128*257 {
		t.Fatal("upload alpha lost")
	}
}
