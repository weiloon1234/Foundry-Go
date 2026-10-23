package ormcore

import (
	"reflect"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ResultCodec reuses scalar codecs and supplies concrete nullable built-ins.
// Named nullable/custom values use the explicit-codec fragment constructor.
func ResultCodec[V any]() codec.Codec[V] {
	if c, ok := resultCodec(reflect.TypeFor[V]()).(codec.Codec[V]); ok {
		return c
	}
	return ScalarCodec[V]()
}
func resultCodec(t reflect.Type) any {
	switch t {
	case reflect.TypeFor[[]byte]():
		return codec.Bytes[[]byte]()
	case reflect.TypeFor[temporal.Interval]():
		return codec.Interval()
	case reflect.TypeFor[value.Nullable[bool]]():
		return codec.Nullable(codec.Bool[bool]())
	case reflect.TypeFor[value.Nullable[string]]():
		return codec.Nullable(codec.String[string]())
	case reflect.TypeFor[value.Nullable[int]]():
		return codec.Nullable(codec.Signed[int]())
	case reflect.TypeFor[value.Nullable[int8]]():
		return codec.Nullable(codec.Signed[int8]())
	case reflect.TypeFor[value.Nullable[int16]]():
		return codec.Nullable(codec.Signed[int16]())
	case reflect.TypeFor[value.Nullable[int32]]():
		return codec.Nullable(codec.Signed[int32]())
	case reflect.TypeFor[value.Nullable[int64]]():
		return codec.Nullable(codec.Signed[int64]())
	case reflect.TypeFor[value.Nullable[uint]]():
		return codec.Nullable(codec.Unsigned[uint]())
	case reflect.TypeFor[value.Nullable[uint8]]():
		return codec.Nullable(codec.Unsigned[uint8]())
	case reflect.TypeFor[value.Nullable[uint16]]():
		return codec.Nullable(codec.Unsigned[uint16]())
	case reflect.TypeFor[value.Nullable[uint32]]():
		return codec.Nullable(codec.Unsigned[uint32]())
	case reflect.TypeFor[value.Nullable[uint64]]():
		return codec.Nullable(codec.Unsigned[uint64]())
	case reflect.TypeFor[value.Nullable[float32]]():
		return codec.Nullable(codec.Float[float32]())
	case reflect.TypeFor[value.Nullable[float64]]():
		return codec.Nullable(codec.Float[float64]())
	case reflect.TypeFor[value.Nullable[[]byte]]():
		return codec.Nullable(codec.Bytes[[]byte]())
	case reflect.TypeFor[value.Nullable[decimal.Decimal]]():
		return codec.Nullable(codec.Decimal())
	case reflect.TypeFor[value.Nullable[time.Time]]():
		return codec.Nullable(codec.Time())
	case reflect.TypeFor[value.Nullable[temporal.DateTime]]():
		return codec.Nullable(codec.DateTime())
	case reflect.TypeFor[value.Nullable[temporal.Date]]():
		return codec.Nullable(codec.Date())
	case reflect.TypeFor[value.Nullable[temporal.Time]]():
		return codec.Nullable(codec.WallTime())
	case reflect.TypeFor[value.Nullable[temporal.LocalDateTime]]():
		return codec.Nullable(codec.LocalDateTime())
	case reflect.TypeFor[value.Nullable[temporal.Interval]]():
		return codec.Nullable(codec.Interval())
	}
	return nil
}
