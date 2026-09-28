package upload

import (
	"context"
	"testing"
)

// Compare allocations across file sizes without allocating the source payload.
// This includes request ownership and cleanup, not just the copying loop.
func BenchmarkCaptureStreaming(b *testing.B) {
	for _, test := range []struct {
		name string
		size int
	}{{"32KiB", 32 << 10}, {"8MiB", 8 << 20}} {
		b.Run(test.name, func(b *testing.B) {
			config := Config{TempDirectory: b.TempDir(), MaxBytes: int64(test.size), MaxFileBytes: int64(test.size), MaxFiles: 1, MaxReaders: 1}
			b.ReportAllocs()
			b.SetBytes(int64(test.size))
			for b.Loop() {
				batch, err := New(context.Background(), config)
				if err != nil {
					b.Fatal(err)
				}
				file, err := batch.Capture(context.Background(), &boundedSource{remaining: test.size}, "stream.bin", "application/octet-stream")
				closeErr := batch.Close()
				if err != nil || closeErr != nil || file.Size() != int64(test.size) {
					b.Fatalf("capture=%v cleanup=%v size=%d", err, closeErr, file.Size())
				}
			}
		})
	}
}
