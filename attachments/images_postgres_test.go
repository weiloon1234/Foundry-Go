package attachments

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresAttachmentImagePolicyTransformsDetectedBytes(t *testing.T) {
	collection := Define(extensiontest.Members, "portrait", Policy{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/png"}, Image: value.Set(imaging.NewPlan().Fill(2, 2, false).Format(imaging.WebP))})
	f := openAttachments(t, collection.Registration())
	img := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for y := range 4 {
		for x := range 8 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 30), G: uint8(y * 60), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	owner := member(t, 1)
	result, err := collection.Add(t.Context(), f.manager, owner, Upload{Source: bytes.NewReader(encoded.Bytes()), OriginalName: "untrusted.txt", ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	file := attachmentOf(t, result)
	if file.Info().MediaType != "image/webp" || file.Info().Width != 2 || file.Info().Height != 2 {
		t.Fatal("stored image metadata follows input hint")
	}
	body, err := collection.ReadBytes(t.Context(), f.manager, owner, file.ID(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	info, err := imaging.Inspect(body, f.image.Limits())
	if err != nil || info.Format != imaging.WebP || info.Width != 2 || info.Height != 2 {
		t.Fatal("transformed image invalid", err)
	}
	rendered, err := collection.Image(t.Context(), f.manager, owner, file.ID(), imaging.NewPlan().Format(imaging.PNG))
	if err != nil || rendered.Info().Format != imaging.PNG {
		t.Fatal("read image conversion", err)
	}
	if _, err := collection.Replace(t.Context(), f.manager, owner, uploadText("fake image")); err == nil {
		t.Fatal("image policy trusted filename or MIME")
	}
	if _, err := collection.Find(t.Context(), f.manager, owner, file.ID()); err != nil {
		t.Fatal("failed image removed prior ownership", err)
	}
}

func TestPostgresAttachmentImageDeclarationsRespectEngineCapabilities(t *testing.T) {
	f := openAttachments(t)
	for _, policy := range []Policy{
		{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/svg+xml"}, Variants: []Variant{DefineVariant("preview", imaging.NewPlan().Format(imaging.PNG))}},
		{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/png"}, Image: value.Set(imaging.NewPlan().ToSRGB())},
		{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/png"}, Variants: []Variant{DefineVariant("preview", imaging.NewPlan().Format(imaging.HEIF))}},
	} {
		collection := Define(extensiontest.Members, "native-unsupported", policy)
		manager, err := New(Dependencies{Store: f.Store, Disks: f.registry, Image: f.image}, DefaultConfig(), collection.Registration())
		if err == nil || manager != nil {
			t.Fatal("portable manager accepted a native declaration")
		}
	}
}
