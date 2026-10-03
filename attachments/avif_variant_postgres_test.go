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

func TestPostgresAttachmentVariantAcceptsAVIF(t *testing.T) {
	f := openAttachments(t)
	variant := DefineVariant("preview", imaging.NewPlan().Resize(8, 4).Format(imaging.PNG))
	photos := Define(extensiontest.Members, "avif-photos", Policy{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/avif"}, Variants: []Variant{variant}})
	manager, disk, _ := linkedManager(t, f, photos.Registration())
	input, err := f.image.Create(t.Context(), 16, 8, color.NRGBA{R: 255, A: 128}, imaging.NewPlan().Format(imaging.AVIF))
	if err != nil {
		t.Fatal(err)
	}
	result, err := photos.Add(t.Context(), manager, member(t, 1), Upload{Source: input.Reader(), OriginalName: "avatar.avif"})
	if err != nil || result.Publication != Published || result.PendingVariants {
		t.Fatal("AVIF variant publication", err)
	}
	files, err := photos.List(t.Context(), manager, member(t, 1))
	if err != nil || len(files) != 1 || len(files[0].variants) != 1 {
		t.Fatal("AVIF persistence", err)
	}
	stored := files[0].variants[0]
	data, _, err := disk.ReadBytes(t.Context(), stored.key, 1<<20, storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil || decoded.Bounds().Dx() != 8 || decoded.Bounds().Dy() != 4 {
		t.Fatal("AVIF persisted variant", err)
	}
	_, _, _, a := decoded.At(0, 0).RGBA()
	if a != 128*257 {
		t.Fatal("AVIF persisted alpha")
	}
}
