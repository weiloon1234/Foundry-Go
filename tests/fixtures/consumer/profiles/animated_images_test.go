package profiles_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"testing"

	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/imaging"
)

func TestAnimatedAvatarUsesFoundryPlanAndCapabilities(t *testing.T) {
	engine, err := imaging.New(imaging.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	capability, ok := engine.Capabilities().ForFormat(imaging.PNG)
	if !ok || !capability.WriteAnimation {
		t.Fatal("animated PNG unavailable")
	}
	frame := image.NewPaletted(image.Rect(0, 0, 640, 320), color.Palette{color.Black, color.White})
	var input bytes.Buffer
	if err := gif.EncodeAll(&input, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{5, 10}, LoopCount: 3}); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []imaging.Plan{profiles.AnimatedAvatarPlan(), profiles.AnimatedWebPAvatarPlan()} {
		if err := attachments.DefineVariant("animated-avatar", plan).Validate(); err != nil {
			t.Fatal(err)
		}
		result, err := engine.ProcessBytes(t.Context(), input.Bytes(), plan)
		if err != nil {
			t.Fatal(err)
		}
		info, err := imaging.Inspect(result.Bytes(), engine.Limits())
		if err != nil || info.Width != 320 || info.Height != 160 || info.Images != 2 || !info.Animated {
			t.Fatal("animation output", info, err)
		}
	}
}
