package attachments

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"

	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestPostgresAttachmentVariantPreservesAnimation(t *testing.T) {
	base := imaging.NewPlan().Frames(imaging.PreserveAnimation).Resize(8, 8)
	for _, test := range []struct {
		name string
		plan imaging.Plan
	}{
		{"png", base.Format(imaging.PNG)},
		{"webp-lossless", base.Format(imaging.WebP)},
		{"webp-lossy", base.Format(imaging.WebP).WebPMode(imaging.WebPLossy).WebPQuality(90)},
	} {
		t.Run(test.name, func(t *testing.T) { testPostgresAnimatedVariant(t, test.plan) })
	}
}

func testPostgresAnimatedVariant(t *testing.T, plan imaging.Plan) {
	f := openAttachments(t)
	variant := DefineVariant("animated", plan)
	photos := Define(extensiontest.Members, "animated-photos", Policy{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/gif"}, Variants: []Variant{variant}})
	manager, disk, _ := linkedManager(t, f, photos.Registration())
	colors := color.Palette{color.Black, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 4, 4), colors)
	second := image.NewPaletted(image.Rect(0, 0, 4, 4), colors)
	for i := range second.Pix {
		second.Pix[i] = 1
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{7, 13}, LoopCount: 2}); err != nil {
		t.Fatal(err)
	}
	result, err := photos.Add(t.Context(), manager, member(t, 1), Upload{Source: bytes.NewReader(encoded.Bytes()), OriginalName: "avatar.gif"})
	if err != nil || result.Publication != Published || result.PendingVariants {
		t.Fatal("animated variant publication", err)
	}
	files, err := photos.List(t.Context(), manager, member(t, 1))
	if err != nil || len(files) != 1 || len(files[0].variants) != 1 {
		t.Fatal("animated variant persistence", err)
	}
	stored := files[0].variants[0]
	data, _, err := disk.ReadBytes(t.Context(), stored.key, 1<<20, storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := imaging.Inspect(data, f.image.Limits())
	if err != nil || !info.Animated || info.Images != 2 || info.Width != 8 || info.Height != 8 {
		t.Fatal("stored variant lost animation", info, err)
	}
	// Read the persisted animation through the public engine and verify its timing and
	// both actual frame images with the standard GIF decoder.
	back, err := f.image.ProcessBytes(t.Context(), data, imaging.NewPlan().Frames(imaging.PreserveAnimation).Format(imaging.GIF))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := gif.DecodeAll(back.Reader())
	if err != nil || len(decoded.Image) != 2 || decoded.Delay[0] != 7 || decoded.Delay[1] != 13 || decoded.LoopCount != 2 {
		t.Fatal("persisted animation timing", err)
	}
	r0, _, _, _ := decoded.Image[0].At(0, 0).RGBA()
	r1, _, _, _ := decoded.Image[1].At(0, 0).RGBA()
	if r0 != 0 || r1 != 65535 {
		t.Fatal("persisted frames changed")
	}
}
