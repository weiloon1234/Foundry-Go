package http

import (
	"bytes"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

type etagDiscardWriter struct {
	header         stdhttp.Header
	bytes, largest int
}

func (w *etagDiscardWriter) Header() stdhttp.Header { return w.header }
func (w *etagDiscardWriter) WriteHeader(int)        {}
func (w *etagDiscardWriter) Write(data []byte) (int, error) {
	w.bytes += len(data)
	w.largest = max(w.largest, len(data))
	return len(data), nil
}

func BenchmarkETagBoundedCapture(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{{"32KiB", 32 << 10}, {"8MiB", 8 << 20}} {
		b.Run(tc.name, func(b *testing.B) {
			payload := bytes.Repeat([]byte("x"), tc.size)
			config := DefaultETagConfig()
			config.MaxBytes = 64 << 10
			handler, err := ApplyMiddleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				for offset := 0; offset < len(payload); {
					end := min(offset+32<<10, len(payload))
					if _, err := w.Write(payload[offset:end]); err != nil {
						b.Fatal(err)
					}
					offset = end
				}
			}), ETags(config))
			if err != nil {
				b.Fatal(err)
			}
			request := httptest.NewRequest("GET", "/", nil)
			b.SetBytes(int64(tc.size))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				writer := &etagDiscardWriter{header: make(stdhttp.Header)}
				handler.ServeHTTP(writer, request)
				if writer.bytes != tc.size || writer.largest > 32<<10 {
					b.Fatal("transfer or write bound changed")
				}
				if (writer.header.Get("ETag") != "") != (tc.size <= int(config.MaxBytes)) {
					b.Fatal("capture budget changed")
				}
			}
		})
	}
}
