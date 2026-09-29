package http

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

// BenchmarkCompressionEncoderReuse compares a fresh encoder per response with
// the middleware's per-encoder pool for a typical 16 KiB text response.
func BenchmarkCompressionEncoderReuse(b *testing.B) {
	body := []byte(strings.Repeat(`{"id":12345,"name":"compressible item","tags":["a","b"]},`, 280))
	for _, encoder := range []CompressionEncoder{GzipCompression(GzipFastest), BrotliCompression(4, 20)} {
		for _, pooled := range []bool{false, true} {
			name := string(encoder.Encoding()) + "/fresh"
			var pools *compressionPools
			if pooled {
				name = string(encoder.Encoding()) + "/pooled"
				pools = newCompressionPools([]CompressionEncoder{encoder})
			}
			b.Run(name, func(b *testing.B) {
				var sink bytes.Buffer
				b.ReportAllocs()
				b.SetBytes(int64(len(body)))
				for b.Loop() {
					sink.Reset()
					stream, release, err := pools.acquire(encoder, &sink)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := stream.Write(body); err != nil {
						b.Fatal(err)
					}
					if err := stream.Close(); err != nil {
						b.Fatal(err)
					}
					release()
				}
			})
		}
	}
}

func TestPooledCompressionStreamsAreResetAndDetached(t *testing.T) {
	for _, encoder := range []CompressionEncoder{GzipCompression(GzipFastest), BrotliCompression(4, 20)} {
		pools := newCompressionPools([]CompressionEncoder{encoder})
		var previous *bytes.Buffer
		var previousLength int
		for _, text := range []string{"first response body", "second response body"} {
			var sink bytes.Buffer
			stream, release, err := pools.acquire(encoder, &sink)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(stream, text); err != nil {
				t.Fatal(err)
			}
			if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
			release()
			var reader io.Reader
			if encoder.Encoding() == GzipEncoding {
				gz, err := gzip.NewReader(bytes.NewReader(sink.Bytes()))
				if err != nil {
					t.Fatal(err)
				}
				reader = gz
			} else {
				reader = brotli.NewReader(bytes.NewReader(sink.Bytes()))
			}
			decoded, err := io.ReadAll(reader)
			if err != nil || string(decoded) != text {
				t.Fatalf("%s pooled stream leaked state: %q %v", encoder.Encoding(), decoded, err)
			}
			// Reuse never writes into an earlier response's writer.
			if previous != nil && previous.Len() != previousLength {
				t.Fatalf("%s reused stream wrote to a completed response", encoder.Encoding())
			}
			previous, previousLength = &sink, sink.Len()
		}
	}
}
