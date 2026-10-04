//go:build cgo && (darwin || linux || freebsd || windows)

package imaging

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

func nativeEngine(t *testing.T) *Engine {
	t.Helper()
	config := DefaultConfig()
	config.Backend = LibvipsBackend
	if _, err := nativeCapabilities(); err != nil {
		if os.Getenv("FOUNDRY_TEST_VIPS_REQUIRED") == "1" {
			t.Fatal(err)
		}
		t.Skip("libvips runtime unavailable")
	}
	return testEngine(t, config)
}
func requireNativeFormat(t *testing.T, e *Engine, format Format) {
	t.Helper()
	c, ok := e.Capabilities().ForFormat(format)
	if !ok || !c.Read || format != SVG && !c.Write {
		if os.Getenv("FOUNDRY_TEST_VIPS_REQUIRED") == "1" {
			t.Fatal("required native format unavailable", format, c)
		}
		t.Skip("installed libvips does not support this optional codec", format)
	}
}
func nativeFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/native/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestNativeCapabilitiesAndFormatRoundTrips(t *testing.T) {
	_ = nativeEngine(t)
	e := testEngine(t, DefaultConfig())
	if e.NativeError() != nil {
		t.Fatal("default engine did not discover the native runtime", e.NativeError())
	}
	caps := e.Capabilities()
	if caps.Backend != LibvipsBackend || !caps.ColorManagement || !caps.MetadataPreservation || !caps.SmartCrop {
		t.Fatal("native capabilities incomplete", caps)
	}
	for _, format := range []Format{HEIF, JPEG2000, JPEGXL} {
		t.Run(string(format), func(t *testing.T) {
			requireNativeFormat(t, e, format)
			original := webpEncodingImage(32, 16)
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, original); err != nil {
				t.Fatal(err)
			}
			out, err := e.ProcessBytes(t.Context(), encoded.Bytes(), NewPlan().Format(format))
			if err != nil {
				t.Fatal(err)
			}
			info, err := e.Inspect(t.Context(), out.data)
			if err != nil || info.Format != format || info.Width != 32 || info.Height != 16 {
				t.Fatal("native roundtrip header", info, err)
			}
			if _, err := Inspect(out.data, e.Limits()); err == nil {
				t.Fatal("portable inspection silently used native codec")
			}
			back, err := e.ProcessBytes(t.Context(), out.data, NewPlan().Format(PNG))
			if err != nil {
				t.Fatal(err)
			}
			pixels, err := png.Decode(back.Reader())
			if err != nil {
				t.Fatal(err)
			}
			if format != HEIF {
				assertImagePixels(t, pixels, original, false)
			}
			resized, err := e.ProcessBytes(t.Context(), out.data, NewPlan().Resize(8, 4).Format(JPEG))
			if err != nil || resized.Info().Width != 8 || resized.Info().Height != 4 {
				t.Fatal("native transform", err)
			}
		})
	}
}

