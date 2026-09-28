// Package contractmeta describes a closed set of framework-owned wire structs.
// Application DTOs always supply their generated typed contract explicitly.
package contractmeta

import (
	"reflect"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TypeID(typ reflect.Type) contract.TypeID {
	if typ.PkgPath() != "" && typ.Name() != "" {
		return contract.TypeID(typ.PkgPath() + "." + typ.Name())
	}
	return contract.TypeID(typ.String())
}

// Struct uses actual JSON fields/tags and the shared promotion rules. Only
// listed structs may be inspected. Overrides and includes retain existing
// typed providers, so their native codecs are not erased by serialization.
func Struct(root reflect.Type, structures []reflect.Type, overrides map[reflect.Type]contract.Type, includes ...contract.Type) (contract.Schema, error) {
	invalid := func() (contract.Schema, error) {
		return contract.Schema{}, fault.New(fault.Invalid, "invalid framework wire metadata")
	}
	if root == nil || len(structures) > jsonwire.MaxNodes || len(includes) > jsonwire.MaxNodes {
		return invalid()
	}
	allowed := make(map[reflect.Type]bool, len(structures))
	for _, typ := range structures {
		if typ == nil || typ.Kind() != reflect.Struct {
			return invalid()
		}
		allowed[typ] = true
	}
	nodes := make(map[contract.TypeID]contract.Type)
	for _, node := range includes {
		if _, exists := nodes[node.ID]; exists {
			return invalid()
		}
		nodes[node.ID] = node
	}
	var shape func(reflect.Type, int) (contract.TypeID, bool)
	shape = func(typ reflect.Type, depth int) (contract.TypeID, bool) {
		if typ == nil || depth > jsonwire.MaxDepth || len(nodes) >= jsonwire.MaxNodes {
			return "", false
		}
		if override, ok := overrides[typ]; ok {
			if previous, exists := nodes[override.ID]; exists && !reflect.DeepEqual(previous, override) {
				return "", false
			}
			nodes[override.ID] = override
			return override.ID, true
		}
		id := TypeID(typ)
		if _, exists := nodes[id]; exists {
			return id, true
		}
		if jsonshape.HasEncoder(typ, true) || jsonshape.HasDecoder(typ, true) {
			return "", false
		}
		node := contract.Type{ID: id}
		nodes[id] = node // recursive structural references are legal
		switch typ.Kind() {
		case reflect.Bool:
			node.Kind = contract.BooleanKind
		case reflect.String:
			node.Kind = contract.StringKind
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			node.Kind, node.Bits, node.Signed = contract.IntegerKind, uint8(typ.Bits()), true
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			node.Kind, node.Bits = contract.IntegerKind, uint8(typ.Bits())
		case reflect.Float32, reflect.Float64:
			node.Kind, node.Bits = contract.NumberKind, uint8(typ.Bits())
		case reflect.Pointer, reflect.Slice, reflect.Array:
			// []byte has encoding/json's base64 special case. It requires an
			// explicit override instead of pretending it is an integer array.
			if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
				return "", false
			}
			child, ok := shape(typ.Elem(), depth+1)
			if !ok {
				return "", false
			}
			node.Element = child
			if typ.Kind() == reflect.Pointer {
				node.Kind, node.Nullable = contract.AliasKind, true
			} else {
				node.Kind, node.Nullable = contract.ArrayKind, typ.Kind() == reflect.Slice
				if typ.Kind() == reflect.Array {
					node.Length = value.Set(typ.Len())
				}
			}
		case reflect.Struct:
			if !allowed[typ] {
				return "", false
			}
			node.Kind = contract.ObjectKind
			properties, err := jsonshape.Collect(typ, reflectionAdapter(allowed))
			if err != nil {
				return "", false
			}
			for _, property := range properties {
				child, ok := shape(property.Type, depth+1)
				if !ok {
					return "", false
				}
				node.Properties = append(node.Properties, contract.Property{Name: property.Name, Type: child, Required: !property.Optional})
			}
		default:
			return "", false
		}
		nodes[id] = node
		return id, true
	}
	id, ok := shape(root, 0)
	if !ok {
		return invalid()
	}
	result := contract.Schema{Root: id, Types: make([]contract.Type, 0, len(nodes))}
	for _, node := range nodes {
		result.Types = append(result.Types, node)
	}
	slices.SortFunc(result.Types, func(a, b contract.Type) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return result, nil
}

func reflectionAdapter(allowed map[reflect.Type]bool) jsonshape.Adapter[reflect.Type] {
	return jsonshape.Adapter[reflect.Type]{
		NumFields: func(t reflect.Type) (int, bool) {
			if t.Kind() != reflect.Struct {
				return 0, false
			}
			return t.NumField(), true
		},
		AllowStruct: func(t reflect.Type) bool { return allowed[t] },
		Field: func(t reflect.Type, i int) jsonshape.Field[reflect.Type] {
			f := t.Field(i)
			return jsonshape.Field[reflect.Type]{Name: f.Name, Tag: f.Tag.Get("json"), Type: f.Type, Anonymous: f.Anonymous, Exported: f.IsExported()}
		},
		Pointer: func(t reflect.Type) (reflect.Type, bool) {
			if t.Kind() == reflect.Pointer {
				return t.Elem(), true
			}
			return t, false
		},
		Optional:     func(reflect.Type) bool { return false },
		QuotedScalar: func(reflect.Type) bool { return false },
	}
}
