package ratewindow

import (
	"crypto/sha256"
	"math/big"
	"testing"
)

func FuzzExactWindowBoundary(f *testing.F) {
	for _, pair := range [][3]int64{{0, 1, 0}, {1250, 1000, 0}, {1999, 1000, 0}, {2000, 1000, 0}, {1250, 1000, 300}, {100, 1000, 999}, {MaxTimestamp - 86400000, 86400000, 5}, {MaxTimestamp, 1, 0}, {-1, 1, 0}, {1, 0, 0}, {5, 10, 10}, {5, 10, -1}} {
		f.Add(pair[0], pair[1], pair[2])
	}
	f.Fuzz(func(t *testing.T, now, window, offset int64) {
		end, err := End(now, window, offset)
		valid := window > 0 && window <= MaxTimestamp && offset >= 0 && offset < window && now >= 0 && now <= MaxTimestamp-window
		if !valid {
			if err == nil {
				t.Fatal(now, window, offset, end)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		// Arbitrary-precision floor division is independent of bounded arithmetic:
		// end = offset + (floor((now-offset)/window)+1)*window.
		n, w, o := big.NewInt(now), big.NewInt(window), big.NewInt(offset)
		shifted := new(big.Int).Sub(n, o)
		// Euclidean division equals floor division for a positive window.
		quotient, _ := new(big.Int).DivMod(shifted, w, new(big.Int))
		want := new(big.Int).Add(quotient, big.NewInt(1))
		want.Mul(want, w)
		want.Add(want, o)
		if !want.IsInt64() || end != want.Int64() || end <= now || end-now > window {
			t.Fatal(now, window, offset, end, want)
		}
	})
}

func TestSeedAndOffsetAreDeterministicAndSpread(t *testing.T) {
	window := int64(60000)
	seen := make(map[int64]bool)
	for _, text := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		digest := sha256.Sum256([]byte(text))
		seed := Seed(digest)
		if seed < 0 || seed >= 1<<48 || seed != Seed(digest) {
			t.Fatal("unbounded or unstable seed", seed)
		}
		offset := Offset(seed, window)
		if offset < 0 || offset >= window {
			t.Fatal(offset)
		}
		seen[offset] = true
	}
	if len(seen) < 6 {
		t.Fatal("offsets did not spread across keys", seen)
	}
	if Offset(123, 0) != 0 || Offset(-1, 10) != 0 || Offset(15, 10) != 5 {
		t.Fatal("offset bounds")
	}
}
