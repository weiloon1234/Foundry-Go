package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestFillRejectsOversizedIntermediateEvenWhenFinalCanvasFits(t *testing.T) {
	config := DefaultConfig()
	config.Limits.Width = 64
	config.Limits.Height = 64
	config.Limits.Pixels = 4096
	engine := testEngine(t, config)
	if _, err := engine.ProcessBytes(t.Context(), pngInput(t, 40, 1), NewPlan().Fill(2, 2, true)); err == nil {
		t.Fatal("fill exceeded admitted intermediate width")
	}
}

// jpegWithDimensions encodes a small baseline JPEG and rewrites its SOF0
// dimensions, so header-level admission sees a large photo without pixels.
func jpegWithDimensions(t *testing.T, width, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewYCbCr(image.Rect(0, 0, 16, 16), image.YCbCrSubsampleRatio420), nil); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	at := bytes.Index(data, []byte{0xff, 0xc0})
	if at < 0 {
		t.Fatal("baseline JPEG has no SOF0 marker")
	}
	binary.BigEndian.PutUint16(data[at+5:], uint16(height))
	binary.BigEndian.PutUint16(data[at+7:], uint16(width))
	return data
}

func TestDefaultLimitsAdmitAdvertisedPixelBudget(t *testing.T) {
	limits := DefaultLimits()
	// 25 MP at the advertised Pixels limit, in the most expensive decoder.
	info, err := Inspect(jpegWithDimensions(t, 5000, 5000), limits)
	if err != nil || info.Width != 5000 || info.Height != 5000 {
		t.Fatal("default limits reject the advertised pixel budget", err)
	}
	input := int64(len(jpegWithDimensions(t, 5000, 5000)))
	for _, format := range []Format{JPEG, PNG, WebP, GIF, TIFF, BMP} {
		large := Info{Format: format, Width: 5000, Height: 5000, Images: 1, Orientation: 6}
		for _, plan := range []Plan{NewPlan().Format(JPEG), NewPlan().Fit(1024, 1024, false).Format(WebP), NewPlan().Rotate(Rotate90).Grayscale().Brightness(10).Format(PNG), NewPlan().Blur(2).Background(color.NRGBA{A: 255}).Format(JPEG), NewPlan().Format(AVIF)} {
			if err := limits.admit(input, int64(large.Width)*int64(large.Height)*format.decodePeakBytes()); err != nil {
				t.Fatal("default limits reject decoding the advertised pixel budget", format)
			}
			if _, err := plan.admit(t.Context(), inspection{Info: large}, plan.output, input, limits, PortableBackend); err != nil {
				t.Fatal("default limits reject a 25 MP pipeline", format, plan.output, err)
			}
		}
	}
	// The pure-Go lossless WebP encoder reserves 128 bytes per pixel plus fixed workspace, so a
	// full-size 25 MP WebP re-encode exceeds the default budget (documented).
	full := Info{Format: JPEG, Width: 5000, Height: 5000, Images: 1, Orientation: 1}
	if _, err := NewPlan().Format(WebP).admit(t.Context(), inspection{Info: full}, WebP, input, limits, PortableBackend); err == nil {
		t.Fatal("full-size lossless WebP encoding was admitted beyond its measured cost")
	}
	over := jpegWithDimensions(t, 5001, 5000)
	if _, err := Inspect(over, limits); err == nil {
		t.Fatal("pixel limit ignored")
	}
}

func TestTwelveMegapixelPhotoIsProcessedWithDefaults(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewYCbCr(image.Rect(0, 0, 4000, 3000), image.YCbCrSubsampleRatio420), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	result, err := testEngine(t, DefaultConfig()).ProcessBytes(t.Context(), b.Bytes(), NewPlan().Fit(1024, 1024, false).Format(JPEG))
	if err != nil || result.Info().Width != 1024 || result.Info().Height != 768 {
		t.Fatal("ordinary phone photo rejected", err)
	}
}

func TestThinImageResamplingAndBlurIncludeWeightsAndRows(t *testing.T) {
	config := DefaultConfig()
	config.Limits.WorkingBytes = 32 << 10
	config.Limits.OutputBytes = 2048
	e := testEngine(t, config)
	input := pngInput(t, 1, 1000)
	// Pixel canvases fit this budget, but Lanczos weights and blur's float32
	// rows do not. Nearest-neighbor needs neither and remains available.
	for _, plan := range []Plan{NewPlan().Resize(1, 999), NewPlan().Blur(1)} {
		if result, err := e.ProcessBytes(t.Context(), input, plan); err == nil || result.Size() != 0 {
			t.Fatal("thin-image filter escaped its workspace budget")
		}
	}
	if _, err := e.ProcessBytes(t.Context(), input, NewPlan().Resize(1, 999).Resampling(NearestNeighbor)); err != nil {
		t.Fatal("bounded nearest-neighbor processing rejected", err)
	}
}
