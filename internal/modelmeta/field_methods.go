package modelmeta

import (
	"fmt"
	"reflect"
	"strings"
)

// FieldMethodPrefix identifies the shared model field-method convention.
// Example: FieldMethodPrefix("MutateEmail").
func FieldMethodPrefix(name string) string {
	for _, prefix := range []string{"Mutate", "Access"} {
		if strings.HasPrefix(name, prefix) {
			return prefix
		}
	}
	return ""
}

// FieldMethod describes a validated, unbound model method without invoking it.
// Example: methods, err := FieldMethods(reflect.TypeFor[Member](), fields).
type FieldMethod struct {
	Field  string
	Prefix string
	Bound  any
}

// FieldMethods checks value and pointer method sets, retaining value receivers only.
// Fields contains persisted fields and their non-null stored types.
func FieldMethods(model reflect.Type, fields map[string]reflect.Type) ([]FieldMethod, error) {
	if model.Kind() != reflect.Struct {
		return nil, nil
	}
	errorType := reflect.TypeFor[error]()
	methods := make([]FieldMethod, 0)
	pointer := reflect.PointerTo(model)
	for i := 0; i < pointer.NumMethod(); i++ {
		method := pointer.Method(i)
		prefix := FieldMethodPrefix(method.Name)
		if prefix == "" {
			continue
		}
		name := strings.TrimPrefix(method.Name, prefix)
		stored, ok := fields[name]
		if !ok {
			return nil, fmt.Errorf("model %s targets no persisted field: %s", strings.ToLower(prefix), method.Name)
		}
		method, ok = model.MethodByName(method.Name)
		if !ok {
			return nil, fmt.Errorf("model field method %s requires a value receiver", prefix+name)
		}
		sig := method.Type
		valid := !sig.IsVariadic() && sig.NumOut() == 2 && sig.Out(1) == errorType
		if prefix == "Mutate" {
			if !valid || sig.NumIn() != 2 || !ValidMutatorInput(sig.In(1)) || sig.Out(0) != stored {
				return nil, fmt.Errorf("model mutator %s requires signature (Input) (fieldType, error) with a concrete non-null input", method.Name)
			}
		} else if !valid || sig.NumIn() != 1 || sig.Out(0).Kind() == reflect.Interface {
			return nil, fmt.Errorf("model accessor %s requires signature () (Result, error) with a concrete result", method.Name)
		}
		methods = append(methods, FieldMethod{Field: name, Prefix: prefix, Bound: method.Func.Interface()})
	}
	return methods, nil
}
func nullableType(t reflect.Type) bool {
	return t.PkgPath() == "github.com/weiloon1234/Foundry-Go/value" && strings.HasPrefix(t.Name(), "Nullable[")
}

// ValidMutatorInput rejects interfaces and nullable wrappers at declaration boundaries.
// Example: ValidMutatorInput(reflect.TypeFor[EmailInput]()).
func ValidMutatorInput(t reflect.Type) bool { return t.Kind() != reflect.Interface && !nullableType(t) }
