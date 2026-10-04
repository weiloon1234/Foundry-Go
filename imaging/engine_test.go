package imaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/gen2brain/gav1d/avif"
)

func testEngine(t *testing.T, config Config) *Engine {
	t.Helper()
	e, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return e
}
func pngInput(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 29), G: uint8(y * 31), B: 90, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestFormatsAndTransforms(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	input := pngInput(t, 8, 4)
	for _, f := range []Format{JPEG, PNG, GIF, BMP, TIFF, WebP, ICO, AVIF} {
		t.Run(string(f), func(t *testing.T) {
			result, err := e.ProcessBytes(t.Context(), input, NewPlan().Resize(4, 3).Format(f))
			if err != nil {
				t.Fatal(err)
			}
			if result.Info().Format != f || result.Info().Width != 4 || result.Info().Height != 3 || result.Size() == 0 {
				t.Fatal("incorrect output metadata")
			}
			if f == AVIF {
				// Verify the encoded bitstream independently of the public pipeline.
				img, err := avif.Decode(result.Reader(), avif.Options{FrameSizeLimit: 12})
				if err != nil || img.Bounds().Dx() != 4 || img.Bounds().Dy() != 3 {
					t.Fatal("AVIF output does not decode", err)
				}
				if _, err := e.ProcessBytes(t.Context(), result.Bytes(), NewPlan()); err != nil {
					t.Fatal("AVIF input cannot round trip", err)
				}
			} else {
				info, err := Inspect(result.Bytes(), DefaultLimits())
				if err != nil || info.Width != 4 || info.Height != 3 || info.Format != f {
					t.Fatal("output inspection", info, err)
				}
				if _, err := e.Process(t.Context(), result.Reader(), NewPlan().Format(PNG)); err != nil {
					t.Fatal("output cannot round trip", err)
				}
			}
			copy := result.Bytes()
			copy[0] ^= 0xff
			if bytes.Equal(copy, result.Bytes()) {
				t.Fatal("output exposes mutable storage")
			}
			if _, err := json.Marshal(result); err == nil {
				t.Fatal("implicit image serialization")
			}
		})
	}
	for _, item := range []struct {
		name string
		plan Plan
		w, h int
	}{
		{"fit", NewPlan().Fit(3, 3, false), 3, 2},
		{"fit no upscale", NewPlan().Fit(20, 20, false), 8, 4},
		{"fit upscale", NewPlan().Fit(20, 20, true), 20, 10},
		{"fill", NewPlan().Fill(3, 3, false), 3, 3},
		{"crop", NewPlan().Crop(2, 1, 3, 2), 3, 2},
		{"rotate", NewPlan().Rotate(Rotate90), 4, 8},
		{"rotate180", NewPlan().Rotate(Rotate180), 8, 4},
		{"rotate270", NewPlan().Rotate(Rotate270), 4, 8},
		{"effects", NewPlan().FlipHorizontal().FlipVertical().Grayscale().Blur(1).Brightness(20).Contrast(10), 8, 4},
	} {
		t.Run(item.name, func(t *testing.T) {
			r, err := e.ProcessBytes(t.Context(), input, item.plan)
			if err != nil || r.Info().Width != item.w || r.Info().Height != item.h {
				t.Fatal("transform dimensions", r.Info(), err)
			}
		})
	}
}

