package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/gen2brain/gav1d/avif"
)

func avifFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "avif", name+".avif"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAVIFStillFixtures(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	files, err := filepath.Glob("testdata/avif/*.avif")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if filepath.Base(path) == "anim.avif" {
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := Inspect(data, e.Limits())
			if err != nil || info.Format != AVIF || info.Animated {
				t.Fatal("inspect", info, err)
			}
			for _, orientation := range []Orientation{ApplyOrientation, IgnoreOrientation} {
				result, err := e.ProcessBytes(t.Context(), data, NewPlan().Orientation(orientation).Format(PNG))
				if err != nil {
					t.Fatal("process", err)
				}
				want, err := avif.Decode(bytes.NewReader(data), avif.Options{AutoRotate: orientation == ApplyOrientation})
				if err != nil {
					t.Fatal(err)
				}
				got, err := png.Decode(result.Reader())
				if err != nil {
					t.Fatal(err)
				}
				if got.Bounds().Size() != want.Bounds().Size() {
					t.Fatal("transformed dimensions", got.Bounds(), want.Bounds())
				}
				admission, err := inspectAVIF(data, e.Limits())
				if err != nil {
					t.Fatal(err)
				}
				for y := 0; y < got.Bounds().Dy(); y++ {
					for x := 0; x < got.Bounds().Dx(); x++ {
						// Orientation transforms use the engine's established 8-bit
						// RGBA pipeline; untouched 10/12-bit input retains precision.
						g := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
						w := color.NRGBAModel.Convert(want.At(x+want.Bounds().Min.X, y+want.Bounds().Min.Y)).(color.NRGBA)
						tolerance := 0
						if _, highPrecision := want.(*image.NRGBA64); highPrecision && orientation == ApplyOrientation {
							// GIFT rounds its 16-to-8-bit conversion; NRGBAModel
							// truncates. Only transformed high-depth input differs.
							if !admission.crop.Empty() || info.Orientation != 1 {
								tolerance = 1
							}
						}
						close := func(a, b byte) bool { d := int(a) - int(b); return d >= -tolerance && d <= tolerance }
						if _, highPrecision := want.(*image.NRGBA64); highPrecision && (orientation == IgnoreOrientation || admission.crop.Empty() && info.Orientation == 1) {
							gr, gg, gb, ga := got.At(x, y).RGBA()
							wr, wg, wb, wa := want.At(x, y).RGBA()
							if gr != wr || gg != wg || gb != wb || ga != wa {
								t.Fatal("untouched high-depth input lost precision")
							}
						}
						if !close(g.R, w.R) || !close(g.G, w.G) || !close(g.B, w.B) || g.A != w.A {
							t.Fatalf("pixel %d,%d: got %v want %v", x, y, g, w)
						}
					}
				}
			}
		})
	}
}

func TestAVIFSequenceTimingPixelsAndPolicies(t *testing.T) {
	data := avifFixture(t, "anim")
	e := testEngine(t, DefaultConfig())
	info, err := Inspect(data, e.Limits())
	if err != nil || !info.Animated || info.Images < 2 {
		t.Fatal("sequence inspection", info, err)
	}
	if result, err := e.ProcessBytes(t.Context(), data, NewPlan().Format(PNG)); err == nil || result.Size() != 0 {
		t.Fatal("implicit animation flattening")
	}
	want, err := avif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.ProcessBytes(t.Context(), data, NewPlan().Frames(PreserveAnimation).Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeAPNG(t.Context(), result.Bytes(), e.Limits())
	if err != nil || len(got.frames) != len(want.Image) {
		t.Fatal("APNG frames", err)
	}
	wantPlays := uint32(0)
	if want.LoopCount < 0 {
		wantPlays = 1
	} else if want.LoopCount > 0 {
		wantPlays = uint32(want.LoopCount) + 1
	}
	if got.plays != wantPlays {
		t.Fatal("changed loop count")
	}
	for i, img := range got.frames {
		if float64(got.delays[i].numerator)/float64(got.delays[i].denominator) != want.Delay[i] {
			t.Fatal("changed frame timing")
		}
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				if color.NRGBAModel.Convert(img.At(x, y)) != color.NRGBAModel.Convert(want.Image[i].At(x, y)) {
					t.Fatal("changed frame pixels", i, x, y)
				}
			}
		}
	}
	first, err := e.ProcessBytes(t.Context(), data, NewPlan().Frames(FirstFrame).Resize(12, 8).Format(PNG))
	if err != nil || first.Info().Animated || first.Info().Width != 12 {
		t.Fatal("first-frame policy", err)
	}
	if result, err := e.ProcessBytes(t.Context(), data, NewPlan().Frames(PreserveAnimation).Format(AVIF)); err == nil || result.Size() != 0 {
		t.Fatal("unsupported animation output")
	}
	// Incompatible dimensions or corrupt sequence data must not silently return
	// DecodeAll's primary-image fallback, even with a one-frame sequence.
	c, err := inspectAVIF(data, e.Limits())
	if err != nil {
		t.Fatal(err)
	}
	broken := bytes.Clone(data)
	for _, sample := range c.track.samples {
		clear(broken[int(sample.offset):int(sample.offset+sample.length)])
	}
	if _, err := decodeAVIFSequence(broken, c); err == nil {
		t.Fatal("accepted codec fallback")
	}
	one := *c.track
	one.samples = one.samples[:1]
	one.deltas = one.deltas[:1]
	oneContainer := *c
	oneContainer.track = &one
	if _, err := decodeAVIFSequence(broken, &oneContainer); err == nil {
		t.Fatal("accepted single-frame codec fallback")
	}
	if result, err := e.ProcessBytes(t.Context(), broken, NewPlan().Frames(FirstFrame).Format(PNG)); err == nil || result.Size() != 0 {
		t.Fatal("accepted broken sequence")
	}
	if _, err := e.ProcessBytes(t.Context(), data, NewPlan().Frames(FirstFrame).Format(PNG)); err != nil {
		t.Fatal("capacity not reusable", err)
	}
}

