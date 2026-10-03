package imaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/gen2brain/vpx/webp"
)

func webpInput(t testing.TB, kind string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/webp/animation-" + kind + ".webp")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func webpSequence(t testing.TB, data []byte) imageSequence {
	t.Helper()
	c, err := parseWebP(data, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	s, err := decodeWebPSequence(t.Context(), c, false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func assertImagePixels(t testing.TB, got, want image.Image, alphaOnly bool) {
	t.Helper()
	if got.Bounds().Size() != want.Bounds().Size() {
		t.Fatal("different image bounds")
	}
	for y := 0; y < want.Bounds().Dy(); y++ {
		for x := 0; x < want.Bounds().Dx(); x++ {
			g := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
			w := color.NRGBAModel.Convert(want.At(x, y)).(color.NRGBA)
			// Invisible RGB is not a rendered color; libwebp may canonicalize it.
			if g.A != w.A || !alphaOnly && w.A != 0 && g != w {
				t.Fatalf("pixel %d,%d: got %v want %v", x, y, g, w)
			}
		}
	}
}

func TestWebPAnimationIndependentFixtures(t *testing.T) {
	for _, kind := range []string{"lossless", "lossy"} {
		t.Run(kind, func(t *testing.T) {
			input := webpInput(t, kind)
			info, err := Inspect(input, DefaultLimits())
			if err != nil || info.Width != 16 || info.Height != 12 || info.Images != 4 || !info.Animated {
				t.Fatal(info, err)
			}
			sequence := webpSequence(t, input)
			if sequence.plays != 2 || len(sequence.frames) != 4 {
				t.Fatal("frame count or loop count changed")
			}
			for i, frame := range sequence.frames {
				if sequence.delays[i] != (frameDelay{[]uint32{30, 70, 110, 130}[i], 1000}) {
					t.Fatal("delay changed")
				}
				data, err := os.ReadFile(fmt.Sprintf("testdata/webp/%s-%d.png", kind, i))
				if err != nil {
					t.Fatal(err)
				}
				want, err := png.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				assertImagePixels(t, frame, want, kind == "lossy")
			}
		})
	}
}

func TestWebPAnimationPolicyTransformsAndRoundTrip(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	input := webpInput(t, "lossless")
	if result, err := e.ProcessBytes(t.Context(), input, NewPlan()); err == nil || result.Size() != 0 {
		t.Fatal("implicit animation")
	}
	if result, err := e.ProcessBytes(t.Context(), input, NewPlan().Frames(PreserveAnimation).Format(JPEG)); err == nil || result.Size() != 0 {
		t.Fatal("discarded frames")
	}
	original := webpSequence(t, input)
	first, err := e.ProcessBytes(t.Context(), input, NewPlan().Frames(FirstFrame).Format(PNG))
	if err != nil || first.Info().Animated || first.Info().Images != 1 {
		t.Fatal(err)
	}
	firstImage, err := png.Decode(first.Reader())
	if err != nil {
		t.Fatal(err)
	}
	assertImagePixels(t, firstImage, original.frames[0], false)

	for _, format := range []Format{WebP, PNG} {
		result, err := e.ProcessBytes(t.Context(), input, NewPlan().Frames(PreserveAnimation).Format(format))
		if err != nil {
			t.Fatal(err)
		}
		info, err := inspect(result.data, e.Limits())
		if err != nil || info.Info != result.Info() {
			t.Fatal("output metadata", err)
		}
		decoded, err := decodeSequence(t.Context(), result.data, info, e.Limits())
		if err != nil || decoded.plays != original.plays {
			t.Fatal("round trip", err)
		}
		for i, frame := range decoded.frames {
			assertImagePixels(t, frame, original.frames[i], false)
		}
		back, err := e.ProcessBytes(t.Context(), result.data, NewPlan().Frames(PreserveAnimation).Format(WebP))
		if err != nil {
			t.Fatal(err)
		}
		for i, frame := range webpSequence(t, back.data).frames {
			assertImagePixels(t, frame, original.frames[i], false)
		}
	}

	badge, err := e.Create(t.Context(), 2, 2, color.NRGBA{G: 255, A: 255}, NewPlan().Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	plan := NewPlan().Frames(PreserveAnimation).Resampling(NearestNeighbor).Resize(32, 24).
		Insert(badge, Placement{Position: BottomRight}).Format(WebP)
	result, err := e.ProcessBytes(t.Context(), input, plan)
	if err != nil || result.Info().Width != 32 || result.Info().Height != 24 {
		t.Fatal(err)
	}
	for _, frame := range webpSequence(t, result.data).frames {
		assertPixel(t, frame, 31, 23, color.NRGBA{G: 255, A: 255})
	}
	capability, ok := e.Capabilities().ForFormat(WebP)
	if !ok || !capability.ReadAnimation || !capability.WriteAnimation {
		t.Fatal("missing WebP animation capabilities")
	}
}

// Build controls independently of the animation encoder. Pixels use the existing
// lossless codec so these fixtures can focus on placement/compositing semantics.
func webpAnimationFixture(t testing.TB, w, h int, bg color.NRGBA, plays uint16, frames []apngTestFrame) []byte {
	t.Helper()
	x := make([]byte, 10)
	x[0] = 0x12
	putWebPUint24(x[4:7], uint32(w-1))
	putWebPUint24(x[7:10], uint32(h-1))
	animation := []byte{bg.B, bg.G, bg.R, bg.A, byte(plays), byte(plays >> 8)}
	chunks := [][]byte{webpTestChunk("VP8X", x), webpTestChunk("ANIM", animation)}
	for _, f := range frames {
		var pixels bytes.Buffer
		if err := webp.Encode(newWebPOutput(&pixels), f.img, webp.EncodeOptions{Lossless: true, Exact: true, Threads: 1}); err != nil {
			t.Fatal(err)
		}
		control := make([]byte, 16)
		putWebPUint24(control[:3], uint32(f.x/2))
		putWebPUint24(control[3:6], uint32(f.y/2))
		putWebPUint24(control[6:9], uint32(f.img.Bounds().Dx()-1))
		putWebPUint24(control[9:12], uint32(f.img.Bounds().Dy()-1))
		putWebPUint24(control[12:15], f.delay.numerator)
		control[15] = f.dispose | (1-f.blend)<<1
		chunks = append(chunks, webpTestChunk("ANMF", append(control, pixels.Bytes()[12:]...)))
	}
	return webpTestFile(chunks...)
}

func TestWebPBackgroundBlendDisposalAndOneFrame(t *testing.T) {
	bg := color.NRGBA{R: 20, G: 40, B: 60, A: 255}
	input := webpAnimationFixture(t, 4, 4, bg, 1, []apngTestFrame{
		{img: solidImage(2, 2, color.NRGBA{R: 220, A: 128}), x: 2, blend: 1, dispose: 1},
		{img: solidImage(2, 2, color.Transparent)},
	})
	s := webpSequence(t, input)
	assertPixel(t, s.frames[0], 0, 0, bg)
	assertPixel(t, s.frames[0], 2, 0, color.NRGBA{R: 120, G: 19, B: 29, A: 255})
	assertPixel(t, s.frames[1], 2, 0, bg)            // Disposal restores the declared background.
	assertPixel(t, s.frames[1], 0, 0, color.NRGBA{}) // No-blend replaces opaque pixels.
	e := testEngine(t, DefaultConfig())
	first, err := e.ProcessBytes(t.Context(), input, NewPlan().Frames(FirstFrame).Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(first.Reader())
	if err != nil {
		t.Fatal(err)
	}
	assertImagePixels(t, img, s.frames[0], false)

	one := webpAnimationFixture(t, 4, 4, color.NRGBA{}, 0, []apngTestFrame{{img: solidImage(1, 1, color.White), x: 2, y: 2}})
	result, err := e.ProcessBytes(t.Context(), one, NewPlan().Frames(PreserveAnimation).Format(WebP))
	if err != nil || !result.Info().Animated || result.Info().Images != 1 {
		t.Fatal("one-frame animation", err)
	}
	info, err := Inspect(result.data, e.Limits())
	if err != nil || !info.Animated || info.Images != 1 {
		t.Fatal("one-frame encoding", info, err)
	}
	assertPixel(t, webpSequence(t, result.data).frames[0], 2, 2, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
}

func TestWebPAnimationBoundsAndCancellation(t *testing.T) {
	input := webpInput(t, "lossless")
	plan := NewPlan().Frames(PreserveAnimation).Format(WebP)
	for _, change := range []func(*Limits){
		func(l *Limits) { l.InputBytes = int64(len(input) - 1) },
		func(l *Limits) { l.OutputBytes = 50 },
		func(l *Limits) { l.Width = 15 },
		func(l *Limits) { l.Frames = 3 },
		func(l *Limits) { l.Pixels = 16*12*4 - 1 },
		func(l *Limits) { l.OutputBytes = 1024; l.WorkingBytes = 4096 },
	} {
		cfg := DefaultConfig()
		change(&cfg.Limits)
		e := testEngine(t, cfg)
		if result, err := e.ProcessBytes(t.Context(), input, plan); err == nil || result.Size() != 0 {
			t.Fatal("animation exceeded limits")
		}
	}
	e := testEngine(t, DefaultConfig())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := e.ProcessBytes(ctx, input, plan); err == nil || result.Size() != 0 {
		t.Fatal("cancelled output")
	}
	if _, err := e.ProcessBytes(t.Context(), input, plan); err != nil {
		t.Fatal("capacity not reusable", err)
	}
	cfg := DefaultConfig()
	cfg.Limits.Width = 20000
	wide := testEngine(t, cfg)
	if result, err := wide.Create(t.Context(), 16385, 1, color.NRGBA{}, NewPlan().Format(WebP)); err == nil || result.Size() != 0 {
		t.Fatal("unrepresentable WebP dimension")
	}
}

func TestAnimationDelayRepresentability(t *testing.T) {
	img := solidImage(2, 2, color.White)
	for _, test := range []struct {
		delay frameDelay
		want  uint32
		valid bool
	}{
		{frameDelay{1, 2000}, 1, true}, {frameDelay{16777215, 1000}, 16777215, true},
		{frameDelay{16777216, 1000}, 0, false}, {frameDelay{1, 0}, 0, false},
	} {
		out := boundedOutput{maximum: 1 << 20}
		err := encodeWebPSequence(t.Context(), &out, imageSequence{frames: []image.Image{img}, delays: []frameDelay{test.delay}}, NewPlan(), DefaultLimits())
		if (err == nil) != test.valid {
			t.Fatalf("delay %v: %v", test.delay, err)
		}
		if err == nil && webpSequence(t, out.data).delays[0].numerator != test.want {
			t.Fatal("WebP rounding")
		}
	}
	for _, plays := range []uint32{0, 1, 65535, 65536} {
		out := boundedOutput{maximum: 1 << 20}
		err := encodeWebPSequence(t.Context(), &out, imageSequence{frames: []image.Image{img}, delays: []frameDelay{{1, 10}}, plays: plays}, NewPlan(), DefaultLimits())
		if (err == nil) != (plays <= 65535) {
			t.Fatal("loop representability", plays, err)
		}
	}
	out := boundedOutput{maximum: 1 << 20}
	if err := encodeAPNG(t.Context(), &out, imageSequence{frames: []image.Image{img}, delays: []frameDelay{{100000, 1000000}}, plays: 2}, encodingOptions{}, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeAPNG(t.Context(), out.data, DefaultLimits())
	if err != nil || decoded.delays[0] != (frameDelay{1, 10}) {
		t.Fatal("reducible fractional delay", err)
	}
}

func TestExtendedLosslessWebPAlpha(t *testing.T) {
	var encoded bytes.Buffer
	img := solidImage(4, 2, color.NRGBA{R: 17, G: 95, B: 201, A: 128})
	if err := webp.Encode(newWebPOutput(&encoded), img, webp.EncodeOptions{Lossless: true, Exact: true, Threads: 1}); err != nil {
		t.Fatal(err)
	}
	canvas := make([]byte, 10)
	canvas[0], canvas[4], canvas[7] = 0x10, 3, 1
	data := webpTestFile(webpTestChunk("VP8X", canvas), encoded.Bytes()[12:])
	result, err := testEngine(t, DefaultConfig()).ProcessBytes(t.Context(), data, NewPlan().Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	back, err := png.Decode(result.Reader())
	if err != nil {
		t.Fatal(err)
	}
	assertImagePixels(t, back, img, false)
}

func TestWebPRejectsMalformedAnimationContainers(t *testing.T) {
	valid := webpInput(t, "lossless")
	for _, test := range []struct {
		name, kind string
		change     func([]byte) []byte
	}{
		{"truncated-animation", "ANIM", func(b []byte) []byte { return b[:5] }},
		{"empty-frame", "ANMF", func(b []byte) []byte { return b[:16] }},
		{"truncated-control", "ANMF", func(b []byte) []byte { return b[:15] }},
		{"outside-canvas", "ANMF", func(b []byte) []byte { b[0] = 8; return b }},
		{"coded-size-mismatch", "ANMF", func(b []byte) []byte { b[6]--; return b }},
		{"duplicate-codec", "ANMF", func(b []byte) []byte { return append(b, b[16:]...) }},
		{"no-animation-flag", "VP8X", func(b []byte) []byte { b[0] &= ^byte(2); return b }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var chunks [][]byte
			count := 0
			changed := false
			if err := walkWebPChunks(valid[12:], &count, func(kind string, body []byte) error {
				body = bytes.Clone(body)
				if kind == test.kind && !changed {
					body = test.change(body)
					changed = true
				}
				chunks = append(chunks, webpTestChunk(kind, body))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := parseWebP(webpTestFile(chunks...), DefaultLimits()); err == nil {
				t.Fatal("accepted malformed animation")
			}
		})
	}
	// Wrong file length, nonzero padding and an image-free container.
	badLength := bytes.Clone(valid)
	binary.LittleEndian.PutUint32(badLength[4:], uint32(len(valid)))
	padding := webpTestChunk("junk", []byte{1})
	padding[len(padding)-1] = 1
	for _, data := range [][]byte{badLength, webpTestFile(padding), webpTestFile(), webpTestFile(webpTestChunk("ANMF", make([]byte, 16)))} {
		if _, err := parseWebP(data, DefaultLimits()); err == nil {
			t.Fatal("invalid RIFF structure")
		}
	}
}

func TestWebPRawAlphaAndContainerOrdering(t *testing.T) {
	container, err := parseWebP(webpInput(t, "lossy"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	frame := container.frames[0]
	x := make([]byte, 10)
	x[0] = 0x10
	putWebPUint24(x[4:7], uint32(frame.bounds.Dx()-1))
	putWebPUint24(x[7:10], uint32(frame.bounds.Dy()-1))
	alpha := bytes.Repeat([]byte{128}, 1+frame.bounds.Dx()*frame.bounds.Dy())
	alpha[0] = 0
	xchunk, achunk, pchunk := webpTestChunk("VP8X", x), webpTestChunk("ALPH", alpha), webpTestChunk(frame.kind, frame.bitstream)
	valid := webpTestFile(xchunk, achunk, pchunk)
	result, err := testEngine(t, DefaultConfig()).ProcessBytes(t.Context(), valid, NewPlan().Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(result.Reader())
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 128*257 {
				t.Fatal("raw alpha changed")
			}
		}
	}
	for _, data := range [][]byte{
		webpTestFile(achunk, pchunk), webpTestFile(xchunk, pchunk, achunk), webpTestFile(xchunk, achunk, achunk, pchunk),
		webpTestFile(xchunk, webpTestChunk("ALPH", alpha[:len(alpha)-1]), pchunk),
		webpTestFile(xchunk, pchunk, xchunk), webpTestFile(xchunk, achunk, pchunk, webpTestChunk("ICCP", []byte{1, 2})),
	} {
		if _, err := parseWebP(data, DefaultLimits()); err == nil {
			t.Fatal("malformed or reordered alpha accepted")
		}
	}
	// Reserved flags are ignored on read, as required by the WebP container.
	x[0] |= 0xc1
	x[1] = 255
	if _, err := parseWebP(webpTestFile(webpTestChunk("VP8X", x), achunk, pchunk), DefaultLimits()); err != nil {
		t.Fatal("reserved flags rejected", err)
	}
}

func BenchmarkWebPAnimationWorkspace(b *testing.B) {
	for _, width := range []int{64, 512} {
		b.Run(fmt.Sprint(width), func(b *testing.B) {
			frames := []apngTestFrame{
				{img: solidImage(width, width/2, color.NRGBA{R: 255, A: 255}), delay: frameDelay{40, 1000}},
				{img: solidImage(width, width/2, color.NRGBA{B: 255, A: 128}), delay: frameDelay{80, 1000}},
				{img: solidImage(width, width/2, color.NRGBA{G: 255, A: 255}), delay: frameDelay{120, 1000}},
				{img: solidImage(width, width/2, color.Transparent), delay: frameDelay{160, 1000}},
			}
			input := webpAnimationFixture(b, width, width/2, color.NRGBA{}, 0, frames)
			config := DefaultConfig()
			config.Limits.OutputBytes = 1 << 20
			e, err := New(config)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() {
				if err := e.Close(context.Background()); err != nil {
					b.Error(err)
				}
			})
			plan := NewPlan().Frames(PreserveAnimation).Format(WebP)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := e.ProcessBytes(b.Context(), input, plan); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestLossyWebPUsesVideoRangeAndCenteredChroma(t *testing.T) {
	sequence := webpSequence(t, webpInput(t, "lossy"))
	for i, frame := range sequence.frames {
		data, err := os.ReadFile(fmt.Sprintf("testdata/webp/lossy-%d.png", i))
		if err != nil {
			t.Fatal(err)
		}
		want, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < frame.Bounds().Dy(); y++ {
			for x := 0; x < frame.Bounds().Dx(); x++ {
				g := color.NRGBAModel.Convert(frame.At(x, y)).(color.NRGBA)
				w := color.NRGBAModel.Convert(want.At(x, y)).(color.NRGBA)
				if g.A != w.A {
					t.Fatal("alpha mismatch")
				}
				if w.A == 0 {
					continue
				}
				for k, v := range []byte{g.R, g.G, g.B} {
					diff := int(v) - int([]byte{w.R, w.G, w.B}[k])
					if diff < -3 || diff > 3 {
						t.Fatalf("frame %d pixel %d,%d: %v want %v", i, x, y, g, w)
					}
				}
			}
		}
	}
	// Extract a lossy frame as a static WebP: both paths must share conversion.
	c, err := parseWebP(webpInput(t, "lossy"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	frame := c.frames[3]
	var chunks [][]byte
	if frame.alpha != nil {
		x := make([]byte, 10)
		x[0] = 16
		putWebPUint24(x[4:7], uint32(frame.bounds.Dx()-1))
		putWebPUint24(x[7:10], uint32(frame.bounds.Dy()-1))
		chunks = append(chunks, webpTestChunk("VP8X", x), webpTestChunk("ALPH", frame.alpha))
	}
	chunks = append(chunks, webpTestChunk(frame.kind, frame.bitstream))
	result, err := testEngine(t, DefaultConfig()).ProcessBytes(t.Context(), webpTestFile(chunks...), NewPlan().Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(result.Reader())
	if err != nil {
		t.Fatal(err)
	}
	assertPixel(t, img, 0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
}

func TestWebPFixedEncoderWorkspaceAndPaddedDecode(t *testing.T) {
	config := DefaultConfig()
	config.Limits.WorkingBytes = 16 << 20
	config.Limits.OutputBytes = 1024
	if result, err := testEngine(t, config).Create(t.Context(), 2, 2, color.NRGBA{}, NewPlan().Format(WebP)); err == nil || result.Size() != 0 {
		t.Fatal("fixed encoder workspace escaped admission")
	}
	lossy := []byte{0x10, 0, 0, 0x9d, 1, 0x2a, 1, 0, 0xff, 0x1f}
	container, err := parseWebP(webpTestFile(webpTestChunk("VP8 ", lossy)), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if container.workspace() < 16*8192*WebP.decodePeakBytes() {
		t.Fatal("macroblock padding escaped admission")
	}
}
