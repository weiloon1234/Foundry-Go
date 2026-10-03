package imaging

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"sync"
	"testing"
)

func webpEncodingImage(width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{R: byte(x*3 + y), G: byte(y*5 + x), B: byte(x*7 + y*3), A: byte(x*13 + y*7)})
		}
	}
	return img
}

func TestWebPEncodingControlsValidateAndRemainImmutable(t *testing.T) {
	for _, p := range []Plan{
		NewPlan().WebPMode(WebPLossless), NewPlan().Format(PNG).WebPMode(WebPLossy),
		NewPlan().Format(WebP).WebPMode(WebPMode(255)),
		NewPlan().Format(WebP).WebPQuality(80),
		NewPlan().Format(WebP).WebPMode(WebPLossy).WebPQuality(0),
		NewPlan().Format(WebP).WebPMode(WebPLossy).WebPQuality(101),
		NewPlan().WebPMethod(0), NewPlan().Format(WebP).WebPMethod(-1),
		NewPlan().Format(WebP).WebPMethod(7),
	} {
		if p.Validate() == nil {
			t.Fatal("invalid WebP controls accepted")
		}
	}
	base := NewPlan().Format(WebP)
	derived := base.WebPMode(WebPLossy).WebPQuality(80).WebPMethod(0)
	if derived.Validate() != nil || !derived.encoding.webpLossy() || base.encoding.webpMode.IsSet() || base.encoding.webpMethod.IsSet() {
		t.Fatal("WebP plan mutation or invalid derived plan")
	}
	method, set := derived.encoding.webpMethod.Get()
	if !set || method != 0 {
		t.Fatal("explicit fastest method lost")
	}
}

func TestWebPColorQualityLosslessAlphaAndHiddenRGB(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	img := webpEncodingImage(64, 48)
	var input bytes.Buffer
	if err := png.Encode(&input, img); err != nil {
		t.Fatal(err)
	}
	var low, high Result
	for _, method := range []int{0, DefaultWebPMethod, 6} {
		for _, mode := range []WebPMode{WebPLossless, WebPLossy} {
			for _, quality := range []int{10, 95} {
				p := NewPlan().Format(WebP).WebPMode(mode).WebPMethod(method)
				if mode == WebPLossy {
					p = p.WebPQuality(quality)
				}
				result, err := e.ProcessBytes(t.Context(), input.Bytes(), p)
				if err != nil {
					t.Fatal(err)
				}
				container, err := parseWebP(result.data, e.Limits())
				if err != nil || (container.frames[0].kind == "VP8 ") != (mode == WebPLossy) {
					t.Fatal("wrong WebP compression mode", err)
				}
				got, err := decodeWebPFrame(t.Context(), container.frames[0])
				if err != nil {
					t.Fatal(err)
				}
				for y := range img.Bounds().Dy() {
					for x := range img.Bounds().Dx() {
						want := img.NRGBAAt(x, y)
						actual := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
						if actual.A != want.A || mode == WebPLossless && actual != want {
							t.Fatal("WebP alpha or lossless RGB changed", mode, method, x, y, actual, want)
						}
					}
				}
				if mode == WebPLossy && method == DefaultWebPMethod {
					if quality == 10 {
						low = result
					} else {
						high = result
					}
				}
			}
		}
	}
	if low.Size() >= high.Size() || bytes.Equal(low.data, high.data) {
		t.Fatal("lossy quality did not affect encoded color", low.Size(), high.Size())
	}
}