func TestAVIFRejectsMetadataAndResourceAbuse(t *testing.T) {
	still := avifFixture(t, "yuv420-8")
	sequence := avifFixture(t, "anim")
	for _, tc := range []struct {
		name   string
		data   []byte
		mutate func([]byte)
	}{
		{"dimensions", still, func(b []byte) { p := bytes.Index(b, []byte("ispe")); binary.BigEndian.PutUint32(b[p+8:], ^uint32(0)) }},
		{"item count", still, func(b []byte) { p := bytes.Index(b, []byte("iloc")); binary.BigEndian.PutUint16(b[p+10:], 65535) }},
		{"sample count", sequence, func(b []byte) { p := bytes.Index(b, []byte("stsz")); binary.BigEndian.PutUint32(b[p+12:], 1<<24) }},
		{"timing expansion", sequence, func(b []byte) { p := bytes.Index(b, []byte("stts")); binary.BigEndian.PutUint32(b[p+12:], 1<<24) }},
		{"zero delay", sequence, func(b []byte) { p := bytes.Index(b, []byte("stts")); binary.BigEndian.PutUint32(b[p+16:], 0) }},
		{"oversized box", still, func(b []byte) { binary.BigEndian.PutUint32(b, ^uint32(0)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := bytes.Clone(tc.data)
			tc.mutate(data)
			if _, err := inspect(data, DefaultLimits()); err == nil {
				t.Fatal("accepted invalid metadata")
			}
		})
	}
	for _, configure := range []func(*Limits){
		func(l *Limits) { l.Frames = 1 }, func(l *Limits) { l.Pixels = 1 }, func(l *Limits) { l.WorkingBytes = 2 << 20 },
	} {
		config := DefaultConfig()
		configure(&config.Limits)
		if result, err := testEngine(t, config).ProcessBytes(t.Context(), sequence, NewPlan().Frames(PreserveAnimation).Format(PNG)); err == nil || result.Size() != 0 {
			t.Fatal("AVIF exceeded resource bound")
		}
	}
	// OBU framing is checked across item extents without gathering payloads.
	if n, err := avifFrameUnits([][]byte{{0x32}, {1}, {0}}, 1); err != nil || n != 1 {
		t.Fatal("split OBU", n, err)
	}
	for _, data := range [][]byte{{0x30, 0}, {0x32, 255}, {0xb2, 0}, {0x36, 7, 0}, {0x32, 0, 0x32, 0}} {
		if _, err := avifFrameUnits([][]byte{data}, 1); err == nil {
			t.Fatal("accepted malformed/excessive OBU", data)
		}
	}
}

func TestAVIFRoundTripAlphaAndLayer(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	layer, err := e.Create(t.Context(), 4, 4, color.NRGBA{R: 230, G: 30, B: 15, A: 128}, NewPlan().Format(AVIF).AVIFQuality(100))
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.ProcessBytes(t.Context(), layer.Bytes(), NewPlan().Resize(8, 8).Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(result.Reader())
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, a := img.At(0, 0).RGBA()
	if a != 128*257 {
		t.Fatal("lost AVIF alpha", a)
	}
	composite, err := e.Create(t.Context(), 8, 8, color.NRGBA{A: 255}, NewPlan().Insert(layer, Placement{}).Format(PNG))
	if err != nil || composite.Size() == 0 {
		t.Fatal("AVIF layer", err)
	}
}

// This benchmark provides representative codec allocation evidence; admission
// remains conservative and does not claim a process heap quota.
func BenchmarkAVIFDecodeWorkspace(b *testing.B) {
	for _, size := range []image.Point{{64, 64}, {1024, 768}} {
		img := image.NewNRGBA(image.Rectangle{Max: size})
		for y := 0; y < size.Y; y++ {
			for x := 0; x < size.X; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: byte(x), G: byte(y), B: byte(x + y), A: byte(128 + x%128)})
			}
		}
		var encoded bytes.Buffer
		if err := avif.Encode(&encoded, img, avif.EncodeOptions{Quality: 60, Speed: 10}); err != nil {
			b.Fatal(err)
		}
		container, err := inspectAVIF(encoded.Bytes(), DefaultLimits())
		if err != nil {
			b.Fatal(err)
		}
		b.Run(size.String(), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := decodeAVIF(encoded.Bytes(), container); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(container.workspace()), "admitted-B")
		})
	}
}
