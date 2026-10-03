//go:build foundry_vips && cgo

package attachments

import (
	"context"
	"image/color"
	"testing"

	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestPostgresNativeImageOriginalAndVariant(t *testing.T) {
	f := openAttachments(t)
	config := imaging.DefaultConfig()
	config.Backend = imaging.LibvipsBackend
	engine, err := imaging.New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	f.image = engine
	variant := DefineVariant("portrait", imaging.NewPlan().SmartFill(8, 8, true, imaging.CropEntropy).Format(imaging.PNG))
	photos := Define(extensiontest.Members, "native-photos", Policy{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/heic"}, Variants: []Variant{variant}})
	manager, disk, _ := linkedManager(t, f, photos.Registration())
	original, err := engine.Create(t.Context(), 16, 8, color.NRGBA{R: 255, A: 255}, imaging.NewPlan().Format(imaging.HEIF))
	if err != nil {
		t.Fatal(err)
	}
	result, err := photos.Add(t.Context(), manager, member(t, 1), Upload{Source: original.Reader(), OriginalName: "portrait.heic"})
	if err != nil || result.Publication != Published || result.PendingVariants {
		t.Fatal("native upload publication", err)
	}
	files, err := photos.List(t.Context(), manager, member(t, 1))
	if err != nil || len(files) != 1 || len(files[0].variants) != 1 {
		t.Fatal("native variant persistence", err)
	}
	stored := files[0].variants[0]
	data, _, err := disk.ReadBytes(t.Context(), stored.key, 1<<20, storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := engine.Inspect(t.Context(), data)
	if err != nil || info.Format != imaging.PNG || info.Width != 8 || info.Height != 8 {
		t.Fatal("persisted native-derived variant", info, err)
	}
}
