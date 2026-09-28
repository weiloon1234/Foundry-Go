// Package model defines model-owned foundational values without depending on a
// database driver or runtime. Generated model APIs build on these contracts.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/textvalue"
)

// ID is a comparable UUID whose owner is checked by the Go compiler. The zero
// value is the nil UUID, not a newly generated identity. M need not be comparable.
// Bytes/ParseID are explicit serialization boundaries that discard/reapply owner
// information; they do not establish record existence or authorization.
type ID[M any] struct {
	_     [0]*M
	value [16]byte
}

// NewID generates a UUIDv7 using the current system time.
func NewID[M any]() (ID[M], error) { return NewIDAt[M](time.Now()) }

// NewIDAt generates a UUIDv7 from an explicit clock reading and 74 random bits,
// following RFC 9562 section 5.7. Ordering within a millisecond, across clock
// rollback, or across hosts is not guaranteed. IDs are not authentication secrets.
func NewIDAt[M any](now time.Time) (ID[M], error) {
	var id ID[M]
	// Compare time instants before UnixMilli, which can overflow for huge years.
	if now.Before(time.UnixMilli(0)) || !now.Before(time.UnixMilli(1<<48)) {
		return id, fault.New(fault.Invalid, "UUIDv7 time is outside the 48-bit millisecond range")
	}
	rand.Read(id.value[:]) // crypto/rand.Read always fills the buffer or terminates.
	millis := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		id.value[i] = byte(millis)
		millis >>= 8
	}
	id.value[6] = (id.value[6] & 0x0f) | 0x70
	id.value[8] = (id.value[8] & 0x3f) | 0x80
	return id, nil
}

// ParseID accepts exactly the canonical 8-4-4-4-12 hexadecimal UUID shape,
// case-insensitively. Existing UUID versions and the nil UUID are preserved.
func ParseID[M any](text string) (ID[M], error) {
	var id ID[M]
	if len(text) != 36 || text[8] != '-' || text[13] != '-' || text[18] != '-' || text[23] != '-' {
		return id, fault.New(fault.Invalid, "invalid UUID text")
	}
	var compact [32]byte
	j := 0
	for i := range len(text) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		compact[j] = text[i]
		j++
	}
	if _, err := hex.Decode(id.value[:], compact[:]); err != nil {
		return ID[M]{}, fault.New(fault.Invalid, "invalid UUID text")
	}
	return id, nil
}

// IDFromBytes is the explicit binary decoding boundary.
func IDFromBytes[M any](bytes [16]byte) ID[M] { return ID[M]{value: bytes} }
func (id ID[M]) Bytes() [16]byte              { return id.value }
func (id ID[M]) IsZero() bool                 { return id.value == [16]byte{} }
func (id ID[M]) String() string {
	var out [36]byte
	hex.Encode(out[:8], id.value[:4])
	out[8] = '-'
	hex.Encode(out[9:13], id.value[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], id.value[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], id.value[8:10])
	out[23] = '-'
	hex.Encode(out[24:], id.value[10:])
	return string(out[:])
}
func (id ID[M]) MarshalText() ([]byte, error) { return []byte(id.String()), nil }
func (id *ID[M]) UnmarshalText(data []byte) error {
	parsed, err := ParseID[M](string(data))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
func (id *ID[M]) UnmarshalJSON(data []byte) error {
	parsed, err := textvalue.Decode(data, ParseID[M])
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
