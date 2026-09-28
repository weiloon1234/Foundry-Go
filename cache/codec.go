package cache

import (
	"bytes"
	"encoding"
	"encoding/json"
	"io"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// KeyCodec preserves the declared key type using the shared keyspace codec.
type KeyCodec[K any] = keyspace.Codec[K]

func NewKeyCodec[K any](encode func(K) (string, error)) KeyCodec[K] { return keyspace.NewCodec(encode) }
func StringKeys[K ~string]() KeyCodec[K]                            { return keyspace.StringKeys[K]() }
func SignedKeys[K ~int | ~int8 | ~int16 | ~int32 | ~int64]() KeyCodec[K] {
	return keyspace.SignedKeys[K]()
}
func UnsignedKeys[K ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr]() KeyCodec[K] {
	return keyspace.UnsignedKeys[K]()
}
func TextKeys[K encoding.TextMarshaler]() KeyCodec[K] { return keyspace.TextKeys[K]() }

// Codec preserves V through serialization. Callbacks must be concurrency-safe,
// leave inputs unchanged and return owned results. The Cache copies buffers at
// adapter boundaries and catches codec panic/Goexit without publishing a value.
// Size limits bound serialized storage, not arbitrary allocations inside a codec.
type Codec[V any] struct {
	encode func(V) ([]byte, error)
	decode func([]byte) (V, error)
}

func NewCodec[V any](encode func(V) ([]byte, error), decode func([]byte) (V, error)) Codec[V] {
	return Codec[V]{encode: encode, decode: decode}
}
func (c Codec[V]) Validate() error {
	if c.encode == nil || c.decode == nil {
		return fault.New(fault.Invalid, "cache value codec is not initialized")
	}
	return nil
}

// JSON uses Go's JSON encoding rules without declaring a public HTTP DTO schema.
// It preserves dynamic JSON numbers as json.Number and rejects trailing documents.
// Change the declaration name/version when changing an incompatible stored format.
func JSON[V any]() Codec[V] {
	return NewCodec(func(value V) ([]byte, error) { return json.Marshal(value) }, func(data []byte) (V, error) {
		var value V
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return *new(V), err
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return *new(V), fault.New(fault.Invalid, "cache JSON contains trailing input")
		}
		return value, nil
	})
}
