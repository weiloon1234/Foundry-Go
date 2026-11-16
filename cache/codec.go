package cache

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/weiloon1234/Foundry-Go/internal/jsoncodec"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Codec preserves V through serialization. Callbacks must be concurrency-safe,
// leave inputs unchanged and return owned results. The Cache copies buffers at
// adapter boundaries and catches codec panic/Goexit without publishing a value.
// Size limits bound serialized storage, not arbitrary allocations inside a codec.
type Codec[V any] struct {
	encode func(V) ([]byte, error)
	decode func([]byte) (V, error)
}

// NewCodec retains the supplied value encoder and decoder without validating them.
func NewCodec[V any](encode func(V) ([]byte, error), decode func([]byte) (V, error)) Codec[V] {
	return Codec[V]{encode: encode, decode: decode}
}

// Validate returns fault.Invalid if either codec callback is missing.
func (c Codec[V]) Validate() error {
	if c.encode == nil || c.decode == nil {
		return safeerror.NewFault(fault.Invalid, "cache value codec is not initialized")
	}
	return nil
}

// JSON uses Go's JSON encoding rules without declaring a public HTTP DTO schema.
// It preserves dynamic JSON numbers as json.Number and rejects trailing documents.
// Change the declaration name/version when changing an incompatible stored format.
func JSON[V any]() Codec[V] {
	return NewCodec(func(value V) ([]byte, error) { return jsoncodec.Marshal(value) }, func(data []byte) (V, error) {
		var value V
		decoder := json.NewDecoder(bytes.NewReader(data))
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return *new(V), err
		}
		if err := jsoncodec.UnmarshalExact(raw, &value); err != nil {
			return *new(V), err
		}
		if err := decoder.Decode(&raw); err != io.EOF {
			return *new(V), safeerror.NewFault(fault.Invalid, "cache JSON contains trailing input")
		}
		return value, nil
	})
}
