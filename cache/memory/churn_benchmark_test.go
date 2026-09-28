package memory_test

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"testing"
	"time"
)

// Rotate capacity+1 precomputed keys so every measured Put must evict. A fixed
// clock isolates the cost of capacity pressure without incidental TTL expiry.
func BenchmarkMemoryCacheChurn(b *testing.B) {
	for _, capacity := range []int{100, 10000} {
		for _, finite := range []bool{false, true} {
			b.Run(fmt.Sprintf("entries=%d/finite=%t", capacity, finite), func(b *testing.B) {
				source := testkit.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
				backend, err := memory.New(memory.Config{MaxEntries: capacity, MaxBytes: 64 << 20}, source)
				if err != nil {
					b.Fatal(err)
				}
				defer backend.Close(context.Background())
				keys := make([]cache.EntryKey, capacity+1)
				ttl := cache.Forever()
				if finite {
					ttl = cache.For(time.Hour)
				}
				data := []byte("bounded-cache-value")
				for i := range keys {
					keys[i], err = cache.NewEntryKey(cache.Namespace{Application: "benchmark", Environment: "test"}, "churn", fmt.Sprint(i))
					if err != nil {
						b.Fatal(err)
					}
					if i < capacity {
						if err := backend.Put(b.Context(), keys[i], data, ttl); err != nil {
							b.Fatal(err)
						}
					}
				}
				index := capacity
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if err := backend.Put(b.Context(), keys[index], data, ttl); err != nil {
						b.Fatal(err)
					}
					index = (index + 1) % len(keys)
				}
			})
		}
	}
}
