package jobs

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"reflect"
)

// Jobs use concrete DTOs. Reject models and runtime capabilities even when
// encoding/json would silently ignore their unexported state. Custom value
// codecs own their representation; no codec is called during registration.
func validatePayloadType(root reflect.Type) error {
	if root == nil || root.Kind() != reflect.Struct || root.Name() == "" {
		return fault.New(fault.Invalid, "job payload must be a named concrete struct")
	}
	seen := make(map[reflect.Type]bool)
	var visit func(reflect.Type) error
	visit = func(t reflect.Type) error {
		if seen[t] {
			return nil
		}
		seen[t] = true
		for _, forbidden := range []reflect.Type{reflect.TypeFor[model.Identifiable](), reflect.TypeFor[context.Context](), reflect.TypeFor[error]()} {
			if t.Implements(forbidden) || reflect.PointerTo(t).Implements(forbidden) {
				return fault.New(fault.Invalid, "job payload cannot contain models or runtime capabilities")
			}
		}
		if t.PkgPath() == "github.com/weiloon1234/Foundry-Go/secret" {
			return fault.New(fault.Invalid, "job payload cannot contain credentials")
		}
		switch t.Kind() {
		case reflect.Interface, reflect.Func, reflect.Chan, reflect.UnsafePointer, reflect.Uintptr, reflect.Complex64, reflect.Complex128:
			return fault.New(fault.Invalid, "job payload requires concrete serializable fields")
		case reflect.Array:
			if t.Len() == 0 {
				return nil
			} // Phantom ownership carries no runtime value.
			return visit(t.Elem())
		case reflect.Pointer, reflect.Slice:
			return visit(t.Elem())
		case reflect.Map:
			if err := visit(t.Key()); err != nil {
				return err
			}
			return visit(t.Elem())
		case reflect.Struct:
			// Inspect private fields too: wrappers must not conceal live models,
			// contexts or secrets. Primitive value wrappers remain allowed.
			for i := 0; i < t.NumField(); i++ {
				if err := visit(t.Field(i).Type); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(root)
}
