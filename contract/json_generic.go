package contract

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/gotype"
	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

// GoTypeID is the identity boundary used by generated generic DTO declarations.
// source is a qualified Go type expression, not a wire schema or a sample value.
// Named and primitive values use named=true; composite expressions use false.
func GoTypeID(source string, named bool) TypeID {
	return TypeID(gotype.Identity(source, named))
}

// GoTypeName returns the concrete source identity for generic substitution.
// Generated descriptors retain executable package names which native reflection
// reports as main. No JSON fields or schema are inferred here.
func (d JSON[T]) GoTypeName() string {
	if d.sourceName != "" {
		return d.sourceName
	}
	return reflectedGoName(reflect.TypeFor[T]())
}

// JSONParameter includes a typed argument in a generated generic graph. The
// generated identity names the actual Go type; the argument owns its wire shape.
// A different root name (for example Slice's synthetic root) is rewritten in an
// owned snapshot. Invalid arguments remain invalid, including unused arguments.
func JSONParameter[T any](id TypeID, argument JSON[T]) Type {
	return Type{ID: id, jsonContract: func() (Schema, error) {
		description, err := argument.Description()
		if err != nil || jsonModelType(reflect.TypeFor[T]()) || id != GoTypeID(argument.GoTypeName(), namedGoType(reflect.TypeFor[T]())) {
			return Schema{}, invalidSchema()
		}
		old := description.Root
		for i := range description.Types {
			node := &description.Types[i]
			if node.ID == old {
				node.ID = id
			}
			if node.Element == old {
				node.Element = id
			}
			for j := range node.Properties {
				if node.Properties[j].Type == old {
					node.Properties[j].Type = id
				}
			}
			for j := range node.Variants {
				if node.Variants[j].Type == old {
					node.Variants[j].Type = id
				}
			}
			if node.Key != nil && node.Key.Value.ID == old {
				key := cloneJSONKeyInfo(*node.Key)
				key.Value.ID = id
				node.Key = &key
				if node.jsonKey != nil {
					runtime := *node.jsonKey
					runtime.info = cloneJSONKeyInfo(key)
					node.jsonKey = &runtime
				}
			}
		}
		description.Root = id
		return description, nil
	}}
}

// DefineGenericJSON is the generated generic DTO construction boundary. source
// retains the fully instantiated Go source expression. Schema nodes may coincide
// after substituting repeated equal arguments; the shared compiler accepts them
// only when their complete normalized wire definitions agree.
func DefineGenericJSON[T any](source string, description Schema) JSON[T] {
	typ := reflect.TypeFor[T]()
	if typ.Kind() != reflect.Struct || !jsonDeclarationMatches(typ, description.Root) ||
		description.Root != GoTypeID(source, true) {
		return JSON[T]{err: invalidSchema()}
	}
	schema, err := compileSchemaGraph(description, false, true)
	if err == nil && (schema.types[description.Root].Kind != ObjectKind || schema.types[description.Root].Nullable) {
		err = invalidSchema()
	}
	return JSON[T]{schema: schema, err: err, sourceName: source}
}

// JSONSliceType specializes the native byte-slice representation after generic
// substitution, including named generic slice types. Ordinary slices retain
// their element contract and nullability.
// No element values or schema fields are inspected.
func JSONSliceType[S ~[]E, E any](id, element TypeID) Type {
	return jsonSliceType(id, element, reflect.TypeFor[E]())
}

func jsonSliceType(id, element TypeID, typ reflect.Type) Type {
	if typ.Kind() == reflect.Uint8 && !jsonshape.HasEncoder(typ, true) {
		return Type{ID: id, Kind: StringKind, Nullable: true, Format: Base64Format, byteElement: element}
	}
	return Type{ID: id, Kind: ArrayKind, Nullable: true, Element: element}
}

// JSONArgumentID retains the actual type of a supplied generic argument.
func JSONArgumentID[T any](argument JSON[T]) TypeID {
	return GoTypeID(argument.GoTypeName(), namedGoType(reflect.TypeFor[T]()))
}

func namedGoType(typ reflect.Type) bool { return typ.Name() != "" }

// reflectedGoName is a fallback for explicit field descriptors. Generated roots
// and the ordinary Slice/Nullable constructors retain their source names directly.
func reflectedGoName(typ reflect.Type) string {
	if typ.Name() != "" {
		if typ.PkgPath() != "" {
			return gotype.Source(typ.PkgPath() + "." + typ.Name())
		}
		return typ.Name()
	}
	switch typ.Kind() {
	case reflect.Pointer:
		return "*" + reflectedGoName(typ.Elem())
	case reflect.Slice:
		return "[]" + reflectedGoName(typ.Elem())
	case reflect.Map:
		return "map[" + reflectedGoName(typ.Key()) + "]" + reflectedGoName(typ.Elem())
	case reflect.Array:
		return "[" + strconv.Itoa(typ.Len()) + "]" + reflectedGoName(typ.Elem())
	case reflect.Interface:
		if typ.NumMethod() == 0 {
			return "interface{}"
		}
	case reflect.Struct:
		var result strings.Builder
		result.WriteString("struct{")
		for i := 0; i < typ.NumField(); i++ {
			if i != 0 {
				result.WriteString("; ")
			}
			field := typ.Field(i)
			if !field.Anonymous {
				result.WriteString(field.Name + " ")
			}
			result.WriteString(reflectedGoName(field.Type))
			if field.Tag != "" {
				result.WriteString(" " + strconv.Quote(string(field.Tag)))
			}
		}
		result.WriteByte('}')
		return result.String()
	}
	return typ.String()
}