func TestNativeSVGUsesSharedTransformAndLayerPipeline(t *testing.T) {
	e := nativeEngine(t)
	requireNativeFormat(t, e, SVG)
	badge, err := e.Create(t.Context(), 2, 2, color.NRGBA{B: 255, A: 255}, NewPlan())
	if err != nil {
		t.Fatal(err)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="#ff0000"/></svg>`)
	result, err := e.ProcessBytes(t.Context(), svg, NewPlan().Resize(20, 10).Insert(badge, Placement{Position: BottomRight}).Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(result.Reader())
	if err != nil {
		t.Fatal(err)
	}
	assertPixel(t, img, 0, 0, color.NRGBA{R: 255, A: 255})
	assertPixel(t, img, 19, 9, color.NRGBA{B: 255, A: 255})
	if out, err := e.ProcessBytes(t.Context(), svg, NewPlan()); err == nil || out.Size() != 0 {
		t.Fatal("SVG output unexpectedly supported")
	}
	config := DefaultConfig()
	config.Backend = LibvipsBackend
	config.Limits.Width = 8
	if _, err := testEngine(t, config).Inspect(t.Context(), svg); err == nil {
		t.Fatal("native dimensions escaped limits")
	}
}

func TestNativeICCConversionAndMetadataPolicies(t *testing.T) {
	e := nativeEngine(t)
	data := nativeFixture(t, "linear-srgb.png")
	expected, err := png.Decode(bytes.NewReader(nativeFixture(t, "converted-srgb.png")))
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.ProcessBytes(t.Context(), data, NewPlan().ToSRGB().Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(result.Reader())
	if err != nil {
		t.Fatal(err)
	}
	for y := range 16 {
		for x := range 24 {
			a := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
			b := color.NRGBAModel.Convert(expected.At(x, y)).(color.NRGBA)
			for _, pair := range [][2]uint8{{a.R, b.R}, {a.G, b.G}, {a.B, b.B}} {
				if int(pair[0])-int(pair[1]) > 2 || int(pair[1])-int(pair[0]) > 2 {
					t.Fatal("ICC conversion differs from independent LittleCMS fixture", a, b)
				}
			}
		}
	}
	if len(pngICC(t, result.data)) != 0 {
		t.Fatal("strip policy retained ICC")
	}
	preserved, err := e.ProcessBytes(t.Context(), data, NewPlan().Metadata(PreserveColorProfile).Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pngICC(t, preserved.data), nativeFixture(t, "linear-srgb.icc")) {
		t.Fatal("RGB profile was not preserved")
	}
	converted, err := e.ProcessBytes(t.Context(), data, NewPlan().ToSRGB().Metadata(PreserveColorProfile).Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	profile := pngICC(t, converted.data)
	if len(profile) == 0 || bytes.Equal(profile, nativeFixture(t, "linear-srgb.icc")) {
		t.Fatal("converted pixels retained their old profile")
	}
	var bad bytes.Buffer
	compressed := zlib.NewWriter(&bad)
	_, _ = compressed.Write([]byte("invalid synthetic profile"))
	_ = compressed.Close()
	invalidProfile := insertPNGChunk(pngInput(t, 4, 4), "iCCP", append([]byte("profile\x00\x00"), bad.Bytes()...))
	for _, plan := range []Plan{NewPlan().ToSRGB(), NewPlan().Metadata(PreserveColorProfile)} {
		if out, err := e.ProcessBytes(t.Context(), invalidProfile, plan.Format(PNG)); err == nil || out.Size() != 0 {
			t.Fatal("malformed ICC silently ignored")
		}
	}
}

func TestNativeMetadataCapabilitiesRoundTripProfiles(t *testing.T) {
	e := nativeEngine(t)
	for _, cap := range e.Capabilities().Formats {
		if !cap.Write {
			continue
		}
		t.Run(string(cap.Format), func(t *testing.T) {
			plan := NewPlan().Format(cap.Format).Metadata(PreserveColorProfile)
			out, err := e.ProcessBytes(t.Context(), nativeFixture(t, "linear-srgb.png"), plan)
			if !cap.WriteMetadata {
				if err == nil || out.Size() != 0 {
					t.Fatal("unavailable metadata output accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			back, err := e.ProcessBytes(t.Context(), out.data, NewPlan().Format(PNG).Metadata(PreserveColorProfile))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(pngICC(t, back.data), nativeFixture(t, "linear-srgb.icc")) {
				t.Fatal("advertised metadata capability lost or changed its RGB profile")
			}
		})
	}
}

func TestNativeFirstFrameQualityAndLayers(t *testing.T) {
	e := nativeEngine(t)
	requireNativeFormat(t, e, JPEGXL)
	animated := nativeFixture(t, "animated.jxl")
	info, err := e.Inspect(t.Context(), animated)
	if err != nil || !info.Animated || info.Images != 4 {
		t.Fatal(info, err)
	}
	for _, frames := range []Frames{RejectAnimation, PreserveAnimation} {
		if out, err := e.ProcessBytes(t.Context(), animated, NewPlan().Frames(frames).Format(PNG)); err == nil || out.Size() != 0 {
			t.Fatal("native animation ignored frame policy")
		}
	}
	if out, err := e.ProcessBytes(t.Context(), animated, NewPlan().Frames(FirstFrame).Format(PNG)); err != nil || out.Info().Images != 1 {
		t.Fatal("native first frame", err)
	}
	input := pngInput(t, 32, 24)
	for _, plan := range []Plan{NewPlan().Format(HEIF).HEIFQuality(35), NewPlan().Format(JPEG2000).JPEG2000Quality(45), NewPlan().Format(JPEGXL).JPEGXLQuality(55)} {
		requireNativeFormat(t, e, plan.output)
		layer, err := e.ProcessBytes(t.Context(), input, plan)
		if err != nil {
			t.Fatal(err)
		}
		out, err := e.Create(t.Context(), 40, 32, color.NRGBA{A: 255}, NewPlan().Insert(layer, Placement{Position: Center}).Format(PNG))
		if err != nil || out.Info().Width != 40 || out.Info().Height != 32 {
			t.Fatal("native layer", err)
		}
	}
}

func BenchmarkNativeImagePipeline(b *testing.B) {
	config := DefaultConfig()
	config.Backend = LibvipsBackend
	e, err := New(config)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = e.Close(context.Background()) })
	for _, size := range []int{256, 1024} {
		var input bytes.Buffer
		if err := png.Encode(&input, webpEncodingImage(size, size/2)); err != nil {
			b.Fatal(err)
		}
		for _, format := range []Format{HEIF, JPEG2000, JPEGXL} {
			b.Run(fmt.Sprintf("%s/%d", format, size), func(b *testing.B) {
				plan := NewPlan().ToSRGB().SmartFill(size/2, size/2, false, CropEntropy).Format(format)
				b.ReportAllocs()
				for b.Loop() {
					if _, err := e.ProcessBytes(b.Context(), input.Bytes(), plan); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func pngICC(t testing.TB, data []byte) []byte {
	t.Helper()
	for at := 8; at+12 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[at:]))
		if n > len(data)-at-12 {
			t.Fatal("truncated PNG")
		}
		if string(data[at+4:at+8]) == "iCCP" {
			body := data[at+8 : at+8+n]
			end := bytes.IndexByte(body, 0)
			if end < 0 || end+2 > len(body) {
				t.Fatal("invalid PNG profile")
			}
			reader, err := zlib.NewReader(bytes.NewReader(body[end+2:]))
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			profile, err := io.ReadAll(io.LimitReader(reader, 4<<20))
			if err != nil {
				t.Fatal(err)
			}
			return profile
		}
		at += 12 + n
	}
	return nil
}

func TestNativePreservedExifNormalizesOrientationAndDimensions(t *testing.T) {
	e := nativeEngine(t)
	input := nativeFixture(t, "oriented-exif.jpg")
	plan := NewPlan().Fit(8, 12, false).Metadata(PreserveMetadata).Format(JPEG)
	out, err := e.ProcessBytes(t.Context(), input, plan)
	if err != nil {
		t.Fatal(err)
	}
	info, err := Inspect(out.data, e.Limits())
	if err != nil || info.Orientation != 1 || info.Width != 8 || info.Height != 12 {
		t.Fatal("stale EXIF geometry", info, err)
	}
	if !bytes.Contains(out.data, []byte("Foundry synthetic camera")) {
		t.Fatal("EXIF camera tag lost")
	}
	stripped, err := e.ProcessBytes(t.Context(), input, plan.Metadata(StripMetadata))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stripped.data, []byte("Foundry synthetic camera")) {
		t.Fatal("strip policy leaked metadata")
	}
}

func TestNativeSmartCropRetainsInterestingRegion(t *testing.T) {
	e := nativeEngine(t)
	img := solidImage(120, 40, color.NRGBA{A: 255})
	for y := 0; y < 40; y++ {
		for x := 90; x < 120; x++ {
			if (x+y)%2 == 0 {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
	var input bytes.Buffer
	if err := png.Encode(&input, img); err != nil {
		t.Fatal(err)
	}
	for _, interest := range []CropInterest{CropEntropy, CropAttention} {
		out, err := e.ProcessBytes(t.Context(), input.Bytes(), NewPlan().SmartFill(40, 40, false, interest).Format(PNG))
		if err != nil {
			t.Fatal(err)
		}
		got, err := png.Decode(out.Reader())
		if err != nil {
			t.Fatal(err)
		}
		white := 0
		for y := 0; y < 40; y++ {
			for x := 0; x < 40; x++ {
				r, _, _, _ := got.At(x, y).RGBA()
				if r > 32768 {
					white++
				}
			}
		}
		if white < 200 {
			t.Fatal("smart crop lost interesting region", interest, white)
		}
	}
}

func TestNativeLimitsCancellationAndIndependentEngines(t *testing.T) {
	e := nativeEngine(t)
	requireNativeFormat(t, e, JPEGXL)
	config := DefaultConfig()
	config.Backend = LibvipsBackend
	config.Limits.OutputBytes = 16
	limitedEngine := testEngine(t, config)
	if out, err := limitedEngine.Create(t.Context(), 32, 32, color.NRGBA{R: 255, A: 255}, NewPlan().Format(JPEGXL)); err == nil || out.Size() != 0 {
		t.Fatal("native output bound ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if out, err := e.ProcessBytes(ctx, nativeFixture(t, "linear-srgb.png"), NewPlan().ToSRGB().Format(PNG)); err == nil || out.Size() != 0 {
		t.Fatal("native cancellation published output")
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			out, err := e.Create(t.Context(), 16, 16, color.NRGBA{G: 180, A: 255}, NewPlan().Format(JPEGXL))
			if err != nil || out.Size() == 0 {
				t.Error("concurrent native operation", err)
			}
		})
	}
	wg.Wait()
	other := nativeEngine(t)
	if err := e.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-e.Done():
	default:
		t.Fatal("closed engine retained work")
	}
	if _, err := e.Create(t.Context(), 1, 1, color.NRGBA{}, NewPlan()); err == nil {
		t.Fatal("closed engine admitted work")
	}
	if out, err := other.Create(t.Context(), 8, 8, color.NRGBA{A: 255}, NewPlan().Format(JPEGXL)); err != nil || out.Size() == 0 {
		t.Fatal("closing one engine stopped shared libvips", err)
	}
}

func TestNativeCloseRetainsActualReaderLifetime(t *testing.T) {
	e := nativeEngine(t)
	reader := blockingReader{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.Process(context.Background(), reader, NewPlan().ToSRGB().Format(PNG))
	}()
	<-reader.entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := e.Close(ctx); err == nil {
		t.Fatal("close hid live reader")
	}
	select {
	case <-e.Done():
		t.Fatal("native engine dropped ownership")
	default:
	}
	close(reader.release)
	<-done
	if err := e.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeInFlightCancellationLeavesEngineUsable(t *testing.T) {
	_ = nativeEngine(t)
	config := DefaultConfig()
	config.Backend, config.MaxActive = LibvipsBackend, 1
	e := testEngine(t, config)
	requireNativeFormat(t, e, JPEGXL)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	out, err := e.Create(ctx, 1536, 1536, color.NRGBA{R: 200, A: 255}, NewPlan().Format(JPEGXL).JPEGXLQuality(65))
	if err == nil || out.Size() != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("in-flight cancellation published output or lost deadline", err)
	}
	if out, err := e.Create(t.Context(), 16, 16, color.NRGBA{A: 255}, NewPlan().Format(JPEGXL)); err != nil || out.Size() == 0 {
		t.Fatal("native cancellation stranded capacity", err)
	}
}
