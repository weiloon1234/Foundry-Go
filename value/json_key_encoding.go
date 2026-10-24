package value

import (
	"encoding"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Keep application map-key failures out of the standard encoder's error
// formatting and inspection. The native encoder sees only a framework error;
// the original cause is reattached after it returns.
type jsonKeyEncoding struct{ cause error }

func (s *jsonKeyEncoding) options() jsonv2.Options {
	return jsonv2.WithMarshalers(jsonv2.MarshalToFunc[any](s.marshal))
}
func (s *jsonKeyEncoding) marshal(enc *jsontext.Encoder, input any) error {
	// MarshalToFunc supplies a pointer to the original value, even for interfaces.
	key := reflect.ValueOf(input).Elem()
	kind, n := enc.StackIndex(enc.StackDepth())
	if kind != '{' || n%2 != 0 {
		return errors.ErrUnsupported
	}
	// String spelling precedes methods; native AppendText precedes MarshalText,
	// then integers use their decimal spelling.
	// JSON methods never override map-key spelling.
	if key.Kind() == reflect.String {
		return enc.WriteToken(jsontext.String(key.String()))
	}
	var text []byte
	var err error
	switch key := key.Interface().(type) {
	case encoding.TextAppender:
		if reflect.ValueOf(key).Kind() == reflect.Pointer && reflect.ValueOf(key).IsNil() {
			return enc.WriteToken(jsontext.String(""))
		}
		text, err = key.AppendText(nil)
	case encoding.TextMarshaler:
		if reflect.ValueOf(key).Kind() == reflect.Pointer && reflect.ValueOf(key).IsNil() {
			return enc.WriteToken(jsontext.String(""))
		}
		text, err = key.MarshalText()
	default:
		var name string
		switch key := reflect.ValueOf(key); key.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			name = strconv.FormatInt(key.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			name = strconv.FormatUint(key.Uint(), 10)
		default:
			return fault.New(fault.Invalid, "unsupported JSON map key")
		}
		return enc.WriteToken(jsontext.String(name))
	}
	if err != nil {
		s.cause = err
		return fault.New(fault.Invalid, "JSON map key encoding failed")
	}
	return enc.WriteToken(jsontext.String(string(text)))
}
func (s *jsonKeyEncoding) failure(err error) error {
	if s.cause != nil {
		return safeerror.Wrap("JSON map key encoding failed", s.cause)
	}
	return safeerror.Wrap("JSON encoding failed", err)
}
