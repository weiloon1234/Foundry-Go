package modelmeta

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
	"unsafe"
)

// FieldAt validates a pointer against one exported, unambiguous top-level field.
func FieldAt(model reflect.Type, template, ptr unsafe.Pointer, typ reflect.Type) (int, error) {
	fail := func(message string) (int, error) { return -1, safeerror.New(message) }
	if ptr == nil || model.Kind() != reflect.Struct {
		return fail("field pointer must name a top-level model field")
	}
	if typ.Size() == 0 {
		return fail("zero-sized persisted column types are unsupported")
	}
	base, address := uintptr(template), uintptr(ptr)
	if address < base || address-base >= model.Size() {
		return fail("field pointer has an invalid offset; pass &o.Field from Define")
	}
	offset := address - base
	index := -1
	for i := 0; i < model.NumField(); i++ {
		f := model.Field(i)
		if f.Offset == offset && f.Type == typ && !f.Anonymous && f.IsExported() {
			index = i
			break
		}
	}
	if index < 0 {
		return fail("field pointer offset/type must match an exported top-level field")
	}
	if OffsetMatches(model, offset, typ) != 1 {
		return fail("ambiguous field offset/type matches a nested or zero-size neighbor")
	}
	return index, nil
}

// OffsetMatches includes nested fields and zero-sized neighbors in ambiguity checks.
func OffsetMatches(t reflect.Type, offset uintptr, target reflect.Type) int {
	n := 0
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Offset == offset && f.Type == target {
			n++
		}
		if f.Type.Kind() == reflect.Struct && offset >= f.Offset && offset-f.Offset <= f.Type.Size() {
			n += OffsetMatches(f.Type, offset-f.Offset, target)
		}
	}
	return n
}
