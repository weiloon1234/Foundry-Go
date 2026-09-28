// Package value distinguishes absent fields, explicit nulls, and present values.
// Optional[Nullable[T]] represents all three states without losing T's type.
package value

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Optional represents an omitted field or a present T, including T's zero value.
// Its zero value is omitted. Use json:",omitzero" on enclosing DTO fields; an
// omitted Optional cannot serialize as a standalone JSON value.
type Optional[T any] struct {
	value T
	set   bool
}

func Set[T any](value T) Optional[T] { return Optional[T]{value: value, set: true} }
func (v Optional[T]) IsSet() bool    { return v.set }
func (v Optional[T]) IsZero() bool   { return !v.set }
func (v Optional[T]) Get() (T, bool) { return v.value, v.set }
func (v Optional[T]) MarshalJSON() ([]byte, error) {
	if !v.set {
		return nil, fault.New(fault.Missing, "omitted field has no JSON value; use omitzero")
	}
	return marshalPresent(v.value)
}
func (v *Optional[T]) UnmarshalJSON(data []byte) error {
	var decoded T
	if isNull(data) && !isNullable(decoded) && !isJSONDocument(decoded) {
		return fault.New(fault.Invalid, "non-nullable field cannot decode null")
	}
	if err := unmarshalPresent(data, &decoded); err != nil {
		return err
	}
	*v = Set(decoded)
	return nil
}

// Nullable represents database/transport null or a present T. It does not
// represent omission. The zero value is null; Of includes T's zero value.
type Nullable[T any] struct {
	value T
	valid bool
}

func Of[T any](value T) Nullable[T]  { return Nullable[T]{value: value, valid: true} }
func Null[T any]() Nullable[T]       { return Nullable[T]{} }
func (v Nullable[T]) IsNull() bool   { return !v.valid }
func (v Nullable[T]) Get() (T, bool) { return v.value, v.valid }
func (v Nullable[T]) nullableValue() {}
func (v Nullable[T]) MarshalJSON() ([]byte, error) {
	if !v.valid {
		return []byte("null"), nil
	}
	return marshalPresent(v.value)
}
func (v *Nullable[T]) UnmarshalJSON(data []byte) error {
	if isNull(data) {
		*v = Null[T]()
		return nil
	}
	var decoded T
	if err := unmarshalPresent(data, &decoded); err != nil {
		return err
	}
	*v = Of(decoded)
	return nil
}

func isNull(data []byte) bool    { return bytes.Equal(bytes.TrimSpace(data), []byte("null")) }
func isNullable[T any](v T) bool { _, ok := any(v).(interface{ nullableValue() }); return ok }

// IsNullableType reports whether T carries Foundry's nullable-value contract.
// It inspects type capability without invoking methods or reading a value.
func IsNullableType[T any]() bool { return isNullable(*new(T)) }

// IsOptionalType reports whether T carries Foundry's optional-value contract.
// It checks type capability without invoking methods or reading a value.
func IsOptionalType[T any]() bool {
	_, ok := any(*new(T)).(jsonOptional)
	return ok
}

func marshalPresent[T any](v T) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if isNull(data) && !isNullable(v) && !isJSONDocument(v) {
		return nil, fault.New(fault.Invalid, "present value encoded null; use Nullable explicitly")
	}
	return data, nil
}

// Use the same exact-number representation as typed snapshots and transport
// descriptors, including when wrappers nest and the outer decoder's options
// cannot pass through a custom UnmarshalJSON method.
func unmarshalPresent(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fault.New(fault.Invalid, "invalid JSON value boundary")
	}
	return nil
}
