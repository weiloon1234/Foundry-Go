package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

func BenchmarkStreamResponseBoundedCopy(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int64
	}{{"32KiB", 32 << 10}, {"8MiB", 8 << 20}} {
		b.Run(tc.name, func(b *testing.B) {
			var closes int
			router, err := NewRouter(streamEndpoint().Handle(func(context.Context, streamRequest) (Stream, error) {
				return StreamFrom(func(context.Context) (StreamContent, error) {
					source := &downloadSizedZeroReader{size: tc.size}
					return StreamContent{Body: fileReaderCallbacks{read: source.Read, close: func() error { closes++; return nil }}, Length: value.Set(tc.size), MediaType: "text/plain; charset=utf-8"}, nil
				}), nil
			}))
			if err != nil {
				b.Fatal(err)
			}
			request := httptest.NewRequest("GET", "/stream", nil)
			b.ReportAllocs()
			b.SetBytes(tc.size)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				writer := &downloadCountingWriter{header: make(stdhttp.Header)}
				router.ServeHTTP(writer, request)
				if writer.bytes != tc.size || writer.largest > 32<<10 {
					b.Fatal("unbounded copy", writer.bytes, writer.largest)
				}
			}
			b.StopTimer()
			if closes != b.N {
				b.Fatal("body leaked", closes)
			}
		})
	}
}