func TestPlanValidationAndAllocationPreflight(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	input := pngInput(t, 4, 2)
	for _, p := range []Plan{
		NewPlan().Format(WebP).JPEGQuality(80), NewPlan().JPEGQuality(101), NewPlan().Format("unknown"),
		NewPlan().Resize(0, 1), NewPlan().Crop(4, 0, 1, 1), NewPlan().Fill(100, 100, false), NewPlan().Rotate(45),
		NewPlan().Blur(math.NaN()), NewPlan().Blur(math.Inf(1)), NewPlan().Blur(101), NewPlan().Contrast(-101),
		NewPlan().Orientation(Orientation(9)), NewPlan().Frames(Frames(9)), NewPlan().Metadata(Metadata(255)),
		NewPlan().Resize(257, 1).Format(ICO),
	} {
		if result, err := e.ProcessBytes(t.Context(), input, p); err == nil || result.Size() != 0 {
			t.Fatal("invalid plan accepted", p)
		}
	}
	p := NewPlan()
	for range MaxTransforms + 1 {
		p = p.Grayscale()
	}
	if p.Validate() == nil {
		t.Fatal("unbounded transforms")
	}
	base := NewPlan().Resize(2, 2)
	extended := base.Rotate(Rotate90)
	if len(base.steps) != 1 || len(extended.steps) != 2 {
		t.Fatal("plan mutation")
	}
	// A very wide output and very tall input can have small final areas but
	// need an enormous horizontal resampling intermediate. Reject before decode.
	config := DefaultConfig()
	config.Limits.Pixels = 10000
	small := testEngine(t, config)
	if _, err := small.ProcessBytes(t.Context(), pngInput(t, 1, 1000), NewPlan().Resize(1000, 1)); err == nil {
		t.Fatal("resampling intermediate not bounded")
	}
	config = DefaultConfig()
	config.Limits.OutputBytes = 8
	if _, err := testEngine(t, config).ProcessBytes(t.Context(), input, NewPlan()); err == nil {
		t.Fatal("output byte limit ignored")
	}
	config = DefaultConfig()
	config.Limits.InputBytes = int64(len(input) - 1)
	if _, err := testEngine(t, config).ProcessBytes(t.Context(), input, NewPlan()); err == nil {
		t.Fatal("input limit ignored")
	}
	limits := DefaultLimits()
	limits.WorkingBytes = 100
	if _, err := Inspect(input, limits); err == nil {
		t.Fatal("workspace limit ignored")
	}
	for _, limits := range []Limits{{}, {InputBytes: 1 << 31}, {InputBytes: 1, OutputBytes: 1, Width: 1, Height: 1, Pixels: 1, Frames: 0, WorkingBytes: 1000}} {
		if limits.Validate() == nil {
			t.Fatal("invalid limits accepted")
		}
	}
}

func TestOrientationAndMetadataStripping(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := range 2 {
		for x := range 3 {
			img.SetNRGBA(x, y, color.NRGBA{R: 220, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, nil); err != nil {
		t.Fatal(err)
	}
	for orientation := uint16(1); orientation <= 8; orientation++ {
		exif := make([]byte, 32)
		copy(exif, []byte("Exif\x00\x00II\x2a\x00"))
		binary.LittleEndian.PutUint32(exif[10:], 8)
		binary.LittleEndian.PutUint16(exif[14:], 1)
		binary.LittleEndian.PutUint16(exif[16:], 274)
		binary.LittleEndian.PutUint16(exif[18:], 3)
		binary.LittleEndian.PutUint32(exif[20:], 1)
		binary.LittleEndian.PutUint16(exif[24:], orientation)
		input := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(exif) + 2)}, exif...)
		input = append(input, encoded.Bytes()[2:]...)
		info, err := Inspect(input, DefaultLimits())
		if err != nil || info.Orientation != uint8(orientation) {
			t.Fatal("EXIF orientation", info, err)
		}
		e := testEngine(t, DefaultConfig())
		result, err := e.ProcessBytes(t.Context(), input, NewPlan().Format(PNG))
		if err != nil {
			t.Fatal(err)
		}
		w, h := 3, 2
		if orientation >= 5 {
			w, h = h, w
		}
		if result.Info().Width != w || result.Info().Height != h || bytes.Contains(result.Bytes(), []byte("Exif")) {
			t.Fatal("orientation or metadata not normalized")
		}
		ignored, err := e.ProcessBytes(t.Context(), input, NewPlan().Orientation(IgnoreOrientation))
		if err != nil || ignored.Info().Width != 3 || ignored.Info().Height != 2 {
			t.Fatal("ignore orientation", err)
		}
	}
}

