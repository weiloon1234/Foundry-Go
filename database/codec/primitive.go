package codec

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"reflect"
	"unsafe"
)

// Each concrete primitive codec is instantiated once here, shared by named types.
var (
	primitiveBool    = Bool[bool]()
	primitiveString  = String[string]()
	primitiveInt     = Signed[int]()
	primitiveInt8    = Signed[int8]()
	primitiveInt16   = Signed[int16]()
	primitiveInt32   = Signed[int32]()
	primitiveInt64   = Signed[int64]()
	primitiveUint    = Unsigned[uint]()
	primitiveUint8   = Unsigned[uint8]()
	primitiveUint16  = Unsigned[uint16]()
	primitiveUint32  = Unsigned[uint32]()
	primitiveUint64  = Unsigned[uint64]()
	primitiveFloat32 = Float[float32]()
	primitiveFloat64 = Float[float64]()
)

func primitiveParameter(k reflect.Kind) ParameterType {
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
		return primitiveBool.encode(*(*bool)(p))
	case reflect.String:
		return primitiveString.encode(*(*string)(p))
	case reflect.Int:
		return primitiveInt.encode(*(*int)(p))
	case reflect.Int8:
		return primitiveInt8.encode(*(*int8)(p))
	case reflect.Int16:
		return primitiveInt16.encode(*(*int16)(p))
	case reflect.Int32:
		return primitiveInt32.encode(*(*int32)(p))
	case reflect.Int64:
		return primitiveInt64.encode(*(*int64)(p))
	case reflect.Uint:
		return primitiveUint.encode(*(*uint)(p))
	case reflect.Uint8:
		return primitiveUint8.encode(*(*uint8)(p))
	case reflect.Uint16:
		return primitiveUint16.encode(*(*uint16)(p))
	case reflect.Uint32:
		return primitiveUint32.encode(*(*uint32)(p))
	case reflect.Uint64:
		return primitiveUint64.encode(*(*uint64)(p))
	case reflect.Float32:
		return primitiveFloat32.encode(*(*float32)(p))
	case reflect.Float64:
		return primitiveFloat64.encode(*(*float64)(p))
	}
	return nil, invalid()
}
func decodePrimitive(k reflect.Kind, raw any, p unsafe.Pointer) error {
	switch k {
	case reflect.Bool:
		v, err := primitiveBool.decode(raw)
		if err == nil {
			*(*bool)(p) = v
		}
		return err
	case reflect.String:
		v, err := primitiveString.decode(raw)
		if err == nil {
			*(*string)(p) = v
		}
		return err
	case reflect.Int:
		v, err := primitiveInt.decode(raw)
		if err == nil {
			*(*int)(p) = v
		}
		return err
	case reflect.Int8:
		v, err := primitiveInt8.decode(raw)
		if err == nil {
			*(*int8)(p) = v
		}
		return err
	case reflect.Int16:
		v, err := primitiveInt16.decode(raw)
		if err == nil {
			*(*int16)(p) = v
		}
		return err
	case reflect.Int32:
		v, err := primitiveInt32.decode(raw)
		if err == nil {
			*(*int32)(p) = v
		}
		return err
	case reflect.Int64:
		v, err := primitiveInt64.decode(raw)
		if err == nil {
			*(*int64)(p) = v
		}
		return err
	case reflect.Uint:
		v, err := primitiveUint.decode(raw)
		if err == nil {
			*(*uint)(p) = v
		}
		return err
	case reflect.Uint8:
		v, err := primitiveUint8.decode(raw)
		if err == nil {
			*(*uint8)(p) = v
		}
		return err
	case reflect.Uint16:
		v, err := primitiveUint16.decode(raw)
		if err == nil {
			*(*uint16)(p) = v
		}
		return err
	case reflect.Uint32:
		v, err := primitiveUint32.decode(raw)
		if err == nil {
			*(*uint32)(p) = v
		}
		return err
	case reflect.Uint64:
		v, err := primitiveUint64.decode(raw)
		if err == nil {
			*(*uint64)(p) = v
		}
		return err
	case reflect.Float32:
		v, err := primitiveFloat32.decode(raw)
		if err == nil {
			*(*float32)(p) = v
		}
		return err
	case reflect.Float64:
		v, err := primitiveFloat64.decode(raw)
		if err == nil {
			*(*float64)(p) = v
		}
		return err
	}
	return invalid()
}

// FoundryPrimitive supplies the native ORM representation adapter. Each concrete
// primitive encoder is shared; the returned codec owns the single callback
// isolation boundary, just as Adapt does for an explicit representation.
func FoundryPrimitive[V any](seal sqlowner.MetadataSeal) Codec[V] {
	if !seal.Valid() {
		return Codec[V]{}
	}
	kind := reflect.TypeFor[V]().Kind()
	parameter := primitiveParameter(kind)
	if parameter == 0 {
		return Codec[V]{}
	}
	return New(func(v V) (driver.Value, error) { return bindPrimitive(kind, unsafe.Pointer(&v)) }, func(raw any) (v V, err error) { err = decodePrimitive(kind, raw, unsafe.Pointer(&v)); return }).WithParameterType(parameter)
}
