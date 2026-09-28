package local_test

import (
	"context"
	"io"
	"strconv"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

type zeroStream struct{}

func (zeroStream) Read(p []byte) (int, error) { clear(p); return len(p), nil }
func BenchmarkLocalStream(b *testing.B) {
	for _, size := range []int64{1 << 20, 16 << 20} {
		b.Run(strconv.FormatInt(size, 10), func(b *testing.B) {
			backend, err := local.Open(b.Context(), local.DefaultConfig(b.TempDir()))
			if err != nil {
				b.Fatal(err)
			}
			defer backend.Close()
			disk, err := storage.NewDisk("benchmark", backend, storage.DefaultConfig())
			if err != nil {
				b.Fatal(err)
			}
			defer disk.Close(context.Background())
			object, err := storage.ParseKey("stream")
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(size)
			b.ResetTimer()
			for b.Loop() {
				result, err := disk.Put(b.Context(), object, io.LimitReader(zeroStream{}, size), storage.PutOptions{})
				if err != nil || result.Object.Size != size {
					b.Fatal(err)
				}
			}
		})
	}
}
