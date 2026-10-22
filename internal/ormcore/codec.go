package ormcore

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"reflect"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// ScalarCodec is the unrestricted ColumnOf fallback. Concrete framework codecs
// are selected non-generically; no V-by-underlying-type adaptations are emitted.
func ScalarCodec[V any]() codec.Codec[V] {
	typ := reflect.TypeFor[V]()
	if direct, ok := frameworkCodec(typ).(codec.Codec[V]); ok {
		return direct
	}
	if typ.PkgPath() == "github.com/weiloon1234/Foundry-Go/model" && strings.HasPrefix(typ.Name(), "ID[") {
		return codec.New(func(v V) (driver.Value, error) { return bindModelID(unsafe.Pointer(&v)) }, func(raw any) (v V, err error) { err = decodeModelID(raw, unsafe.Pointer(&v)); return }).WithParameterType(idRepresentation.ParameterType())
	}
	return primitiveCodec[V](typ.Kind())
}

// NumberCodec selects only numeric representations. Decimal has its exact codec.
func NumberCodec[V any]() codec.Codec[V] {
	if c, ok := any(codec.Decimal()).(codec.Codec[V]); ok {
		return c
	}
	kind := reflect.TypeFor[V]().Kind()
	if !numericKind(kind) {
		return codec.Codec[V]{}
	}
	return primitiveCodec[V](kind)
}

// TimeCodec retains the exact framework codec, with no representation adapter.
func TimeCodec[V any]() codec.Codec[V] {
	if c, ok := temporalCodec(reflect.TypeFor[V]()).(codec.Codec[V]); ok {
		return c
	}
	return codec.Codec[V]{}
}

// KeyScalarCodec accepts only the primitive kinds allowed for natural keys/enums.
func KeyScalarCodec[V any]() codec.Codec[V] {
	kind := reflect.TypeFor[V]().Kind()
	if kind != reflect.String && !integerKind(kind) {
		return codec.Codec[V]{}
	}
	return primitiveCodec[V](kind)
}

func frameworkCodec(t reflect.Type) any {
	if t == reflect.TypeFor[decimal.Decimal]() {
		return codec.Decimal()
	}
	return temporalCodec(t)
}
func temporalCodec(t reflect.Type) any {
	switch t {
	case reflect.TypeFor[time.Time]():
		return codec.Time()
	case reflect.TypeFor[temporal.DateTime]():
		return codec.DateTime()
	case reflect.TypeFor[temporal.Date]():
		return codec.Date()
	case reflect.TypeFor[temporal.Time]():
		return codec.WallTime()
	case reflect.TypeFor[temporal.LocalDateTime]():
		return codec.LocalDateTime()
	}
	return nil
}

// Pointer arguments never enter a function-valued callback: the static dispatch
// below lets escape analysis keep &v on the stack. Each branch delegates codec
// validation to the existing implementation, and writes only on successful decode.
func primitiveCodec[V any](kind reflect.Kind) codec.Codec[V] {
	parameter := primitiveParameter(kind)
	if parameter == 0 {
		return codec.Codec[V]{}
	}
	return codec.New(func(v V) (driver.Value, error) { return bindPrimitive(kind, unsafe.Pointer(&v)) }, func(raw any) (v V, err error) { err = decodePrimitive(kind, raw, unsafe.Pointer(&v)); return }).WithParameterType(parameter)
}

var idRepresentation = codec.ID[struct{}]()

func bindModelID(p unsafe.Pointer) (driver.Value, error) {
	return idRepresentation.Bind(*(*model.ID[struct{}])(p))
}
func decodeModelID(raw any, p unsafe.Pointer) error {
	v, err := idRepresentation.Decode(raw)
	if err == nil {
		*(*model.ID[struct{}])(p) = v
	}
	return err
}

// EnumCodec resolves generated descriptors after package initialization.
func EnumCodec[E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}]() (codec.Codec[E], func() error) {
	resolve := sync.OnceValue(func() enum.Descriptor[E] { var e E; return e.EnumDescriptor() })
	check := sync.OnceValue(func() error {
		if err := resolve().Validate(); err != nil {
			return Invalid("orm: invalid enum descriptor: " + err.Error())
		}
		return nil
	})
	c := primitiveCodec[E](reflect.TypeFor[E]().Kind()).Validated(func(v E) error {
		if err := check(); err != nil {
			return err
		}
		if !resolve().Contains(v) {
			return Invalid("orm: invalid enum value")
		}
		return nil
	})
	return c, check
}
