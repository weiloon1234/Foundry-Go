package ratewindow

import (
	"math/big"
	"testing"
)

func FuzzExactWindowBoundary(f *testing.F) {
	for _, pair := range [][2]int64{{0, 1}, {1250, 1000}, {1999, 1000}, {2000, 1000}, {MaxTimestamp - 86400000, 86400000}, {MaxTimestamp, 1}, {-1, 1}, {1, 0}} {
		f.Add(pair[0], pair[1])
	}
	f.Fuzz(func(t *testing.T, now, window int64) {
		end, err := End(now, window)
		valid := window > 0 && window <= MaxTimestamp && now >= 0 && now <= MaxTimestamp-window
		if !valid {
			if err == nil {
				t.Fatal(now, window, end)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		// Arbitrary-precision quotient is independent of bounded timestamp arithmetic.
		n, w := big.NewInt(now), big.NewInt(window)
		want := new(big.Int).Quo(n, w)
		want.Add(want, big.NewInt(1))
		want.Mul(want, w)
		if !want.IsInt64() || end != want.Int64() || end <= now {
			t.Fatal(now, window, end, want)
		}
	})
}
