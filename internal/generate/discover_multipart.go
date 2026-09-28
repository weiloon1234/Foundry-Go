package generate

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

type multipartDeclaration struct {
	name     string
	typ      *types.Named
	position token.Position
	fields   []multipartField
}

type multipartField struct {
	transportField
	kind, binding string
	element       types.Type
	query         queryField
	graph         dtoGraph
}

func discoverMultipart(p *packageInput, spec *ast.TypeSpec, named *types.Named, args map[string]string) (multipartDeclaration, error) {
	declaration := multipartDeclaration{name: spec.Name.Name, typ: named, position: p.fset.Position(spec.Pos())}
	if len(args) != 0 {
		return declaration, p.diagnostic(spec.Pos(), "multipart declarations do not accept options")
	}
	structure, ok := named.Underlying().(*types.Struct)
	if !ok {
		return declaration, p.diagnostic(spec.Pos(), "multipart declaration must be a struct")
	}
	fields, err := discoverTransportFields(p, structure, "form", map[string]bool{"json": true, "repeat": true}, []string{"foundry", "query", "path", "json"})
	if err != nil {
		return declaration, err
	}
	for _, field := range fields {
		if field.options["repeat"] && !field.options["json"] {
			return declaration, p.diagnostic(field.position, "form repeat requires an explicit JSON part; text and file slices already repeat")
		}
		declaration.fields = append(declaration.fields, multipartField{transportField: field})
	}
	return declaration, nil
}

func resolveMultipartFields(p *packageInput, declaration *multipartDeclaration, models map[*types.Named]bool) error {
	for i := range declaration.fields {
		field := &declaration.fields[i]
		element := field.typ
		optional := false
		if inner, ok := jsonWrapper(element, "Optional"); ok {
			element, optional = inner, true
		}
		if field.options["json"] {
			field.kind, field.binding = "json", "JSONPart"
			if optional {
				field.binding = "OptionalJSONPart"
			}
			if field.options["repeat"] {
				sequence, ok := types.Unalias(element).Underlying().(*types.Slice)
				if !ok || optional {
					return p.diagnostic(field.position, "repeated JSON parts require an ordinary or named slice without Optional")
				}
				element, field.binding = sequence.Elem(), "RepeatedJSONPart"
			}
			if _, nested := jsonWrapper(element, "Optional"); nested {
				return p.diagnostic(field.position, "nested Optional cannot describe one multipart JSON value")
			}
			field.element = element
			if err := resolveJSONGraph(p, &field.graph, element, field.position, models, true); err != nil {
				return err
			}
			continue
		}
		repeated := false
		if sequence, ok := types.Unalias(element).Underlying().(*types.Slice); ok && multipartFileType(sequence.Elem()) {
			if optional {
				return p.diagnostic(field.position, "optional file collections are ambiguous; use a slice for repeated file parts")
			}
			element, repeated = sequence.Elem(), true
		}
		if multipartFileType(element) {
			field.kind, field.binding, field.element = "file", "FilePart", element
			if optional {
				field.binding = "OptionalFilePart"
			}
			if repeated {
				field.binding = "RepeatedFilePart"
			}
			continue
		}
		field.kind = "text"
		field.query = queryField{transportField: field.transportField}
		if err := resolveQueryField(p, &field.query, "multipart"); err != nil {
			return err
		}
		field.binding, field.element = field.query.binding, field.query.element
	}
	return nil
}

func multipartFileType(typ types.Type) bool {
	named, ok := types.Unalias(typ).(*types.Named)
	return ok && isNamed(named, framework+"/internal/upload", "File")
}

func (d multipartDeclaration) validationProperties() []jsonshape.Property[types.Type] {
	fields := make([]jsonshape.Property[types.Type], len(d.fields))
	for i, field := range d.fields {
		fields[i] = jsonshape.Property[types.Type]{GoName: field.name, Name: field.parameter, Type: field.typ, Index: []int{field.index}}
	}
	return fields
}
