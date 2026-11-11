package http

import (
	"bytes"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
)

// A kernel microbenchmark with a synchronous fake store. Admission cohorts each
// get a fresh recorder so rate refusals cannot masquerade as successful capture.
// Both sides read the same body and validate the same response; the captured
// side also verifies the raw snapshot and releases its retained-byte charge.
func BenchmarkWebhookCapture(b *testing.B) {
	for _, captured := range []bool{false, true} {
		name := "uncaptured"
		if captured {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			payload := strings.Repeat("p", 512)
			reply := []byte(strings.Repeat("r", 64))
			next := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				n, err := io.Copy(io.Discard, r.Body)
				if err != nil || n != int64(len(payload)) {
					b.Fatal("handler did not consume exact request")
				}
				_, _ = w.Write(reply)
			})
			limits := httpcapture.DefaultConfig()
			limits.BodyBytes, limits.Rate, limits.Burst = 1024, 10000, 10000
			recorder, err := httpcapture.New[httpcapture.Snapshot](limits)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { recorder.Seal() })
			owner := newHandlerLifetime()
			if captured {
				prefix, err := ParsePathPrefix("/wh")
				if err != nil {
					b.Fatal(err)
				}
				options, err := configureServer([]ServerOption{WithWebhookCapture(recorder, CapturePrefix(prefix, FullCaptureBody()))})
				if err != nil {
					b.Fatal(err)
				}
				owner.capture = options.capture
			}
			handler := owner.wrap(next, slog.New(slog.NewTextHandler(io.Discard, nil)), DefaultServerConfig())
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			cohort := 0
			for b.Loop() {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest("POST", "/wh", strings.NewReader(payload)))
				if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), reply) {
					b.Fatal("response mismatch")
				}
				if captured {
					record := recorder.Drain()
					if record == nil {
						b.Fatal("capture refused")
					}
					if string(record.Snapshot.Body.Prefix) != payload || !bytes.Equal(record.Snapshot.Completion.ResponseHead, reply) {
						b.Fatal("snapshot mismatch")
					}
					record.Release()
					cohort++
					if cohort == limits.Burst {
						for _, drops := range recorder.Drops() {
							if drops != 0 {
								b.Fatal("benchmark included capture drops")
							}
						}
						recorder.Seal()
						recorder, err = httpcapture.New[httpcapture.Snapshot](limits)
						if err != nil {
							b.Fatal(err)
						}
						owner.capture.recorder, owner.capture.lane = recorder, recorder.NewLane()
						cohort = 0
					}
				}
			}
			for _, drops := range recorder.Drops() {
				if drops != 0 {
					b.Fatal("benchmark included capture drops")
				}
			}
		})
	}
}
