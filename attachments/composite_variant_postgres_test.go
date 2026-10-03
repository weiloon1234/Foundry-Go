package attachments

import (
	"bytes"
	"image/color"
	"image/png"
	"testing"

	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestPostgresAttachmentVariantCompositesPreparedWatermark(t *testing.T) {
	f := openAttachments(t)
	watermark, err := f.image.Create(t.Context(), 2, 2, color.NRGBA{G: 255, A: 255}, imaging.NewPlan())
	if err != nil {
		t.Fatal(err)
	}
	plan := imaging.NewPlan().Pad(12, 12, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, imaging.Center).
		Insert(watermark, imaging.Placement{Position: imaging.BottomRight, X: -1, Y: -1}).Format(imaging.PNG)
	variant := DefineVariant("watermarked", plan)
	photos := Define(extensiontest.Members, "watermarked-photos", Policy{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/png"}, Variants: []Variant{variant}})
	manager, disk, _ := linkedManager(t, f, photos.Registration())
	owner := member(t, 1)
	result, err := photos.Add(t.Context(), manager, owner, pngUpload(t, 8, 4))
	if err != nil || result.Publication != Published || result.PendingVariants {
		t.Fatal("composited variant publication", err)
	}
	files, err := photos.List(t.Context(), manager, owner)
	if err != nil || len(files) != 1 || len(files[0].variants) != 1 {
		t.Fatal("variant persistence", err)
	}
	stored := files[0].variants[0]
	data, _, err := disk.ReadBytes(t.Context(), stored.key, 1<<20, storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() != 12 || img.Bounds().Dy() != 12 {
		t.Fatal("stored variant pixels", err)
	}
	for _, point := range []struct {
		x, y int
		want color.NRGBA
	}{{0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255}}, {9, 9, color.NRGBA{G: 255, A: 255}}, {11, 11, color.NRGBA{R: 255, G: 255, B: 255, A: 255}}} {
		if got := color.NRGBAModel.Convert(img.At(point.x, point.y)).(color.NRGBA); got != point.want {
			t.Fatal("stored variant does not match composition", point.x, point.y, got)
		}
	}
	if _, err := photos.Replace(t.Context(), manager, owner, pngUpload(t, 4, 4)); err != nil {
		t.Fatal(err)
	}
	assertVariantsCleaned(t, f, disk, result.Operation)
}
