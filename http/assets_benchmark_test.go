package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func BenchmarkAssetStreaming(b *testing.B) {
	for _, tc := range []struct {
		name  string
		bytes int
	}{{"32KiB", 32 << 10}, {"8MiB", 8 << 20}} {
		b.Run(tc.name, func(b *testing.B) {
			source := fstest.MapFS{"large.bin": {Data: make([]byte, tc.bytes)}}
			assets, err := OpenAssets(b.Context(), DefaultAssetsConfig(FilesystemAssets(source)))
			if err != nil {
				b.Fatal(err)
			}
			defer assets.Close(context.Background())
			router, err := NewRouter(assets.Mount("assets", "/assets").Register())
			if err != nil {
				b.Fatal(err)
			}
			request := httptest.NewRequest("GET", "/assets/large.bin", nil)
			b.ReportAllocs()
			b.SetBytes(int64(tc.bytes))
			for b.Loop() {
				writer := &downloadCountingWriter{header: make(stdhttp.Header)}
				router.ServeHTTP(writer, request)
				if writer.status != 200 || writer.bytes != int64(tc.bytes) || writer.largest > 32<<10 {
					b.Fatal("asset transfer bound", writer.status, writer.bytes, writer.largest)
				}
			}
		})
	}
}