func TestLossyWebPFromJPEGUsesFullRangeColor(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 16))
	for y := range 16 {
		for x := range 64 {
			v := byte(x/16*70 + 20)
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	var input bytes.Buffer
	if err := jpeg.Encode(&input, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	result, err := testEngine(t, DefaultConfig()).ProcessBytes(t.Context(), input.Bytes(), NewPlan().Format(WebP).WebPMode(WebPLossy).WebPQuality(100))
	if err != nil {
		t.Fatal(err)
	}
	c, err := parseWebP(result.data, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeWebPFrame(t.Context(), c.frames[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []int{8, 24, 40, 56} {
		actual := color.NRGBAModel.Convert(got.At(x, 8)).(color.NRGBA)
		want := int(img.NRGBAAt(x, 8).R)
		for _, v := range []byte{actual.R, actual.G, actual.B} {
			if int(v) < want-3 || int(v) > want+3 {
				t.Fatal("full-range JPEG colors shifted", actual, want)
			}
		}
	}
}

func TestLossyWebPAnimationUsesControlsOnEveryFrame(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	p := NewPlan().Frames(PreserveAnimation).Format(WebP).WebPMode(WebPLossy).WebPQuality(80).WebPMethod(0)
	result, err := e.ProcessBytes(t.Context(), webpInput(t, "lossless"), p)
	if err != nil {
		t.Fatal(err)
	}
	original, err := parseWebP(webpInput(t, "lossless"), e.Limits())
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseWebP(result.data, e.Limits())
	if err != nil || !got.animated || len(got.frames) != len(original.frames) || got.plays != original.plays {
		t.Fatal("animation changed", err)
	}
	for i, frame := range got.frames {
		if frame.kind != "VP8 " || frame.delay != original.frames[i].delay {
			t.Fatal("frame lost encoding mode or timing")
		}
	}
	if _, err := decodeWebPSequence(t.Context(), got, false); err != nil {
		t.Fatal(err)
	}
}

func TestWebPModeBoundsFailuresAndConcurrentReuse(t *testing.T) {
	config := DefaultConfig()
	config.Limits.Width = 16384
	e := testEngine(t, config)
	lossy := NewPlan().Format(WebP).WebPMode(WebPLossy).WebPMethod(0)
	if out, err := e.Create(t.Context(), 16384, 1, color.NRGBA{}, lossy); err == nil || out.Size() != 0 {
		t.Fatal("lossy dimension overflow admitted")
	}
	config.Limits.OutputBytes = 16
	if out, err := testEngine(t, config).Create(t.Context(), 16, 16, color.NRGBA{R: 255, A: 127}, lossy); err == nil || out.Size() != 0 {
		t.Fatal("lossy output limit ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if out, err := e.Create(ctx, 16, 16, color.NRGBA{}, lossy); err == nil || out.Size() != 0 {
		t.Fatal("canceled encode published")
	}
	// Padded macroblocks must dominate the area of an unusually narrow image.
	thin := image.Rect(0, 0, 1, 8192)
	if lossy.encodeWorkspace(WebP, thin) < 16*8192*192 {
		t.Fatal("lossy padded workspace omitted")
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			out, err := e.Create(t.Context(), 16, 16, color.NRGBA{G: 180, A: 128}, lossy)
			if err != nil || out.Size() == 0 {
				t.Error("concurrent immutable plan", err)
			}
		})
	}
	wg.Wait()
	if _, err := e.Create(t.Context(), 4, 4, color.NRGBA{}, lossy); err != nil {
		t.Fatal("capacity not reusable", err)
	}
}

func BenchmarkWebPEncodingWorkspace(b *testing.B) {
	for _, mode := range []WebPMode{WebPLossless, WebPLossy} {
		for _, method := range []int{0, 4, 6} {
			b.Run(fmt.Sprintf("mode=%d/method=%d", mode, method), func(b *testing.B) {
				img := webpEncodingImage(256, 256)
				p := NewPlan().Format(WebP).WebPMode(mode).WebPMethod(method)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					out := boundedOutput{maximum: 4 << 20, ctx: b.Context()}
					if err := p.encode(&out, img, WebP, 4<<20); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestWebPOutputCanonicalPaddingAcrossWriteBoundaries(t *testing.T) {
	good := webpTestFile(webpTestChunk("ODD!", []byte{1, 2, 3}), webpTestChunk("EVEN", []byte{4, 5}))
	bad := bytes.Clone(good)
	bad[23] = 'O'
	for step := 1; step <= len(bad); step++ {
		var out bytes.Buffer
		w := newWebPOutput(&out)
		for at := 0; at < len(bad); at += step {
			part := bad[at:min(at+step, len(bad))]
			if n, err := w.Write(part); err != nil || n != len(part) {
				t.Fatal("stream write", n, err)
			}
		}
		if !bytes.Equal(out.Bytes(), good) {
			t.Fatal("padding changed payload or header", step)
		}
	}
	out := boundedOutput{maximum: 16}
	w := newWebPOutput(&out)
	if _, err := w.Write(bad); err == nil {
		t.Fatal("writer error lost")
	}
	if _, err := w.Write([]byte{1}); err == nil {
		t.Fatal("writer recovered after failure")
	}
}

func FuzzWebPEncoding(f *testing.F) {
	f.Add([]byte{0, 127, 255, 42, 63, 211}, byte(0), byte(4))
	f.Add([]byte{255, 0, 0, 128}, byte(1), byte(6))
	f.Fuzz(func(t *testing.T, pixels []byte, mode, effort byte) {
		if len(pixels) == 0 || len(pixels) > 1024 {
			return
		}
		img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for i := range img.Pix {
			img.Pix[i] = pixels[i%len(pixels)]
		}
		var input bytes.Buffer
		if err := png.Encode(&input, img); err != nil {
			t.Fatal(err)
		}
		p := NewPlan().Format(WebP).WebPMode(WebPMode(mode % 2)).WebPMethod(int(effort % 7))
		result, err := testEngine(t, DefaultConfig()).ProcessBytes(t.Context(), input.Bytes(), p)
		if err != nil {
			t.Fatal(err)
		}
		container, err := parseWebP(result.data, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeWebPFrame(t.Context(), container.frames[0])
		if err != nil {
			t.Fatal(err)
		}
		for y := range 8 {
			for x := range 8 {
				got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
				want := img.NRGBAAt(x, y)
				if got.A != want.A || mode%2 == 0 && got != want {
					t.Fatal("WebP encoding lost alpha or lossless RGB")
				}
			}
		}
	})
}
