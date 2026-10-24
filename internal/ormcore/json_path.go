package ormcore

import (
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// JSONFieldBinding is immutable metadata, resolved once at declaration startup.
// A zero binding is invalid; no selector or application pointer is retained.
type JSONFieldBinding struct {
	Name   string
	Quoted bool
	Err    error
	Bound  bool
}

func (b JSONFieldBinding) Validate() error {
	if b.Err != nil {
		return b.Err
	}
	if !b.Bound {
		return Invalid("uninitialized JSON field declaration")
	}
	return nil
}

func JSONFieldAt(root reflect.Value, ptr unsafe.Pointer, field reflect.Type) JSONFieldBinding {
	fail := func() JSONFieldBinding {
		return JSONFieldBinding{Err: Invalid("JSON selector must name one exported JSON payload field")}
	}
	if ptr == nil || root.Kind() != reflect.Struct || field.Size() == 0 {
		return fail()
	}
	adapter := jsonshape.ReflectAdapter(nil)
	adapter.AllowStruct = func(t reflect.Type) bool { return !jsonshape.HasEncoder(t, true) && !jsonshape.HasDecoder(t, true) }
	properties, err := jsonshape.Collect(root.Type(), adapter)
	if err != nil {
		return fail()
	}
	base, address := root.UnsafeAddr(), uintptr(ptr)
	if address < base || address-base >= root.Type().Size() || modelmeta.OffsetMatches(root.Type(), address-base, field) != 1 {
		return fail()
	}
	for _, property := range properties {
		v, err := root.FieldByIndexErr(property.Index)
		if err == nil && v.CanAddr() && v.Type() == field && v.UnsafeAddr() == address {
			return JSONFieldBinding{Name: property.Name, Quoted: property.Quoted, Bound: true, Err: querycore.ValidateJSONProperty(property.Name)}
		}
	}
	return fail()
}

func JSONArrayAt(root reflect.Value, ptr unsafe.Pointer, element reflect.Type) error {
	if root.Kind() != reflect.Array || root.Type().Elem() != element || element.Size() == 0 || ptr == nil || jsonshape.HasEncoder(root.Type(), true) || jsonshape.HasDecoder(root.Type(), true) {
		return Invalid("JSON array selector must name an element of a fixed array")
	}
	base, address := root.UnsafeAddr(), uintptr(ptr)
	if address < base || address-base >= root.Type().Size() || (address-base)%element.Size() != 0 {
		return Invalid("JSON array selector points outside its array")
	}
	return nil
}

// JSONCollection rejects custom encodings and base64 byte slices, whose JSON
// representation is not the Go map/slice structure. Fixed byte arrays are arrays.
func JSONCollection(t reflect.Type) error {
	if jsonshape.HasEncoder(t, true) || jsonshape.HasDecoder(t, true) || (t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8) {
		return Invalid("JSON path requires a structural collection")
	}
	return nil
}
