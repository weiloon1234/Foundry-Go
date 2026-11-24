package generate

import (
	"go/types"
	"reflect"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

func modelKeyType(t types.Type, customCodec bool) bool {
	if !types.Comparable(t) {
		return false
	}
	if customCodec {
		return true
	}
	n, _ := types.Unalias(t).(*types.Named)
	if n != nil && isNamed(n, framework+"/model", "ID") {
		return true
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && (b.Info()&types.IsString != 0 || b.Info()&types.IsInteger != 0 && b.Kind() != types.Uintptr)
}

func modelMutatorInput(t types.Type) bool {
	if _, ok := t.Underlying().(*types.Interface); ok {
		return false
	}
	n, ok := types.Unalias(t).(*types.Named)
	return !ok || !isNamed(n, framework+"/value", "Nullable")
}

// validateModel mirrors decidable declaration rules without calling user code.
func validateModel(p *packageInput, d *modelDeclaration) error {
	fail := func(f modelField, message string) error {
		return modelError(p, f.field.Pos(), d.name, f.field.Name(), message)
	}
	fields := make(map[string]modelField)
	columns := make(map[string]string)
	roles := make(map[string]modelField)
	var key modelField
	for _, f := range d.fields {
		if f.slot {
			continue
		}
		fields[f.field.Name()] = f
		name := f.tag.Column
		if name == "" {
			name = modelmeta.Snake(f.field.Name())
		}
		if !sqlname.Valid(name) {
			return fail(f, "invalid inferred SQL column name; use column=<name>")
		}
		if previous, duplicate := columns[name]; duplicate {
			return fail(f, "column duplicates "+previous+"; give each persisted field a distinct column name")
		}
		columns[name] = f.field.Name()
		if f.key {
			key = f
		}
		switch f.carrier {
		case "CreatedAt", "UpdatedAt", "SoftDelete":
			if previous, exists := roles[f.carrier]; exists {
				return fail(f, "timestamp role is already declared by "+previous.field.Name())
			}
			roles[f.carrier] = f
		}
	}
	if len(columns) > querycore.MaxExpressionNodes {
		return modelError(p, d.typ.Obj().Pos(), d.name, "", "persisted column count exceeds ORM declaration limit")
	}
	if err := validateModelTimestamps(p, d, fields, roles); err != nil {
		return err
	}
	for _, f := range d.fields {
		if f.carrier == "Encrypted" || f.carrier == "NullableEncrypted" {
			if f.tag.DatabaseDefault {
				return fail(f, "encrypted fields cannot have database defaults")
			}
			if key.tag.DatabaseDefault {
				return fail(f, "encrypted fields require an application-assigned primary key")
			}
		}
	}
	for _, name := range sortedNames(p.methods[d.name]) {
		prefix := modelmeta.FieldMethodPrefix(name)
		if prefix == "" {
			continue
		}
		field := strings.TrimPrefix(name, prefix)
		f, exists := fields[field]
		if !exists {
			return modelError(p, p.methods[d.name][name], d.name, field, "model field method "+name+" targets no persisted field")
		}
		obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(d.typ), true, p.types, name)
		fn, ok := obj.(*types.Func)
		if !ok {
			return fail(f, "model field method "+name+" is unavailable")
		}
		sig := fn.Type().(*types.Signature)
		if _, pointer := sig.Recv().Type().(*types.Pointer); pointer {
			return fail(f, "model field method "+name+" requires a value receiver")
		}
		if prefix == "Access" {
			valid := !sig.Variadic() && sig.Params().Len() == 0 && sig.Results().Len() == 2
			if valid {
				_, iface := sig.Results().At(0).Type().Underlying().(*types.Interface)
				valid = !iface && types.Identical(sig.Results().At(1).Type(), types.Universe.Lookup("error").Type())
			}
			if !valid {
				return fail(f, "model accessor "+name+" requires signature () (Result, error) with a concrete result")
			}
		}
	}
	return nil
}

func modelTableMember(name string) bool {
	if name == "Table" {
		return true
	}
	_, ok := reflect.TypeFor[orm.Table[struct{}, int64]]().MethodByName(name)
	return ok
}
