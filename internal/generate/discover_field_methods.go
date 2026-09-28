package generate

import (
	"go/types"
	"slices"
	"strings"
)

func fieldMethodPrefix(name string) string {
	for _, prefix := range []string{"Mutate", "Access"} {
		if strings.HasPrefix(name, prefix) {
			return prefix
		}
	}
	return ""
}

// Field methods are resolved through consumer Go type information on a fresh
// checkout. Native explicit getters need no duplicate generated wrapper.
func discoverFieldMethods(p *packageInput, m *model) error {
	for _, f := range m.fields {
		if f.name == "FoundryCreateMutation" {
			return p.diagnostic(m.typ.Obj().Pos(), "model field FoundryCreateMutation conflicts with generated draft integration")
		}
		if f.name == "Format" {
			return p.diagnostic(m.typ.Obj().Pos(), "model field Format conflicts with generated draft diagnostics")
		}
	}
	for i := range m.typ.NumMethods() {
		method := m.typ.Method(i)
		prefix := fieldMethodPrefix(method.Name())
		if prefix == "" {
			continue
		}
		fieldName := strings.TrimPrefix(method.Name(), prefix)
		kind := "mutator"
		if prefix == "Access" {
			kind = "accessor"
		}
		index := slices.IndexFunc(m.fields, func(f field) bool { return f.name == fieldName })
		if index < 0 {
			return p.diagnostic(method.Pos(), "model "+kind+" targets no persisted field: "+method.Name())
		}
		f := &m.fields[index]
		sig := method.Type().(*types.Signature)
		_, pointer := sig.Recv().Type().(*types.Pointer)
		valid := !pointer && !sig.Variadic() && sig.Results().Len() == 2 &&
			types.Identical(sig.Results().At(1).Type(), types.Universe.Lookup("error").Type())
		if prefix == "Mutate" {
			if !valid || sig.Params().Len() != 1 ||
				types.IsInterface(sig.Params().At(0).Type()) ||
				!types.Identical(sig.Results().At(0).Type(), f.base) {
				return p.diagnostic(method.Pos(), "model mutator requires a value receiver and signature "+method.Name()+"(Input) (fieldType, error); Input must be concrete and nullable fields use a non-null input type")
			}
			input := sig.Params().At(0).Type()
			if named, ok := types.Unalias(input).(*types.Named); ok && isNamed(named, framework+"/value", "Nullable") {
				return p.diagnostic(method.Pos(), "model mutator requires a value receiver and signature "+method.Name()+"(Input) (fieldType, error); nullable fields use a non-null input type")
			}
			if !types.Identical(input, f.base) {
				f.input = input
			}
			f.mutator = method.Name()
			continue
		}
		if !valid || sig.Params().Len() != 0 || types.IsInterface(sig.Results().At(0).Type()) {
			return p.diagnostic(method.Pos(), "model accessor requires a value receiver and signature "+method.Name()+"() (Result, error), where Result is a concrete Go type")
		}
		f.accessor = method.Name()
	}
	return nil
}
