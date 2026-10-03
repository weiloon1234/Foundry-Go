package imaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"testing"
)

type apngTestFrame struct {
	img            image.Image
	x, y           int
	delay          frameDelay
	dispose, blend byte
}

func solidImage(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

// Constructs standard APNG chunks directly, including frame rectangles,
// disposal/blending and an optional non-animation default image.
func apngFixture(t testing.TB, w, h int, plays uint32, poster image.Image, frames []apngTestFrame) []byte {
	t.Helper()
	var out bytes.Buffer
	out.WriteString(pngSignature)
	var header [13]byte
	binary.BigEndian.PutUint32(header[:4], uint32(w))
	binary.BigEndian.PutUint32(header[4:8], uint32(h))
	header[8], header[9] = 8, 6
	if err := writePNGChunk(&out, "IHDR", header[:]); err != nil {
		t.Fatal(err)
	}
	var animation [8]byte
	binary.BigEndian.PutUint32(animation[:4], uint32(len(frames)))
	binary.BigEndian.PutUint32(animation[4:], plays)
	if err := writePNGChunk(&out, "acTL", animation[:]); err != nil {
		t.Fatal(err)
	}
	var seq uint32
	writeData := func(img image.Image, asDefault bool) {
		var frame bytes.Buffer
		if err := png.Encode(&frame, rgbaPNGFrame{img}); err != nil {
			t.Fatal(err)
		}
		if err := walkPNG(frame.Bytes(), func(kind string, body []byte) error {
			if kind != "IDAT" {
				return nil
			}
			if asDefault {
				return writePNGChunk(&out, "IDAT", body)
			}
			var prefix [4]byte
			binary.BigEndian.PutUint32(prefix[:], seq)
			seq++
			return writePNGChunk(&out, "fdAT", prefix[:], body)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if poster != nil {
		writeData(poster, true)
	}
	for i, frame := range frames {
		var control [26]byte
		binary.BigEndian.PutUint32(control[:4], seq)
		seq++
		binary.BigEndian.PutUint32(control[4:8], uint32(frame.img.Bounds().Dx()))
		binary.BigEndian.PutUint32(control[8:12], uint32(frame.img.Bounds().Dy()))
		binary.BigEndian.PutUint32(control[12:16], uint32(frame.x))
		binary.BigEndian.PutUint32(control[16:20], uint32(frame.y))
		binary.BigEndian.PutUint16(control[20:22], uint16(frame.delay.numerator))
		binary.BigEndian.PutUint16(control[22:24], uint16(frame.delay.denominator))
		control[24], control[25] = frame.dispose, frame.blend
		if err := writePNGChunk(&out, "fcTL", control[:]); err != nil {
			t.Fatal(err)
		}
		writeData(frame.img, i == 0 && poster == nil)
	}
	if err := writePNGChunk(&out, "IEND"); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestAPNGPreservesTimingBlendingDisposalAndTransforms(t *testing.T) {
	red, blue, white := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}, color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	input := apngFixture(t, 4, 3, 3, nil, []apngTestFrame{
		{img: solidImage(4, 3, red), delay: frameDelay{7, 100}},
		{img: solidImage(2, 2, color.NRGBA{G: 255, A: 128}), x: 1, delay: frameDelay{11, 1000}, dispose: 2, blend: 1},
		{img: solidImage(1, 1, blue), delay: frameDelay{13, 100}, dispose: 1},
		{img: solidImage(1, 1, white), x: 3, y: 2, delay: frameDelay{0, 0}},
	})
	e := testEngine(t, DefaultConfig())
	result, err := e.ProcessBytes(t.Context(), input, NewPlan().Frames(PreserveAnimation).Resampling(NearestNeighbor).Resize(8, 6))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Info(); !got.Animated || got.Images != 4 || got.Width != 8 || got.Height != 6 {
		t.Fatal("animation metadata", got)
	}
	sequence, err := decodeAPNG(t.Context(), result.Bytes(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if sequence.plays != 3 || sequence.delays[0] != (frameDelay{7, 100}) || sequence.delays[1] != (frameDelay{11, 1000}) || sequence.delays[3] != (frameDelay{0, 100}) {
		t.Fatal("timing/loops changed", sequence.plays, sequence.delays)
	}
	assertPixel(t, sequence.frames[0], 2, 0, red)
	assertPixel(t, sequence.frames[1], 2, 0, color.NRGBA{R: 127, G: 128, A: 255})
	assertPixel(t, sequence.frames[2], 2, 0, red)
	assertPixel(t, sequence.frames[2], 0, 0, blue)
	assertPixel(t, sequence.frames[3], 0, 0, color.NRGBA{})
	assertPixel(t, sequence.frames[3], 6, 4, white)
	// The first APNG frame remains a standard PNG to independent PNG decoders.
	first, err := png.Decode(result.Reader())
	if err != nil {
		t.Fatal(err)
	}
	assertPixel(t, first, 2, 0, red)
	if _, err := e.ProcessBytes(t.Context(), input, NewPlan()); err == nil {
		t.Fatal("animation implicitly accepted")
	}
	if _, err := e.ProcessBytes(t.Context(), input, NewPlan().Frames(PreserveAnimation).Format(JPEG)); err == nil {
		t.Fatal("animation silently discarded by output codec")
	}
}

func TestAPNGSeparatePosterAndSingleFrameAnimation(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	red, green := color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255}
	input := apngFixture(t, 4, 4, 1, solidImage(4, 4, red), []apngTestFrame{{img: solidImage(1, 1, green), x: 2, y: 2, delay: frameDelay{3, 10}, dispose: 2}})
	info, err := Inspect(input, DefaultLimits())
	if err != nil || !info.Animated || info.Images != 1 {
		t.Fatal("single-frame APNG", info, err)
	}
	preserved, err := e.ProcessBytes(t.Context(), input, NewPlan().Frames(PreserveAnimation))
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := decodeAPNG(t.Context(), preserved.Bytes(), DefaultLimits())
	if err != nil || sequence.plays != 1 {
		t.Fatal("single-frame preservation", err)
	}
	assertPixel(t, sequence.frames[0], 0, 0, color.NRGBA{})
	assertPixel(t, sequence.frames[0], 2, 2, green)
	poster, err := e.ProcessBytes(t.Context(), input, NewPlan().Frames(FirstFrame))
	if err != nil || poster.Info().Animated {
		t.Fatal("explicit default image", err)
	}
	assertPixel(t, resultImage(t, poster), 0, 0, red)
}

func TestGIFAnimationRoundTripsAcrossPNG(t *testing.T) {
	colors := color.Palette{color.Transparent, color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255}, color.NRGBA{B: 255, A: 255}}
	frame := func(bounds image.Rectangle, index uint8) *image.Paletted {
		img := image.NewPaletted(bounds, colors)
		for i := range img.Pix {
			img.Pix[i] = index
		}
		return img
	}
	var input bytes.Buffer
	err := gif.EncodeAll(&input, &gif.GIF{
		Image: []*image.Paletted{frame(image.Rect(0, 0, 4, 4), 1), frame(image.Rect(1, 1, 3, 3), 2), frame(image.Rect(0, 0, 1, 1), 3), frame(image.Rect(3, 3, 4, 4), 2)},
		Delay: []int{3, 5, 7, 9}, Disposal: []byte{gif.DisposalNone, gif.DisposalPrevious, gif.DisposalBackground, gif.DisposalNone}, LoopCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	e := testEngine(t, DefaultConfig())
	pngResult, err := e.ProcessBytes(t.Context(), input.Bytes(), NewPlan().Frames(PreserveAnimation).Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := decodeAPNG(t.Context(), pngResult.Bytes(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertPixel(t, sequence.frames[1], 1, 1, color.NRGBA{G: 255, A: 255})
	assertPixel(t, sequence.frames[2], 1, 1, color.NRGBA{R: 255, A: 255})
	assertPixel(t, sequence.frames[3], 0, 0, color.NRGBA{})
	back, err := e.ProcessBytes(t.Context(), pngResult.Bytes(), NewPlan().Frames(PreserveAnimation).Format(GIF))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := gif.DecodeAll(back.Reader())
	if err != nil || len(decoded.Image) != 4 || decoded.LoopCount != 2 {
		t.Fatal("GIF sequence output", err)
	}
	for i, want := range []int{3, 5, 7, 9} {
		if decoded.Delay[i] != want {
			t.Fatal("GIF delay changed")
		}
	}
	roundTrip, err := decodeGIFSequence(t.Context(), back.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	assertPixel(t, roundTrip.frames[3], 0, 0, color.NRGBA{})
	assertPixel(t, roundTrip.frames[3], 3, 3, color.NRGBA{G: 255, A: 255})
}

func TestAnimationResourceBoundsCancellationAndLayers(t *testing.T) {
	input := apngFixture(t, 4, 4, 0, nil, []apngTestFrame{{img: solidImage(4, 4, color.NRGBA{R: 255, A: 255}), delay: frameDelay{1, 100}}, {img: solidImage(4, 4, color.NRGBA{B: 255, A: 255}), delay: frameDelay{1, 100}}})
	plan := NewPlan().Frames(PreserveAnimation)
	for _, config := range []Config{
		func() Config { c := DefaultConfig(); c.Limits.Frames = 1; return c }(),
		func() Config { c := DefaultConfig(); c.Limits.Pixels = 31; return c }(),
		func() Config { c := DefaultConfig(); c.Limits.WorkingBytes = 1 << 20; return c }(),
		func() Config { c := DefaultConfig(); c.Limits.OutputBytes = 100; return c }(),
	} {
		if result, err := testEngine(t, config).ProcessBytes(t.Context(), input, plan); err == nil || result.Size() != 0 {
			t.Fatal("animation exceeded a configured bound")
		}
	}
	config := DefaultConfig()
	config.Limits.Pixels = 40
	if _, err := testEngine(t, config).ProcessBytes(t.Context(), input, plan.Resize(8, 8)); err == nil {
		t.Fatal("aggregate output pixels not bounded")
	}
	e := testEngine(t, DefaultConfig())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := e.ProcessBytes(ctx, input, plan); err == nil || result.Size() != 0 {
		t.Fatal("cancelled animation returned output")
	}
	watermark, err := e.Create(t.Context(), 1, 1, color.NRGBA{G: 255, A: 255}, NewPlan())
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.ProcessBytes(t.Context(), input, plan.Insert(watermark, Placement{Position: BottomRight}))
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := decodeAPNG(t.Context(), result.Bytes(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range sequence.frames {
		assertPixel(t, frame, 3, 3, color.NRGBA{G: 255, A: 255})
	}
}

func TestAPNGRejectsMalformedAnimationControls(t *testing.T) {
	input := apngFixture(t, 2, 2, 0, nil, []apngTestFrame{{img: solidImage(2, 2, color.White), delay: frameDelay{1, 100}}, {img: solidImage(2, 2, color.Black), delay: frameDelay{1, 100}}})
	for _, test := range []struct {
		kind   string
		change func([]byte)
	}{
		{"acTL", func(b []byte) { binary.BigEndian.PutUint32(b, 3) }},
		{"acTL", func(b []byte) { binary.BigEndian.PutUint32(b, 0) }},
		{"fcTL", func(b []byte) { binary.BigEndian.PutUint32(b, 5) }},
		{"fcTL", func(b []byte) { binary.BigEndian.PutUint32(b[4:], 3) }},
		{"fcTL", func(b []byte) { b[24] = 3 }},
		{"fcTL", func(b []byte) { b[25] = 2 }},
		{"fdAT", func(b []byte) { binary.BigEndian.PutUint32(b, 0) }},
	} {
		var changed bytes.Buffer
		changed.WriteString(pngSignature)
		done := false
		if err := walkPNG(input, func(kind string, body []byte) error {
			body = bytes.Clone(body)
			if kind == test.kind && !done {
				test.change(body)
				done = true
			}
			return writePNGChunk(&changed, kind, body)
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := Inspect(changed.Bytes(), DefaultLimits()); err == nil {
			t.Fatal("invalid APNG control accepted", test.kind)
		}
	}
}

func TestEngineCapabilitiesAreOwnedAndDistinguishOperations(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	capabilities := e.Capabilities()
	gif, ok := capabilities.ForFormat(GIF)
	if !ok || !gif.ReadAnimation || !gif.WriteAnimation {
		t.Fatal("missing GIF capability")
	}
	avif, ok := capabilities.ForFormat(AVIF)
	if !ok || !avif.Read || !avif.Write || !avif.ReadAnimation || avif.WriteAnimation {
		t.Fatal("incorrect AVIF capability")
	}
	capabilities.Formats[0].Read = false
	jpeg, _ := e.Capabilities().ForFormat(JPEG)
	if !jpeg.Read {
		t.Fatal("capability snapshot mutated engine")
	}
	if f, err := ParseExtension(".apng"); err != nil || f != PNG {
		t.Fatal("APNG extension", f, err)
	}
	if _, ok := capabilities.ForFormat(Format("unknown")); ok {
		t.Fatal("unknown format capability")
	}
}

func TestAPNGPaletteAndTransparency(t *testing.T) {
	colors := color.Palette{color.Transparent, color.NRGBA{R: 255, A: 255}}
	frame := image.NewPaletted(image.Rect(0, 0, 2, 2), colors)
	frame.SetColorIndex(1, 0, 1)
	var ordinary bytes.Buffer
	if err := png.Encode(&ordinary, frame); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	output.WriteString(pngSignature)
	var compressed [][]byte
	control := func(seq uint32) []byte {
		data := make([]byte, 26)
		binary.BigEndian.PutUint32(data, seq)
		binary.BigEndian.PutUint32(data[4:], 2)
		binary.BigEndian.PutUint32(data[8:], 2)
		binary.BigEndian.PutUint16(data[20:], 1)
		binary.BigEndian.PutUint16(data[22:], 10)
		return data
	}
	if err := walkPNG(ordinary.Bytes(), func(kind string, body []byte) error {
		if kind == "IEND" {
			return nil
		}
		if err := writePNGChunk(&output, kind, body); err != nil {
			return err
		}
		if kind == "IHDR" {
			if err := writePNGChunk(&output, "acTL", []byte{0, 0, 0, 2, 0, 0, 0, 1}); err != nil {
				return err
			}
			return writePNGChunk(&output, "fcTL", control(0))
		}
		if kind == "IDAT" {
			compressed = append(compressed, body)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := writePNGChunk(&output, "fcTL", control(1)); err != nil {
		t.Fatal(err)
	}
	for i, part := range compressed {
		var prefix [4]byte
		binary.BigEndian.PutUint32(prefix[:], uint32(i+2))
		if err := writePNGChunk(&output, "fdAT", prefix[:], part); err != nil {
			t.Fatal(err)
		}
	}
	if err := writePNGChunk(&output, "IEND"); err != nil {
		t.Fatal(err)
	}
	result, err := testEngine(t, DefaultConfig()).ProcessBytes(t.Context(), output.Bytes(), NewPlan().Frames(PreserveAnimation))
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := decodeAPNG(t.Context(), result.Bytes(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, img := range sequence.frames {
		assertPixel(t, img, 0, 0, color.NRGBA{})
		assertPixel(t, img, 1, 0, color.NRGBA{R: 255, A: 255})
	}
}

func TestAnimationAdmissionIncludesRetainedFrames(t *testing.T) {
	config := DefaultConfig()
	config.Limits.OutputBytes = 1000
	config.Limits.WorkingBytes = 3000
	e := testEngine(t, config)
	if _, err := e.ProcessBytes(t.Context(), pngInput(t, 4, 4), NewPlan()); err != nil {
		t.Fatal("ordinary frame should fit", err)
	}
	input := apngFixture(t, 4, 4, 0, nil, []apngTestFrame{{img: solidImage(4, 4, color.White), delay: frameDelay{1, 100}}, {img: solidImage(4, 4, color.Black), delay: frameDelay{1, 100}}})
	if _, err := e.ProcessBytes(t.Context(), input, NewPlan().Frames(PreserveAnimation)); err == nil {
		t.Fatal("retained animation buffers escaped admission")
	}
}

func TestGIFOpaqueBackgroundAndDisposal(t *testing.T) {
	yellow, blue := color.NRGBA{R: 255, G: 255, A: 255}, color.NRGBA{B: 255, A: 255}
	colors := color.Palette{yellow, blue}
	first := image.NewPaletted(image.Rect(1, 1, 2, 2), colors)
	first.SetColorIndex(1, 1, 1)
	second := image.NewPaletted(image.Rect(2, 2, 3, 3), colors)
	second.SetColorIndex(2, 2, 1)
	var input bytes.Buffer
	if err := gif.EncodeAll(&input, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{1, 1}, Disposal: []byte{gif.DisposalBackground, gif.DisposalNone}, Config: image.Config{Width: 3, Height: 3, ColorModel: colors}}); err != nil {
		t.Fatal(err)
	}
	result, err := testEngine(t, DefaultConfig()).ProcessBytes(t.Context(), input.Bytes(), NewPlan().Frames(PreserveAnimation).Format(PNG))
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := decodeAPNG(t.Context(), result.Bytes(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertPixel(t, sequence.frames[0], 0, 0, yellow)
	assertPixel(t, sequence.frames[0], 1, 1, blue)
	assertPixel(t, sequence.frames[1], 1, 1, yellow)
	assertPixel(t, sequence.frames[1], 2, 2, blue)
}

func TestGIFAnimationRetainsSmallPaletteColors(t *testing.T) {
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	input := apngFixture(t, 2, 2, 1, nil, []apngTestFrame{{img: solidImage(2, 2, white), delay: frameDelay{1, 100}}, {img: solidImage(2, 2, color.NRGBA{R: 17, G: 43, B: 81, A: 255}), delay: frameDelay{1, 100}}})
	result, err := testEngine(t, DefaultConfig()).ProcessBytes(t.Context(), input, NewPlan().Frames(PreserveAnimation).Format(GIF))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := gif.DecodeAll(result.Reader())
	if err != nil {
		t.Fatal(err)
	}
	assertPixel(t, decoded.Image[0], 0, 0, white)
	assertPixel(t, decoded.Image[1], 0, 0, color.NRGBA{R: 17, G: 43, B: 81, A: 255})
}
