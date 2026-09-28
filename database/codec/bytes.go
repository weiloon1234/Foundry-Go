package codec

import (
	"database/sql/driver"
	"slices"
)

// Bytes preserves a concrete []byte or named byte-slice type through PostgreSQL
// bytea. A non-nil empty slice is a present empty value; nil is invalid. Use
// Nullable(Bytes[T]()) for SQL NULL. Binding and decoding own their buffers.
func Bytes[T ~[]byte]() Codec[T] {
	return typed(TypeBytes, func(v T) (driver.Value, error) {
		if v == nil {
			return nil, invalid()
		}
		return []byte(v), nil
	}, func(source any) (T, error) {
		v, ok := source.([]byte)
		if !ok || v == nil {
			return nil, invalid()
		}
		return T(slices.Clone(v)), nil
	}).withClone(slices.Clone[T])
}
