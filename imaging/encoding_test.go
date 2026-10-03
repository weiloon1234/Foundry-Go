package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/gen2brain/gav1d/avif"
)

func TestEncodingOptionsValidateFormatAndPresence(t *testing.T) {
	for _, plan := range []Plan{
		NewPlan().PNGCompression(PNGDefaultCompression),
		NewPlan().Format(JPEG).PNGCompression(PNGBestSpeed),
		NewPlan().Format(PNG).PNGCompression(PNGCompression(255)),
		NewPlan().AVIFSpeed(0), NewPlan().Format(PNG).AVIFSpeed(5),
		NewPlan().Format(AVIF).AVIFSpeed(-1), NewPlan().Format(AVIF).AVIFSpeed(11),
		NewPlan().AVIFAlphaQuality(80), NewPlan().Format(PNG).AVIFAlphaQuality(80),
		NewPlan().Format(AVIF).AVIFAlphaQuality(0), NewPlan().Format(AVIF).AVIFAlphaQuality(101),
	} {
		if plan.Validate() == nil {
			t.Fatal("invalid encoder option accepted")
		}
	}
	base := NewPlan().Format(AVIF)
	slow := base.AVIFSpeed(0).AVIFAlphaQuality(100)
	if err := slow.Validate(); err != nil {
		t.Fatal(err)
	}
	if base.encoding.speed() != DefaultAVIFSpeed || base.encoding.avifAlphaQuality.IsSet() || slow.encoding.speed() != 0 {
		t.Fatal("encoding options lost omission or immutable plan semantics")
	}
}

func TestPNGCompressionChangesSizeWithoutChangingPixels(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	input := pngInput(t, 64, 64)
	want, err := png.Decode(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	sizes := make(map[PNGCompression]int64)
	base := NewPlan().Format(PNG)
	for _, compression := range []PNGCompression{PNGDefaultCompression, PNGNoCompression, PNGBestSpeed, PNGBestCompression} {
		result, err := e.ProcessBytes(t.Context(), input, base.PNGCompression(compression))
		if err != nil {
			t.Fatal(err)
		}
		got, err := png.Decode(result.Reader())
		if err != nil {
			t.Fatal(err)
		}
		sizes[compression] = result.Size()
		for y := range 64 {
			for x := range 64 {
				if got.At(x, y) != want.At(x, y) {
					t.Fatal("PNG compression changed pixels", compression, x, y)
				}
			}
		}
	}
	if sizes[PNGNoCompression] <= sizes[PNGBestCompression] {
		t.Fatal("PNG compression choice was ignored", sizes)
	}
	if base.encoding.pngCompression.IsSet() {
		t.Fatal("derived encoding mutated plan")
	}
	config := DefaultConfig()
	config.Limits.OutputBytes = sizes[PNGNoCompression] - 1
	if result, err := testEngine(t, config).ProcessBytes(t.Context(), input, base.PNGCompression(PNGNoCompression)); err == nil || result.Size() != 0 {
		t.Fatal("uncompressed PNG bypassed output limit")
	}
}

func TestAPNGCompressionAppliesToEveryFrame(t *testing.T) {
	input := apngFixture(t, 32, 32, 3, nil, []apngTestFrame{
		{img: solidImage(32, 32, color.NRGBA{R: 255, A: 128}), delay: frameDelay{1, 10}},
		{img: solidImage(32, 32, color.NRGBA{B: 255, A: 255}), delay: frameDelay{3, 10}},
	})
	e := testEngine(t, DefaultConfig())
	plan := NewPlan().Frames(PreserveAnimation).Format(PNG)
	var results [2]Result
	for i, compression := range []PNGCompression{PNGNoCompression, PNGBestCompression} {
		result, err := e.ProcessBytes(t.Context(), input, plan.PNGCompression(compression))
		if err != nil {
			t.Fatal(err)
		}
		results[i] = result
	}
	plain, err := parsePNG(results[0].Bytes(), e.Limits())
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := parsePNG(results[1].Bytes(), e.Limits())
	if err != nil {
		t.Fatal(err)
	}
	for i, frame := range plain.frames {
		length := func(parts [][]byte) int {
			n := 0
			for _, part := range parts {
				n += len(part)
			}
			return n
		}
		if length(frame.data) <= length(compressed.frames[i].data) {
			t.Fatal("APNG frame compression ignored", i)
		}
	}
	for _, result := range results {
		sequence, err := decodeAPNG(t.Context(), result.Bytes(), e.Limits())
		if err != nil || len(sequence.frames) != 2 || sequence.plays != 3 {
			t.Fatal("APNG compression changed sequence", err)
		}
		assertPixel(t, sequence.frames[0], 0, 0, color.NRGBA{R: 255, A: 128})
		assertPixel(t, sequence.frames[1], 0, 0, color.NRGBA{B: 255, A: 255})
		if sequence.delays[0] != (frameDelay{1, 10}) || sequence.delays[1] != (frameDelay{3, 10}) {
			t.Fatal("compression changed timing")
		}
	}
}

func TestAVIFExplicitZeroSpeedAndIndependentAlphaQuality(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	input := pngInput(t, 24, 24)
	img, err := png.Decode(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	base := NewPlan().Format(AVIF).AVIFQuality(45)
	var slow, fast Result
	for _, speed := range []int{0, DefaultAVIFSpeed} {
		result, err := e.ProcessBytes(t.Context(), input, base.AVIFSpeed(speed))
		if err != nil {
			t.Fatal(err)
		}
		var expected bytes.Buffer
		if err := avif.Encode(&expected, img, avif.EncodeOptions{Quality: 45, Speed: speed}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(result.Bytes(), expected.Bytes()) {
			t.Fatal("AVIF speed was not forwarded", speed)
		}
		if speed == 0 {
			slow = result
		} else {
			fast = result
		}
	}
	if bytes.Equal(slow.Bytes(), fast.Bytes()) {
		t.Fatal("fixture did not exercise different encoder searches")
	}
	alpha := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			alpha.SetNRGBA(x, y, color.NRGBA{R: byte(x * 7), G: byte(y * 7), B: 200, A: byte(32 + (x*17+y*7)%224)})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, alpha); err != nil {
		t.Fatal(err)
	}
	result, err := e.ProcessBytes(t.Context(), encoded.Bytes(), NewPlan().Format(AVIF).AVIFQuality(5).AVIFAlphaQuality(100))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := avif.Decode(result.Reader())
	if err != nil {
		t.Fatal(err)
	}
	for y := range 32 {
		for x := range 32 {
			_, _, _, a := decoded.At(x, y).RGBA()
			if a != uint32(alpha.NRGBAAt(x, y).A)*257 {
				t.Fatal("independent alpha quality lost exact alpha", x, y)
			}
		}
	}
}

func TestEveryEncoderHonorsOutputFailure(t *testing.T) {
	config := DefaultConfig()
	config.Limits.OutputBytes = 16
	e := testEngine(t, config)
	input := pngInput(t, 4, 4)
	for _, capability := range portableFormats() {
		t.Run(string(capability.Format), func(t *testing.T) {
			result, err := e.ProcessBytes(t.Context(), input, NewPlan().Format(capability.Format))
			if err == nil || result.Size() != 0 {
				t.Fatal("encoder published truncated output", capability.Format, err)
			}
		})
	}
	writer := boundedOutput{maximum: 4}
	if _, err := writer.Write([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte{4, 5}); err == nil {
		t.Fatal("writer ignored overflow")
	}
	if _, err := writer.Write([]byte{6}); err == nil || len(writer.data) != 3 {
		t.Fatal("writer recovered from terminal error")
	}
}
