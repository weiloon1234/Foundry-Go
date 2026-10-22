package ormcore

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"reflect"
	"unsafe"
)

// Each concrete primitive codec is instantiated once here, shared by named types.
var (
	primitiveBool    = codec.Bool[bool]()
	primitiveString  = codec.String[string]()
	primitiveInt     = codec.Signed[int]()
	primitiveInt8    = codec.Signed[int8]()
	primitiveInt16   = codec.Signed[int16]()
	primitiveInt32   = codec.Signed[int32]()
	primitiveInt64   = codec.Signed[int64]()
	primitiveUint    = codec.Unsigned[uint]()
	primitiveUint8   = codec.Unsigned[uint8]()
	primitiveUint16  = codec.Unsigned[uint16]()
	primitiveUint32  = codec.Unsigned[uint32]()
	primitiveUint64  = codec.Unsigned[uint64]()
	primitiveFloat32 = codec.Float[float32]()
	primitiveFloat64 = codec.Float[float64]()
)

func integerKind(k reflect.Kind) bool {
	return k >= reflect.Int && k <= reflect.Int64 || k >= reflect.Uint && k <= reflect.Uint64
}
func numericKind(k reflect.Kind) bool {
	return integerKind(k) || k == reflect.Float32 || k == reflect.Float64
}
func primitiveParameter(k reflect.Kind) codec.ParameterType {
	switch k {
	case reflect.Bool:
		return primitiveBool.ParameterType()
	case reflect.String:
		return primitiveString.ParameterType()
	case reflect.Int:
		return primitiveInt.ParameterType()
	case reflect.Int8:
		return primitiveInt8.ParameterType()
	case reflect.Int16:
		return primitiveInt16.ParameterType()
	case reflect.Int32:
		return primitiveInt32.ParameterType()
	case reflect.Int64:
		return primitiveInt64.ParameterType()
	case reflect.Uint:
		return primitiveUint.ParameterType()
	case reflect.Uint8:
		return primitiveUint8.ParameterType()
	case reflect.Uint16:
		return primitiveUint16.ParameterType()
	case reflect.Uint32:
		return primitiveUint32.ParameterType()
	case reflect.Uint64:
		return primitiveUint64.ParameterType()
	case reflect.Float32:
		return primitiveFloat32.ParameterType()
	case reflect.Float64:
		return primitiveFloat64.ParameterType()
	}
	return 0
}
func bindPrimitive(k reflect.Kind, p unsafe.Pointer) (driver.Value, error) {
	switch k {
	case reflect.Bool:
		return primitiveBool.Bind(*(*bool)(p))
	case reflect.String:
		return primitiveString.Bind(*(*string)(p))
	case reflect.Int:
		return primitiveInt.Bind(*(*int)(p))
	case reflect.Int8:
		return primitiveInt8.Bind(*(*int8)(p))
	case reflect.Int16:
		return primitiveInt16.Bind(*(*int16)(p))
	case reflect.Int32:
		return primitiveInt32.Bind(*(*int32)(p))
	case reflect.Int64:
		return primitiveInt64.Bind(*(*int64)(p))
	case reflect.Uint:
		return primitiveUint.Bind(*(*uint)(p))
	case reflect.Uint8:
		return primitiveUint8.Bind(*(*uint8)(p))
	case reflect.Uint16:
		return primitiveUint16.Bind(*(*uint16)(p))
	case reflect.Uint32:
		return primitiveUint32.Bind(*(*uint32)(p))
	case reflect.Uint64:
		return primitiveUint64.Bind(*(*uint64)(p))
	case reflect.Float32:
		return primitiveFloat32.Bind(*(*float32)(p))
	case reflect.Float64:
		return primitiveFloat64.Bind(*(*float64)(p))
	}
	return nil, Invalid("unsupported primitive codec")
}
func decodePrimitive(k reflect.Kind, raw any, p unsafe.Pointer) error {
	switch k {
	case reflect.Bool:
		v, err := primitiveBool.Decode(raw)
		if err == nil {
			*(*bool)(p) = v
		}
		return err
	case reflect.String:
		v, err := primitiveString.Decode(raw)
		if err == nil {
			*(*string)(p) = v
		}
		return err
	case reflect.Int:
		v, err := primitiveInt.Decode(raw)
		if err == nil {
			*(*int)(p) = v
		}
		return err
	case reflect.Int8:
		v, err := primitiveInt8.Decode(raw)
		if err == nil {
			*(*int8)(p) = v
		}
		return err
	case reflect.Int16:
		v, err := primitiveInt16.Decode(raw)
		if err == nil {
			*(*int16)(p) = v
		}
		return err
	case reflect.Int32:
		v, err := primitiveInt32.Decode(raw)
		if err == nil {
			*(*int32)(p) = v
		}
		return err
	case reflect.Int64:
		v, err := primitiveInt64.Decode(raw)
		if err == nil {
			*(*int64)(p) = v
		}
		return err
	case reflect.Uint:
		v, err := primitiveUint.Decode(raw)
		if err == nil {
			*(*uint)(p) = v
		}
		return err
	case reflect.Uint8:
		v, err := primitiveUint8.Decode(raw)
		if err == nil {
			*(*uint8)(p) = v
		}
		return err
	case reflect.Uint16:
		v, err := primitiveUint16.Decode(raw)
		if err == nil {
			*(*uint16)(p) = v
		}
		return err
	case reflect.Uint32:
		v, err := primitiveUint32.Decode(raw)
		if err == nil {
			*(*uint32)(p) = v
		}
		return err
	case reflect.Uint64:
		v, err := primitiveUint64.Decode(raw)
		if err == nil {
			*(*uint64)(p) = v
		}
		return err
	case reflect.Float32:
		v, err := primitiveFloat32.Decode(raw)
		if err == nil {
			*(*float32)(p) = v
		}
		return err
	case reflect.Float64:
		v, err := primitiveFloat64.Decode(raw)
		if err == nil {
			*(*float64)(p) = v
		}
		return err
	}
	return Invalid("unsupported primitive codec")
}
