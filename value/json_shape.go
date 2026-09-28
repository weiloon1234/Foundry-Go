package value

import (
	"encoding"
	"encoding/json"
	"reflect"
	"sync"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

type jsonContent interface{ jsonContentType() reflect.Type }
type jsonNullable interface{ jsonNullableType() reflect.Type }
type jsonOptional interface{ jsonOptionalType() reflect.Type }

func isJSONDocument(v any) bool { _, ok := v.(jsonContent); return ok }

func validateJSONShape(node any, typ reflect.Type, depth int) error {
	if depth > JSONMaxDepth {
		return invalidJSON()
	}
	// Unwrap pointers before inspecting value-receiver markers: their promoted
	// methods cannot be called on reflect.Zero's nil pointer.
	if typ.Kind() == reflect.Pointer {
		if node == nil {
			return nil
		}
		return validateJSONShape(node, typ.Elem(), depth+1)
	}
	zero := reflect.Zero(typ).Interface()
	if wrapper, ok := zero.(jsonNullable); ok {
		if node == nil {
			return nil
		}
		return validateJSONShape(node, wrapper.jsonNullableType(), depth+1)
	}
	if wrapper, ok := zero.(jsonOptional); ok {
		inner := wrapper.jsonOptionalType()
		if node == nil && !isNullable(reflect.Zero(inner).Interface()) && !isJSONDocument(reflect.Zero(inner).Interface()) {
			return invalidJSON()
		}
		return validateJSONShape(node, inner, depth+1)
	}
	if wrapper, ok := zero.(jsonContent); ok {
		return validateJSONShape(node, wrapper.jsonContentType(), depth+1)
	}
	if node == nil {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
			return nil
		default:
			return invalidJSON()
		}
	}
	if typ.Implements(reflect.TypeFor[json.Unmarshaler]()) || reflect.PointerTo(typ).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	if typ.Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) || reflect.PointerTo(typ).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		if _, ok := node.(string); !ok {
			return invalidJSON()
		}
		return nil
	}
	if typ == reflect.TypeFor[json.Number]() {
		if _, ok := node.(json.Number); !ok {
			return invalidJSON()
		}
		return nil
	}
	switch typ.Kind() {
	case reflect.Interface:
		return nil // An explicit dynamic payload; Decode uses json.Number.
	case reflect.Bool:
		if _, ok := node.(bool); !ok {
			return invalidJSON()
		}
	case reflect.String:
		if _, ok := node.(string); !ok {
			return invalidJSON()
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		if _, ok := node.(json.Number); !ok {
			return invalidJSON()
		}
	case reflect.Map:
		object, ok := node.(map[string]any)
		if !ok {
			return invalidJSON()
		}
		for _, child := range object {
			if err := validateJSONShape(child, typ.Elem(), depth+1); err != nil {
				return err
			}
		}
		for key := range object {
			if err := validateJSONMapKey(key, typ.Key()); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
			if _, ok := node.(string); ok {
				return nil
			}
		}
		array, ok := node.([]any)
		if !ok || (typ.Kind() == reflect.Array && len(array) != typ.Len()) {
			return invalidJSON()
		}
		for _, child := range array {
			if err := validateJSONShape(child, typ.Elem(), depth+1); err != nil {
				return err
			}
		}
	case reflect.Struct:
		object, ok := node.(map[string]any)
		if !ok {
			return invalidJSON()
		}
		fields, err := jsonFields(typ)
		if err != nil {
			return err
		}
		for key, child := range object {
			field, exists := fields[key]
			if !exists {
				return invalidJSON()
			}
			if field.quoted {
				if child == nil && field.typ.Kind() == reflect.Pointer {
					continue
				}
				quoted, ok := child.(string)
				if !ok {
					return invalidJSON()
				}
				_, inner, err := jsonwire.Parse([]byte(quoted))
				if err != nil || inner == nil {
					return invalidJSON()
				}
				if err := validateJSONShape(inner, field.typ, depth+1); err != nil {
					return err
				}
				continue // encoding/json also validates the concrete numeric range.
			}
			if err := validateJSONShape(child, field.typ, depth+1); err != nil {
				return err
			}
		}
		for key, field := range fields {
			if _, exists := object[key]; !exists && !field.optional {
				return invalidJSON()
			}
		}
	default:
		return invalidJSON()
	}
	return nil
}

type jsonField struct {
	typ              reflect.Type
	optional, quoted bool
	index            []int
}
type jsonFieldResult struct {
	fields map[string]jsonField
	err    error
}

var jsonFieldCache sync.Map // reflect.Type -> immutable jsonFieldResult

func jsonFields(typ reflect.Type) (map[string]jsonField, error) {
	if cached, ok := jsonFieldCache.Load(typ); ok {
		result := cached.(jsonFieldResult)
		return result.fields, result.err
	}
	fields, err := collectJSONFields(typ)
	result, _ := jsonFieldCache.LoadOrStore(typ, jsonFieldResult{fields, err})
	cached := result.(jsonFieldResult)
	return cached.fields, cached.err
}

// Runtime and source generation use one field/tag promotion algorithm.
func collectJSONFields(typ reflect.Type) (map[string]jsonField, error) {
	properties, err := jsonshape.Collect(typ, jsonshape.Adapter[reflect.Type]{
		NumFields: func(t reflect.Type) (int, bool) {
			if t.Kind() != reflect.Struct {
				return 0, false
			}
			return t.NumField(), true
		},
		Field: func(t reflect.Type, i int) jsonshape.Field[reflect.Type] {
			f := t.Field(i)
			return jsonshape.Field[reflect.Type]{Name: f.Name, Tag: f.Tag.Get("json"), Type: f.Type, Anonymous: f.Anonymous, Exported: f.PkgPath == ""}
		},
		Pointer: func(t reflect.Type) (reflect.Type, bool) {
			if t.Kind() == reflect.Pointer {
				return t.Elem(), true
			}
			return t, false
		},
		Optional: func(t reflect.Type) bool { _, ok := reflect.Zero(t).Interface().(jsonOptional); return ok },
		QuotedScalar: func(t reflect.Type) bool {
			switch t.Kind() {
			case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
				return true
			}
			return false
		},
	})
	if err != nil {
		return nil, invalidJSON()
	}
	fields := make(map[string]jsonField, len(properties))
	for _, p := range properties {
		fields[p.Name] = jsonField{typ: p.Type, optional: p.Optional, quoted: p.Quoted, index: p.Index}
	}
	return fields, nil
}
