package capture

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/httpclient"
)

// BenchmarkClientCapture measures the caller-visible cost of capture on one
// Do with a JSON request body over an in-memory transport, so the network
// does not hide it. The recorder is drained every iteration as the writer
// would, and rebuilt before its rate tokens run out so no iteration takes the
// cheaper refused path.
func BenchmarkClientCapture(b *testing.B) {
	payload := []byte(`{"amount":1200,"currency":"myr","description":"order 42"}`)
	for _, mode := range []string{"none", "excluded", "captured_json", "captured_html"} {
		b.Run(mode, func(b *testing.B) {
			body, contentType := []byte(`{"id":"ch_1","amount":1200,"currency":"myr","status":"succeeded","token":"tok_1"}`), "application/json"
			if mode == "captured_html" {
				body, contentType = []byte("<html><body>"+strings.Repeat("receipt ", 64)+"</body></html>"), "text/html"
			}
			transport := roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Body != nil {
					_, _ = io.Copy(io.Discard, r.Body)
				}
				return &http.Response{StatusCode: 200, Proto: "HTTP/1.1", Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: r}, nil
			})
			client := testClientConfig("https://api.test/v1")
			config := DefaultConfig()
			config.Rate, config.Burst = 10_000, 10_000
			if mode == "excluded" {
				config.Exclude = []Exclusion{{Host: "api.test"}}
			}
			var h harness
			build := func() {
				if mode == "none" {
					c, err := httpclient.New(client, transport)
					if err != nil {
						b.Fatal(err)
					}
					b.Cleanup(func() { _ = c.Close(context.Background()) })
					h = harness{client: c}
					return
				}
				h = newHarness(b, config, client, transport)
			}
			build()
			captured := strings.HasPrefix(mode, "captured")
			cohort := 0
			b.ReportAllocs()
			for b.Loop() {
				request := h.client.Post("charges").Header("Content-Type", "application/json").WithBody(httpclient.Bytes(payload))
				response, err := h.client.Do(b.Context(), request)
				if err != nil || response.Status() != 200 {
					b.Fatal("call failed", err)
				}
				if h.recorder == nil {
					continue
				}
				record := h.recorder.Recorder().Drain()
				if captured != (record != nil) {
					b.Fatal("capture admission changed during the benchmark")
				}
				if record != nil {
					record.Release()
				}
				if cohort++; cohort == config.Burst {
					b.StopTimer()
					build()
					cohort = 0
					b.StartTimer()
				}
			}
		})
	}
}
