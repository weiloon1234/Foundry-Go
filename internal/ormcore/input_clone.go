package ormcore

import (
	"bytes"
	"reflect"
	"unsafe"
)

// InputClone selects direct binary-input ownership once, without reflective writes.
// Other custom values retain ordinary Go copy semantics, as in generated drafts.
// Example: clone := InputClone[Payload]().
func InputClone[I any]() func(I) I {
	typ := reflect.TypeFor[I]()
	if typ.Kind() != reflect.Slice || typ.Elem().Kind() != reflect.Uint8 {
		return nil
	}
	return func(v I) I {
		buffer := (*[]byte)(unsafe.Pointer(&v))
		*buffer = bytes.Clone(*buffer)
		return v
	}
}
