package jsonshape

import (
	"encoding"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
)

var encoders = [...]reflect.Type{
	reflect.TypeFor[json.Marshaler](), reflect.TypeFor[jsonv2.MarshalerTo](),
	reflect.TypeFor[encoding.TextMarshaler](), reflect.TypeFor[encoding.TextAppender](),
}
var decoders = [...]reflect.Type{
	reflect.TypeFor[json.Unmarshaler](), reflect.TypeFor[jsonv2.UnmarshalerFrom](),
	reflect.TypeFor[encoding.TextUnmarshaler](),
}

// HasEncoder detects supported JSON/text encoders without invoking them.
func HasEncoder(typ reflect.Type, addressable bool) bool {
	return hasCodec(typ, addressable, encoders[:])
}

// HasDecoder detects supported JSON/text decoders without invoking them.
func HasDecoder(typ reflect.Type, addressable bool) bool {
	return hasCodec(typ, addressable, decoders[:])
}

func hasCodec(typ reflect.Type, addressable bool, codecs []reflect.Type) bool {
	if typ == nil {
		return false
	}
	for _, codec := range codecs {
		if typ.Implements(codec) || (addressable && reflect.PointerTo(typ).Implements(codec)) {
			return true
		}
	}
	return false
}
