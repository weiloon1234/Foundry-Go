// Package model defines model-owned foundational values without depending on a
// database driver or runtime. Generated model APIs build on these contracts.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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

// NewIDAt generates a UUIDv7 from an explicit clock reading and 74 random bits,
// following RFC 9562 section 5.7. Ordering within a millisecond, across clock
// rollback, or across hosts is not guaranteed. IDs are not authentication secrets.
func NewIDAt[M any](now time.Time) (ID[M], error) {
	value, err := newIDAt(now)
	return ID[M]{value: value}, err
}

func newIDAt(now time.Time) ([16]byte, error) {
	var value [16]byte
	// Compare time instants before UnixMilli, which can overflow for huge years.
	if now.Before(time.UnixMilli(0)) || !now.Before(time.UnixMilli(1<<48)) {
		return value, safeerror.NewFault(fault.Invalid, "UUIDv7 time is outside the 48-bit millisecond range")
	}
	rand.Read(value[:]) // crypto/rand.Read always fills the buffer or terminates.
	millis := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		value[i] = byte(millis)
		millis >>= 8
	}
	value[6] = (value[6] & 0x0f) | 0x70
	value[8] = (value[8] & 0x3f) | 0x80
	return value, nil
}

// ParseID accepts exactly the canonical 8-4-4-4-12 hexadecimal UUID shape,
// case-insensitively. Existing UUID versions and the nil UUID are preserved.
func ParseID[M any](text string) (ID[M], error) {
	value, err := parseID(text)
	return ID[M]{value: value}, err
}

func parseID(text string) ([16]byte, error) {
	var value [16]byte
	if len(text) != 36 || text[8] != '-' || text[13] != '-' || text[18] != '-' || text[23] != '-' {
		return value, safeerror.NewFault(fault.Invalid, "invalid UUID text")
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
	if _, err := hex.Decode(value[:], compact[:]); err != nil {
		return [16]byte{}, safeerror.NewFault(fault.Invalid, "invalid UUID text")
	}
	return value, nil
}

// IDFromBytes is the explicit binary decoding boundary.
func IDFromBytes[M any](bytes [16]byte) ID[M] { return ID[M]{value: bytes} }

// Bytes returns the identifier's 16 bytes by value.
func (id ID[M]) Bytes() [16]byte { return id.value }

// IsZero reports whether the identifier is all zero bytes.
func (id ID[M]) IsZero() bool { return id.value == [16]byte{} }

// String returns the identifier as a lowercase, hyphenated UUID string.
func (id ID[M]) String() string { return formatID(id.value) }

func formatID(value [16]byte) string {
	var out [36]byte
	hex.Encode(out[:8], value[:4])
	out[8] = '-'
	hex.Encode(out[9:13], value[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], value[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], value[8:10])
	out[23] = '-'
	hex.Encode(out[24:], value[10:])
	return string(out[:])
}

// MarshalText encodes the identifier's UUID string as text.
func (id ID[M]) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

// UnmarshalText parses an identifier, replacing the receiver only on success.
func (id *ID[M]) UnmarshalText(data []byte) error {
	parsed, err := ParseID[M](string(data))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// UnmarshalJSON decodes a JSON identifier, replacing the receiver only on success.
func (id *ID[M]) UnmarshalJSON(data []byte) error {
	parsed, err := textvalue.Decode(data, parseID)
	if err != nil {
		return err
	}
	*id = ID[M]{value: parsed}
	return nil
}