func TestContainersFramesAndMalformedDimensions(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	palette := color.Palette{color.Black, color.White}
	frame := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	var animated bytes.Buffer
	if err := gif.EncodeAll(&animated, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{0, 1}}); err != nil {
		t.Fatal(err)
	}
	info, err := Inspect(animated.Bytes(), DefaultLimits())
	if err != nil || info.Images != 2 || !info.Animated {
		t.Fatal("GIF frames", info, err)
	}
	if _, err := e.ProcessBytes(t.Context(), animated.Bytes(), NewPlan()); err == nil {
		t.Fatal("animation silently discarded")
	}
	if _, err := e.ProcessBytes(t.Context(), animated.Bytes(), NewPlan().Frames(FirstFrame).Format(PNG)); err != nil {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	limits.Frames = 1
	if _, err := Inspect(animated.Bytes(), limits); err == nil {
		t.Fatal("frame limit ignored")
	}
	huge := pngInput(t, 1, 1)
	binary.BigEndian.PutUint32(huge[16:], 0x7fffffff)
	binary.BigEndian.PutUint32(huge[29:], crc32.ChecksumIEEE(huge[12:29]))
	if _, err := Inspect(huge, DefaultLimits()); err == nil {
		t.Fatal("huge image header accepted")
	}
	for _, input := range [][]byte{nil, []byte("not an image"), []byte("II\x2a\x00\xff\xff\xff\xff"), []byte{0, 0, 1, 0, 255, 255}, huge, animated.Bytes()[:15]} {
		if result, err := e.ProcessBytes(t.Context(), input, NewPlan()); err == nil || result.Size() != 0 {
			t.Fatal("malformed input produced output")
		}
	}
}

func TestCancellationAndReaderOwnership(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader := &countReader{data: bytes.NewReader(pngInput(t, 2, 2))}
	if _, err := e.Process(ctx, reader, NewPlan()); !errors.Is(err, context.Canceled) || reader.reads != 0 {
		t.Fatal("canceled call consumed input", err)
	}
	if _, err := e.Process(nil, reader, NewPlan()); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := e.Process(t.Context(), reader, NewPlan()); err != nil || reader.closed {
		t.Fatal("input reader ownership", err)
	}
	for _, mode := range []string{"panic", "goexit"} {
		if _, err := e.Process(t.Context(), abnormalReader(mode), NewPlan()); err == nil {
			t.Fatal("abnormal reader accepted")
		}
	}
}

type countReader struct {
	data   *bytes.Reader
	reads  int
	closed bool
}

func (r *countReader) Read(p []byte) (int, error) { r.reads++; return r.data.Read(p) }
func (r *countReader) Close() error               { r.closed = true; return nil }

type abnormalReader string

func (r abnormalReader) Read([]byte) (int, error) {
	if r == "panic" {
		panic("private payload")
	}
	runtime.Goexit()
	return 0, io.EOF
}

func TestCloseRetainsActualReaderLifetime(t *testing.T) {
	config := DefaultConfig()
	config.MaxActive = 1
	e, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	r := blockingReader{entered: make(chan struct{}), release: make(chan struct{})}
	returned := make(chan error, 1)
	go func() { _, err := e.Process(context.Background(), r, NewPlan()); returned <- err }()
	defer func() { close(r.release); <-returned; _ = e.Close(context.Background()) }()
	<-r.entered
	if _, err := e.ProcessBytes(t.Context(), pngInput(t, 1, 1), NewPlan()); err == nil {
		t.Fatal("capacity released while reader is running")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := e.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("Close failed to bound wait", err)
	}
	select {
	case <-e.Done():
		t.Fatal("Done closed before reader exit")
	default:
	}
}

type blockingReader struct{ entered, release chan struct{} }

func (r blockingReader) Read([]byte) (int, error) { close(r.entered); <-r.release; return 0, io.EOF }

type emptyReader struct{ calls int }

func (r *emptyReader) Read([]byte) (int, error) {
	r.calls++
	if r.calls > 1000 {
		return 0, io.EOF
	}
	return 0, nil
}

func TestEmptyReaderStopsWithoutWaitingForTimeout(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	r := &emptyReader{}
	result, err := e.Process(t.Context(), r, NewPlan())
	if !errors.Is(err, io.ErrNoProgress) || result.Size() != 0 || r.calls > 100 {
		t.Fatal("empty reader consumed the operation timeout", r.calls, err)
	}
	if _, err := e.ProcessBytes(t.Context(), pngInput(t, 1, 1), NewPlan()); err != nil {
		t.Fatal("reader failure did not release operation capacity", err)
	}
}
