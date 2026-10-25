package oracle

import (
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Snapshot records structure, not Go formatting. Named model identities are
// intentionally erased so separately generated and handwritten models compare.
// Kind tags, lengths, field names and presence survive that normalization.
func Snapshot(value any) string      { return snapshot(value, false) }
func ModelSnapshot(value any) string { return snapshot(value, true) }
func snapshot(value any, model bool) string {
	data, err := json.Marshal(structure(reflect.ValueOf(value), snapshotModeFor(model), 0))
	if err != nil {
		panic(err)
	}
	return string(data)
}

type node struct {
	Kind  string `json:"kind"`
	Value any    `json:"value"`
}
type entry struct{ Key, Value any }

type snapshotMode uint8

const (
	rawSnapshot snapshotMode = iota
	modelBoundary
	modelScalar
)

func snapshotModeFor(model bool) snapshotMode {
	if model {
		return modelBoundary
	}
	return rawSnapshot
}

func structure(v reflect.Value, mode snapshotMode, depth int) any {
	if depth > 64 {
		panic("oracle snapshot exceeds fixture depth")
	}
	if !v.IsValid() {
		return node{Kind: "nil"}
	}
	kind := v.Kind().String()
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return node{Kind: "nil"}
		}
		return structure(v.Elem(), mode, depth+1)
	case reflect.Pointer:
		if v.IsNil() {
			return node{Kind: "pointer"}
		}
		return node{kind, structure(v.Elem(), mode, depth+1)}
	case reflect.String:
		return node{kind, strconv.Quote(v.String())}
	case reflect.Bool:
		return node{kind, v.Bool()}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return node{kind, strconv.FormatInt(v.Int(), 10)}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return node{kind, strconv.FormatUint(v.Uint(), 10)}
	case reflect.Float32:
		return node{kind, strconv.FormatUint(uint64(math.Float32bits(float32(v.Float()))), 16)}
	case reflect.Float64:
		return node{kind, strconv.FormatUint(math.Float64bits(v.Float()), 16)}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return node{Kind: kind}
		}
		values := make([]any, v.Len())
		for i := range values {
			values[i] = structure(v.Index(i), mode, depth+1)
		}
		return node{kind, values}
	case reflect.Map:
		if v.IsNil() {
			return node{Kind: kind}
		}
		values := make([]entry, 0, v.Len())
		it := v.MapRange()
		for it.Next() {
			values = append(values, entry{structure(it.Key(), mode, depth+1), structure(it.Value(), mode, depth+1)})
		}
		sort.Slice(values, func(i, j int) bool {
			a, _ := json.Marshal(values[i].Key)
			b, _ := json.Marshal(values[j].Key)
			return string(a) < string(b)
		})
		return node{kind, values}
	case reflect.Struct:
		pkg := v.Type().PkgPath()
		name := strings.Split(v.Type().Name(), "[")[0]
		// Framework scalars keep private canonical storage even when supplied
		// directly. Only model structs and relation model boundaries filter fields.
		if pkg == "github.com/weiloon1234/Foundry-Go/model" || pkg == "github.com/weiloon1234/Foundry-Go/decimal" || pkg == "github.com/weiloon1234/Foundry-Go/temporal" || pkg == "time" {
			if mode == modelBoundary {
				mode = modelScalar
			}
		}
		if v.CanInterface() && (strings.HasPrefix(pkg, "github.com/weiloon1234/Foundry-Go/value") || pkg == "github.com/weiloon1234/Foundry-Go/database/relation") {
			if get := v.MethodByName("Get"); get.IsValid() {
				parts := get.Call(nil)
				var value any
				if parts[1].Bool() {
					childMode := mode
					if mode != rawSnapshot && pkg == "github.com/weiloon1234/Foundry-Go/database/relation" {
						childMode = modelScalar
						if name != "Value" {
							childMode = modelBoundary
						}
					}
					value = structure(parts[0], childMode, depth+1)
				}
				state := []any{parts[1].Bool(), value}
				if loaded := v.MethodByName("IsLoaded"); loaded.IsValid() {
					state = append([]any{loaded.Call(nil)[0].Bool()}, state...)
				}
				return node{"presence:" + strings.Split(v.Type().Name(), "[")[0], state}
			}
		}
		fields := make([]entry, 0, v.NumField())
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if mode == modelBoundary && !f.IsExported() {
				continue
			}
			if mode == modelBoundary && f.Tag.Get("foundry") == "-" {
				continue
			}
			field := v.Field(i)
			if mode == modelBoundary && f.Type.PkgPath() == "github.com/weiloon1234/Foundry-Go/database/relation" && !field.MethodByName("IsLoaded").Call(nil)[0].Bool() {
				continue
			}
			childMode := rawSnapshot
			if mode != rawSnapshot {
				childMode = modelScalar
			}
			if mode != rawSnapshot && pkg == "github.com/weiloon1234/Foundry-Go/database/relation" && name == "Link" {
				childMode = modelBoundary
			}
			fields = append(fields, entry{f.Name, structure(field, childMode, depth+1)})
		}
		return node{kind, fields}
	default:
		panic("unsupported oracle snapshot kind: " + kind)
	}
}
