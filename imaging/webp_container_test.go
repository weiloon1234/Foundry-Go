package imaging

import (
	"encoding/binary"
	"os"
	"testing"
)

func webpTestChunk(kind string, body []byte) []byte {
	chunk := make([]byte, 8+len(body)+len(body)%2)
	copy(chunk, kind)
	binary.LittleEndian.PutUint32(chunk[4:], uint32(len(body)))
	copy(chunk[8:], body)
	return chunk
}

func webpTestFile(chunks ...[]byte) []byte {
	data := append([]byte("RIFF\x00\x00\x00\x00"), []byte("WEBP")...)
	for _, chunk := range chunks {
		data = append(data, chunk...)
	}
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	return data
}

func TestWebPCanvasCannotHideLargerCodedFrame(t *testing.T) {
	// Header-only fixtures intentionally contain no valid pixel payload.
	// Inspection must find the mismatch without allocating or decoding pixels.
	lossy := []byte{0x10, 0, 0, 0x9d, 1, 0x2a, 8, 0, 4, 0}
	lossless := make([]byte, 5)
	lossless[0] = 0x2f
	binary.LittleEndian.PutUint32(lossless[1:], 7|3<<14)
	for _, frame := range []struct {
		kind string
		data []byte
	}{{"VP8 ", lossy}, {"VP8L", lossless}} {
		t.Run(frame.kind, func(t *testing.T) {
			canvas := make([]byte, 10) // Falsely declares a 1x1 canvas.
			data := webpTestFile(webpTestChunk("VP8X", canvas), webpTestChunk(frame.kind, frame.data))
			if _, err := parseWebP(data, DefaultLimits()); err == nil {
				t.Fatal("accepted conflicting coded dimensions")
			}
			limits := DefaultLimits()
			limits.Width, limits.Height, limits.Pixels = 1, 1, 1
			if _, err := Inspect(data, limits); err == nil {
				t.Fatal("outer header bypassed input admission")
			}
			canvas[4], canvas[7] = 7, 3
			valid := webpTestFile(webpTestChunk("VP8X", canvas), webpTestChunk(frame.kind, frame.data))
			info, err := Inspect(valid, DefaultLimits())
			if err != nil || info.Width != 8 || info.Height != 4 {
				t.Fatal("matching headers rejected", info, err)
			}
		})
	}
}

func TestExtendedWebPStillRoundTrips(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	encoded, err := e.ProcessBytes(t.Context(), pngInput(t, 8, 4), NewPlan().Format(WebP))
	if err != nil {
		t.Fatal(err)
	}
	canvas := make([]byte, 10)
	canvas[4], canvas[7] = 7, 3
	data := webpTestFile(webpTestChunk("VP8X", canvas), encoded.Bytes()[12:])
	result, err := e.ProcessBytes(t.Context(), data, NewPlan().Format(PNG))
	if err != nil || result.Info().Width != 8 || result.Info().Height != 4 {
		t.Fatal("extended WebP round trip", err)
	}
}

func FuzzWebPHeaders(f *testing.F) {
	canvas := make([]byte, 10)
	canvas[4], canvas[7] = 7, 3
	lossless := make([]byte, 5)
	lossless[0] = 0x2f
	binary.LittleEndian.PutUint32(lossless[1:], 7|3<<14)
	f.Add(webpTestFile(webpTestChunk("VP8X", canvas), webpTestChunk("VP8L", lossless)))
	f.Add(webpTestFile(webpTestChunk("VP8X", canvas), webpTestChunk("VP8 ", []byte{0x10, 0, 0, 0x9d, 1, 0x2a, 8, 0, 4, 0})))
	for _, name := range []string{"lossy", "lossless"} {
		data, err := os.ReadFile("testdata/webp/animation-" + name + ".webp")
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}
		// Bypass Inspect's panic containment to exercise raw container/header
		// parsing directly. This performs no pixel decoding.
		container, err := parseWebP(data, DefaultLimits())
		if err == nil && (len(container.frames) < 1 || len(container.frames) > DefaultLimits().Frames || container.orientation > 8) {
			t.Fatal("invalid WebP metadata")
		}
	})
}
