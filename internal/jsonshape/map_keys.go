package jsonshape

import (
	"encoding"
	"reflect"
)

// MapKeyCapabilities describes the legacy map-key rules used by Foundry's
// native JSON boundary. JSON marshal/unmarshal methods do not supply key codecs;
// text decoding takes precedence, and string keys bypass text encoding.
func MapKeyCapabilities(typ reflect.Type) (supported, custom bool) {
	if typ == nil || !typ.Comparable() {
		return false, false
	}
	decode := reflect.PointerTo(typ).Implements(reflect.TypeFor[encoding.TextUnmarshaler]())
	encode := typ.Implements(reflect.TypeFor[encoding.TextMarshaler]()) ||
		typ.Implements(reflect.TypeFor[encoding.TextAppender]())
	switch typ.Kind() {
	case reflect.String:
		return true, decode
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true, decode || encode
	}
	return decode && encode, decode || encode
}

// MapKeyCodecMethod shares key-specific method discovery with code generation.
func MapKeyCodecMethod(name string, stringKey bool) bool {
	return name == "UnmarshalText" || (!stringKey && (name == "MarshalText" || name == "AppendText"))
}
