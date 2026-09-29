// Package ratewindow owns the shared exact timestamp domain and per-key window
// phase for rate authorities. Go adapters and the Redis script apply the same
// arithmetic, so every authority sharing a key agrees on its window boundaries.
package ratewindow

import (
	"encoding/binary"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxTimestamp is the greatest exact integer shared by Go and Redis Lua numbers.
const MaxTimestamp int64 = 1<<53 - 1

// Seed derives a deterministic phase seed from a key's address digest. It is
// bounded to 48 bits so Lua numbers (IEEE doubles) represent it exactly.
func Seed(digest [32]byte) int64 {
	var wide [8]byte
	copy(wide[2:], digest[:6])
	return int64(binary.BigEndian.Uint64(wide[:]))
}

// Offset returns a key's window phase in [0, window). Windows then start at
// offset + n*window instead of Unix-epoch multiples, so buckets of many keys do
// not all reset at the same instant. A nonpositive window has no phase.
func Offset(seed, window int64) int64 {
	if window <= 0 || seed < 0 {
		return 0
	}
	return seed % window
}

// End returns the end of the window containing now, for windows starting at
// offset + n*window. The result is in (now, now+window].
func End(now, window, offset int64) (int64, error) {
	if window <= 0 || offset < 0 || offset >= window || now < 0 || now > MaxTimestamp-window {
		return 0, fault.New(fault.Invalid, "rate limit clock outside supported epoch range")
	}
	elapsed := (now - offset) % window
	if elapsed < 0 {
		elapsed += window
	}
	return now - elapsed + window, nil
}
