package generate

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

type dtoValidationField struct {
	name, wire, typ, selection string
	guards                     []string
}

// emitDTOValidation uses the properties already resolved for the DTO schema.
// Business rules stay ordinary Go functions with explicit dependencies; field
// selectors and wire names are generated automatically from the same source.
func (e *emitter) emitDTOValidation(declaration dtoDeclaration) {
	e.emitTransportValidation(declaration.name, declaration.typ, declaration.position, declaration.properties, "JSON", "property")
}

// emitTransportValidation derives typed fields from already discovered transport
// metadata, preserving one selector/naming implementation for DTOs and forms.
func (e *emitter) emitTransportValidation(name string, root types.Type, position token.Position, properties []jsonshape.Property[types.Type], wireLabel, fieldLabel string) {
	validation := ""
	if len(properties) != 0 {
		validation = e.useNamed(framework+"/validation", "foundryvalidation")
	}
	counts := make(map[string]int)
	for _, property := range properties {
		counts[property.GoName]++
	}
	fields := make([]dtoValidationField, 0, len(properties))
	for _, property := range properties {
		selection, guards, names, err := dtoFieldAccess(root, property.Index, e.pkg.types)
		if err != nil {
			e.err = fmt.Errorf("%s: %w", position, err)
			return
		}
		name := property.GoName
		if counts[name] > 1 || !token.IsExported(name) || strings.HasPrefix(name, "Field_") {
			for i := range names {
				names[i] = jsonPathName(names[i])
			}
			name = "Field_" + strings.Join(names, "_")
		}
		typ := e.typeName(property.Type)
		if len(guards) != 0 {
			typ = e.use(framework+"/value") + ".Optional[" + typ + "]"
		}
		fields = append(fields, dtoValidationField{name: name, wire: property.Name, typ: typ, selection: selection, guards: guards})
	}
	input := e.localName("input")
	params, args := e.genericDeclaration(root)
	rootName := name + args
	e.line("// %sValidationFieldSet retains concrete DTO and value types for validation.", name)
	e.line("type %sValidationFieldSet%s struct {", name, params)
	for _, field := range fields {
		e.line("// %s validates the declared %s %s %q.", field.name, wireLabel, fieldLabel, field.wire)
		if len(field.guards) != 0 {
			e.line("// Optional preserves absence through a nil embedded pointer.")
		}
		e.line("%s %s.Field[%s,%s]", field.name, validation, rootName, field.typ)
	}
	e.line("}")
	e.line("// %sValidationFields returns independent typed field descriptors.", name)
	e.line("// Compose rules with these fields; %s names and selectors are generated.", wireLabel)
	e.line("func %sValidationFields%s()%sValidationFieldSet%s{return %sValidationFieldSet%s{", name, params, name, args, name, args)
	for _, field := range fields {
		e.line("%s:%s.DefineField(%q,func(%s %s)%s{", field.name, validation, field.wire, input, rootName, field.typ)
		for _, guard := range field.guards {
			e.line("if %s.%s==nil{return %s{}}", input, guard, field.typ)
		}
		if len(field.guards) != 0 {
			e.line("return %s.Set(%s.%s)", e.use(framework+"/value"), input, field.selection)
		} else {
			e.line("return %s.%s", input, field.selection)
		}
		e.line("}),")
	}
	e.line("}}")
}

// dtoFieldAccess follows jsonshape's winning field index, not a guessed promoted
// selector. A hidden or ignored Go field can otherwise shadow the JSON winner.
func dtoFieldAccess(root types.Type, index []int, from *types.Package) (string, []string, []string, error) {
	var guards, names []string
	current := root
	for i, position := range index {
		if pointer, ok := types.Unalias(current).(*types.Pointer); ok {
			guard, err := dtoAccessibleSelection(root, index[:i], from)
			if err != nil {
				return "", nil, nil, err
			}
			guards = append(guards, guard)
			current = pointer.Elem()
		}
		structure, ok := types.Unalias(current).Underlying().(*types.Struct)
		if !ok || position < 0 || position >= structure.NumFields() {
			return "", nil, nil, fmt.Errorf("invalid DTO validation field index")
		}
		field := structure.Field(position)
		names = append(names, field.Name())
		current = field.Type()
	}
	selection, err := dtoAccessibleSelection(root, index, from)
	return selection, guards, names, err
}

func dtoAccessibleSelection(root types.Type, index []int, from *types.Package) (string, error) {
	var names []string
	accessible := true
	current := root
	for _, position := range index {
		if pointer, ok := types.Unalias(current).(*types.Pointer); ok {
			current = pointer.Elem()
		}
		structure, ok := types.Unalias(current).Underlying().(*types.Struct)
		if !ok || position < 0 || position >= structure.NumFields() {
			return "", fmt.Errorf("invalid DTO validation field index")
		}
		field := structure.Field(position)
		names = append(names, field.Name())
		accessible = accessible && (field.Exported() || field.Pkg() == from)
		current = field.Type()
	}
	if len(names) == 0 {
		return "", fmt.Errorf("missing DTO validation field selector")
	}
	last := names[len(names)-1]
	object, promoted, _ := types.LookupFieldOrMethod(root, true, from, last)
	if _, ok := object.(*types.Var); ok && slices.Equal(promoted, index) {
		return last, nil
	}
	if accessible {
		return strings.Join(names, "."), nil
	}
	return "", fmt.Errorf("DTO validation property %s has no accessible Go selector; declare the request field explicitly", last)
}
