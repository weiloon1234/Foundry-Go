package profiles_test

import (
	"context"
	"image/color"
	"testing"

	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/imaging"
)

func TestImageEncodingPlansUseFoundryControls(t *testing.T) {
	e, err := imaging.New(imaging.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	for _, plan := range []imaging.Plan{profiles.PNGDownloadPlan(), profiles.AVIFPreviewPlan(), profiles.WebPPreviewPlan()} {
		if err := attachments.DefineVariant("export", plan).Validate(); err != nil {
			t.Fatal(err)
		}
		result, err := e.Create(t.Context(), 32, 32, color.NRGBA{R: 255, A: 128}, plan)
		if err != nil {
			t.Fatal(err)
		}
		info, err := imaging.Inspect(result.Bytes(), e.Limits())
		if err != nil || info.Width != 32 || info.Height != 32 {
			t.Fatal("encoding plan output", info, err)
		}
	}
}
