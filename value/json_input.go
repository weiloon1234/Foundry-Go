package value

import (
	"context"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

// Inspect ordinary input before the standard encoder can allocate a document.
// Codecs own their internal work; post-encoding parsing checks their wire output.
// The queue bounds native work without recursively following pointer cycles.
type jsonInputBudget struct {
	nodes, bytes, steps int
	ctx                 context.Context
	limits              *JSONEncodingLimits
	allowNUL            bool
}
type jsonInputWrapper interface{ jsonInput() reflect.Value }
type jsonInputFrame struct {
	value reflect.Value
	depth int
}

func (b *jsonInputBudget) check(input reflect.Value, depth int) error {
	limits := JSONEncodingLimits{Bytes: JSONMaxBytes, Depth: JSONMaxDepth, Nodes: JSONMaxNodes, Steps: JSONMaxNodes}
	if b.limits != nil {
		limits = *b.limits
	}
	pending := []jsonInputFrame{{input, depth}}
	add := func(v reflect.Value, depth int) error {
		if len(pending) >= limits.Steps-b.steps {
			return invalidJSON()
		}
		pending = append(pending, jsonInputFrame{v, depth})
		return nil
	}
	account := func(n int) error {
		if n > limits.Bytes-b.bytes {
			return invalidJSON()
		}
		b.bytes += n
		return nil
	}
	for len(pending) != 0 {
		if b.ctx != nil {
			if err := b.ctx.Err(); err != nil {
				return err
			}
		}
		if b.steps >= limits.Steps {
			return invalidJSON()
		}
		b.steps++
		frame := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		v, depth := frame.value, frame.depth
		if b.limits == nil && depth > limits.Depth {
			return invalidJSON()
		}
		if v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && !v.IsNil() {
			if err := add(v.Elem(), depth); err != nil {
				return err
			}
			continue
		}
		// Nil pointers retain JSON null; do not invoke promoted value methods on them.
		if v.IsValid() && v.Kind() != reflect.Pointer && v.Kind() != reflect.Interface && v.CanInterface() {
			if wrapper, ok := v.Interface().(jsonInputWrapper); ok {
				if err := add(wrapper.jsonInput(), depth); err != nil {
					return err
				}
				continue
			}
		}
		b.nodes++
		if b.limits == nil && b.nodes > limits.Nodes {
			return invalidJSON()
		}
		if !v.IsValid() || ((v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil()) {
			continue
		}
		typ := v.Type()
		if v.Kind() == reflect.String {
			text := v.String()
			if err := account(len(text)); err != nil {
				return err
			}
			if !utf8.ValidString(text) || (!b.allowNUL && strings.ContainsRune(text, 0)) {
				return invalidJSON()
			}
		}
		bytes := (v.Kind() == reflect.Slice || v.Kind() == reflect.Array) && typ.Elem().Kind() == reflect.Uint8
		if bytes {
			if err := account(v.Len()); err != nil {
				return err
			}
		}
		if jsonshape.HasEncoder(typ, v.CanAddr()) {
			continue
		}
		switch v.Kind() {
		case reflect.Map:
			if v.Len() > (limits.Steps-b.steps-len(pending))/2 {
				return invalidJSON()
			}
			iter := v.MapRange()
			for iter.Next() {
				if err := add(iter.Key(), depth+1); err != nil {
					return err
				}
				if err := add(iter.Value(), depth+1); err != nil {
					return err
				}
			}
		case reflect.Slice, reflect.Array:
			if bytes && v.Kind() == reflect.Slice && !jsonshape.HasEncoder(typ.Elem(), true) {
				continue
			}
			if v.Len() > limits.Steps-b.steps-len(pending) {
				return invalidJSON()
			}
			for i := 0; i < v.Len(); i++ {
				if err := add(v.Index(i), depth+1); err != nil {
					return err
				}
			}
		case reflect.Struct:
			fields, err := jsonFields(typ)
			if err != nil {
				return err
			}
			if len(fields) > (limits.Steps-b.steps-len(pending))/2 {
				return invalidJSON()
			}
			for name, field := range fields {
				child, exists := jsonInputField(v, field.index)
				if !exists || jsonInputOmitted(child, field.tag) {
					continue
				}
				// A declared name is a static string: account its work, node and
				// bytes inline rather than queueing a boxed reflect.Value.
				if len(pending) >= limits.Steps-b.steps {
					return invalidJSON()
				}
				b.steps++
				b.nodes++
				if b.limits == nil && (depth+1 > limits.Depth || b.nodes > limits.Nodes) {
					return invalidJSON()
				}
				if err := account(len(name)); err != nil {
					return err
				}
				if !utf8.ValidString(name) || (!b.allowNUL && strings.ContainsRune(name, 0)) {
					return invalidJSON()
				}
				if err := add(child, depth+1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func jsonInputField(v reflect.Value, index []int) (reflect.Value, bool) {
	for _, i := range index {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v, true
}

// Follow ordinary omission without reading ignored fields. Custom IsZero methods
// share the codec's deterministic, bounded-work requirement and isolation boundary.
func jsonInputOmitted(v reflect.Value, tag string) bool {
	_, options, _ := strings.Cut(tag, ",")
	for _, option := range strings.Split(options, ",") {
		switch option {
		case "omitempty":
			switch v.Kind() {
			case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
				if v.Len() == 0 {
					return true
				}
			case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
				reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.Interface, reflect.Pointer:
				if v.IsZero() {
					return true
				}
			}
		case "omitzero":
			if jsonInputIsZero(v) {
				return true
			}
		}
	}
	return false
}

func jsonInputIsZero(v reflect.Value) bool {
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
		return true
	}
	zeroType := reflect.TypeFor[interface{ IsZero() bool }]()
	if v.CanInterface() && v.Type().Implements(zeroType) {
		return v.Interface().(interface{ IsZero() bool }).IsZero()
	}
	if reflect.PointerTo(v.Type()).Implements(zeroType) && v.CanInterface() {
		if !v.CanAddr() {
			boxed := reflect.New(v.Type()).Elem()
			boxed.Set(v)
			v = boxed
		}
		return v.Addr().Interface().(interface{ IsZero() bool }).IsZero()
	}
	return v.IsZero()
}
