package codec

import "database/sql/driver"

// ParameterType identifies the SQL representation of a standalone bound value.
// It does not declare a column schema. The zero value is unspecified; custom
// codecs must opt in before they can create typed SQL parameter expressions.
type ParameterType uint8

const (
	TypeBoolean ParameterType = iota + 1
	TypeInteger
	TypeFloat
	TypeText
	TypeUUID
	TypeDecimal
	TypeDate
	TypeTime
	TypeLocalDateTime
	TypeDateTime
	TypeBytes
	TypeInterval
	TypeJSON
)

// ParameterType reports the codec's standalone SQL representation. Nullable and
// validated adapters preserve it; binding and decoding retain their own checks.
func (c Codec[T]) ParameterType() ParameterType { return c.parameterType }

// WithParameterType declares the SQL representation used by a custom codec.
// The caller owns compatibility between this representation and both callbacks.
// Invalid/unspecified kinds are rejected when compiling parameter expressions.
func (c Codec[T]) WithParameterType(kind ParameterType) Codec[T] {
	c.parameterType = kind
	return c
}

func typed[T any](kind ParameterType, encode func(T) (driver.Value, error), decode func(any) (T, error)) Codec[T] {
	return New(encode, decode).WithParameterType(kind)
}
