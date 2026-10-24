package jsonshape

import "reflect"

// ReflectAdapter shares runtime field/tag facts with handwritten path declarations.
// optional supplies the value package's private optional-wrapper knowledge.
func ReflectAdapter(optional func(reflect.Type) bool) Adapter[reflect.Type] {
	if optional == nil {
		optional = func(reflect.Type) bool { return false }
	}
	return Adapter[reflect.Type]{
		NumFields: func(t reflect.Type) (int, bool) {
			if t.Kind() != reflect.Struct {
				return 0, false
			}
			return t.NumField(), true
		},
		Field: func(t reflect.Type, i int) Field[reflect.Type] {
			f := t.Field(i)
			return Field[reflect.Type]{Name: f.Name, Tag: f.Tag.Get("json"), Type: f.Type, Anonymous: f.Anonymous, Exported: f.PkgPath == ""}
		},
		Pointer: func(t reflect.Type) (reflect.Type, bool) {
			if t.Kind() == reflect.Pointer {
				return t.Elem(), true
			}
			return t, false
		},
		Optional: optional,
		QuotedScalar: func(t reflect.Type) bool {
			switch t.Kind() {
			case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
				return true
			}
			return false
		},
	}
}
