package cacheint_test

import (
	"math"
	"math/big"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/cacheint"
)

// An arbitrary-precision oracle exercises signed arithmetic independently of the
// implementation's bounds checks. Successful values must also round-trip exactly.
func FuzzIntegerAddition(f *testing.F) {
	for _, pair := range [][2]int64{{0, 0}, {1, -1}, {math.MaxInt64, 1}, {math.MinInt64, -1}, {0, math.MinInt64}, {-1, math.MinInt64}, {math.MaxInt64, math.MinInt64}, {9007199254740993, 1}} {
		f.Add(pair[0], pair[1])
	}
	f.Fuzz(func(t *testing.T, value, delta int64) {
		exact := new(big.Int).Add(big.NewInt(value), big.NewInt(delta))
		got, err := cacheint.Add(value, delta)
		if !exact.IsInt64() {
			if err == nil {
				t.Fatal("overflow accepted", value, delta, got)
			}
			return
		}
		if err != nil || got != exact.Int64() {
			t.Fatal("integer arithmetic mismatch", value, delta, got, err)
		}
		encoded := cacheint.Encode(got)
		decoded, err := cacheint.Decode(encoded)
		if len(encoded) > cacheint.MaxBytes || err != nil || decoded != got {
			t.Fatal("integer representation mismatch", got, decoded, err)
		}
	})
}
