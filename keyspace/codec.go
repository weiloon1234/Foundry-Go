package keyspace

import (
	"encoding"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Codec encodes one concrete key type. Callbacks must be deterministic,
// concurrency-safe and return different text for distinct semantic keys.
// The consuming feature validates key size and syntax before backend access.
type Codec[K any] struct{ encode func(K) (string, error) }

func NewCodec[K any](encode func(K) (string, error)) Codec[K] {
	return Codec[K]{encode: encode}
}
func StringKeys[K ~string]() Codec[K] {
	return NewCodec(func(k K) (string, error) { return string(k), nil })
}

// SignedKeys preserves exact signed integer keys, including named natural IDs.
func SignedKeys[K ~int | ~int8 | ~int16 | ~int32 | ~int64]() Codec[K] {
	return NewCodec(func(k K) (string, error) { return strconv.FormatInt(int64(k), 10), nil })
}

// UnsignedKeys preserves the full uint64 range without a floating-point conversion.
func UnsignedKeys[K ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr]() Codec[K] {
	return NewCodec(func(k K) (string, error) { return strconv.FormatUint(uint64(k), 10), nil })
}
func TextKeys[K encoding.TextMarshaler]() Codec[K] {
	return NewCodec(func(k K) (string, error) { data, err := k.MarshalText(); return string(data), err })
}
func (c Codec[K]) Validate() error {
	if c.encode == nil {
		return fault.New(fault.Invalid, "key codec is not initialized")
	}
	return nil
}

// Encode calls the declared codec. Feature APIs isolate callback failures and
// validate the resulting address; direct adapter callers own those boundaries.
func (c Codec[K]) Encode(key K) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	return c.encode(key)
}
