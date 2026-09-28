package generate

import (
	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
	"go/ast"
	"go/token"
	"go/types"
)

type queryDeclaration struct {
	name     string
	source   string
	typ      *types.Named
	fields   []queryField
	position token.Position
}

type queryField struct {
	transportField
	binding string
	element types.Type
	scalar  urlScalar
}

func discoverQuery(p *packageInput, spec *ast.TypeSpec, named *types.Named, args map[string]string, source string) (queryDeclaration, error) {
	declaration := queryDeclaration{name: spec.Name.Name, source: source, typ: named, position: p.fset.Position(spec.Pos())}
	if len(args) != 0 {
		return declaration, p.diagnostic(spec.Pos(), source+" declarations do not accept options")
	}
	structure, ok := named.Underlying().(*types.Struct)
	if !ok {
		return declaration, p.diagnostic(spec.Pos(), source+" declaration must be a struct")
	}
	fields, err := discoverTransportFields(p, structure, source, nil, []string{"foundry", "path"})
	if err != nil {
		return declaration, err
	}
	for _, field := range fields {
		declaration.fields = append(declaration.fields, queryField{transportField: field})
	}
	return declaration, nil
}

func resolveQueryCodecs(p *packageInput, declaration *queryDeclaration) error {
	for i := range declaration.fields {
		if err := resolveQueryField(p, &declaration.fields[i], declaration.source); err != nil {
			return err
		}
	}
	return nil
}

// resolveQueryField shares scalar, omission, repetition and custom codec
// precedence between URL queries and multipart text parts.
func resolveQueryField(p *packageInput, field *queryField, source string) error {
	if _, pointer := types.Unalias(field.typ).(*types.Pointer); pointer {
		return p.diagnostic(field.position, source+" fields require concrete values, not pointers")
	}
	field.binding, field.element = "QueryParam", field.typ
	if inner, optional := jsonWrapper(field.typ, "Optional"); optional {
		field.binding, field.element = "OptionalQueryParam", inner
	}
	base := types.Unalias(field.element)
	if _, nullable := jsonWrapper(base, "Nullable"); nullable {
		return p.diagnostic(field.position, source+" text fields have no implicit null representation; use Optional for omission")
	}
	if sequence, ok := base.Underlying().(*types.Slice); ok {
		marshal, unmarshal, invalid := urlTextMethods(base)
		// A declared slice's own text protocol takes precedence over list
		// inference. Partial/invalid codecs fail in the shared resolver.
		if !marshal && !unmarshal && !invalid {
			if field.binding == "OptionalQueryParam" {
				return p.diagnostic(field.position, "optional "+source+" text fields require one scalar; use a slice directly for repeated parts")
			}
			field.binding, field.element = "RepeatedQueryParam", sequence.Elem()
		}
	}
	scalar, err := resolveURLScalar(p, field.element, field.position, source)
	if err != nil {
		return err
	}
	field.scalar = scalar
	return nil
}

func (d queryDeclaration) validationProperties() []jsonshape.Property[types.Type] {
	fields := make([]jsonshape.Property[types.Type], len(d.fields))
	for i, field := range d.fields {
		fields[i] = jsonshape.Property[types.Type]{GoName: field.name, Name: field.parameter, Type: field.typ, Index: []int{field.index}}
	}
	return fields
}
