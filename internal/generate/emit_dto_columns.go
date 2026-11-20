package generate

import (
	"fmt"
	"go/types"
)

type dtoColumnField struct {
	name, wire, typ, selection string
	help                       fieldHelp
}

// emitDTOColumns emits typed row field bindings (wire name + struct pointer
// accessor) for //foundry:dto role=row declarations. It emits transport
// bindings only; no SQL, ORM, or persistence metadata is generated.
func (e *emitter) emitDTOColumns(declaration dtoDeclaration) {
	validation := ""
	if len(declaration.properties) != 0 {
		validation = e.useNamed(framework+"/validation", "foundryvalidation")
	}
	fields := make([]dtoColumnField, 0, len(declaration.properties))
	for i, property := range declaration.properties {
		// validateRowDTOProperties admits only direct fields: no nil guards apply.
		selection, _, _, err := dtoFieldAccess(declaration.typ, property.Index, e.pkg.types)
		if err != nil {
			e.err = fmt.Errorf("%s: %w", declaration.position, err)
			return
		}
		var help fieldHelp
		if i < len(declaration.fieldHelp) {
			help = declaration.fieldHelp[i]
		}
		fields = append(fields, dtoColumnField{
			name:      property.GoName,
			wire:      property.Name,
			typ:       e.typeName(property.Type),
			selection: selection,
			help:      help,
		})
	}
	input := e.localName("input")
	columnsFn, setName, cached := columnDescriptorNames(declaration.name)
	e.line("// %s retains concrete row and field types for datatable column bindings.", setName)
	e.line("// Example: %s()", columnsFn)
	e.line("type %s struct {", setName)
	for _, field := range fields {
		e.line("// %s binds the declared JSON property %q.", field.name, field.wire)
		e.emitFieldHelp(field.help)
		e.line("%s %s.Binding[%s,%s]", field.name, validation, declaration.name, field.typ)
	}
	e.line("}")
	e.line("// %s returns typed transport field bindings for %s.", columnsFn, declaration.name)
	e.line("// Example: %s()", columnsFn)
	e.line("func %s()%s{return *%s()}", columnsFn, setName, cached)
	e.line("var %s=%s.OnceValue(func()*%s{return &%s{", cached, e.use("sync"), setName, setName)
	for _, field := range fields {
		e.line("%s:%s.DefineBinding(%q,func(%s *%s)*%s{", field.name, validation, field.wire, input, declaration.name, field.typ)
		e.line("return &%s.%s", input, field.selection)
		e.line("}),")
	}
	e.line("}})")
}

func validateRowDTOProperties(p *packageInput, declaration *dtoDeclaration) error {
	for _, property := range declaration.properties {
		if len(property.Index) != 1 {
			return p.diagnostic(declaration.typ.Obj().Pos(), "row DTO fields must be direct exported struct fields, not embedded fields")
		}
	}
	structure, ok := declaration.typ.Underlying().(*types.Struct)
	if !ok {
		return p.diagnostic(declaration.typ.Obj().Pos(), "DTO declaration must be a struct")
	}
	for i := 0; i < structure.NumFields(); i++ {
		f := structure.Field(i)
		if f.Embedded() {
			return p.diagnostic(f.Pos(), "row DTOs cannot embed structs; declare row fields directly")
		}
	}
	return nil
}
