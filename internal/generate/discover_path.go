package generate

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/httppath"
)

type pathDeclaration struct {
	name, pattern string
	fields        []pathField
	position      token.Position
}

type pathField struct {
	name, parameter, codec string
	typ                    types.Type
	owner                  types.Type
	position               token.Pos
	presentation           contract.Presentation
}

func discoverPath(p *packageInput, spec *ast.TypeSpec, named *types.Named, args map[string]string) (pathDeclaration, error) {
	declaration := pathDeclaration{name: spec.Name.Name, pattern: args["pattern"], position: p.fset.Position(spec.Pos())}
	for option := range args {
		if option != "pattern" {
			return declaration, p.diagnostic(spec.Pos(), "unsupported path option "+option)
		}
	}
	segments, err := httppath.Parse(declaration.pattern)
	if err != nil {
		return declaration, p.diagnostic(spec.Pos(), err.Error())
	}
	structure, ok := named.Underlying().(*types.Struct)
	if !ok {
		return declaration, p.diagnostic(spec.Pos(), "path declaration must be a struct")
	}
	fields := make(map[string]pathField)
	for i := 0; i < structure.NumFields(); i++ {
		field := structure.Field(i)
		tags, err := parseTags(structure.Tag(i))
		if err != nil {
			return declaration, p.diagnostic(field.Pos(), err.Error())
		}
		if _, exists := tags["foundry"]; exists {
			return declaration, p.diagnostic(field.Pos(), "path fields do not declare persistence tags")
		}
		parameter, explicit := tags["path"]
		if parameter == "-" {
			continue
		}
		if field.Embedded() || !field.Exported() {
			return declaration, p.diagnostic(field.Pos(), "path fields must be exported and non-embedded; use path:\"-\" to skip")
		}
		if !explicit {
			parameter = snake(field.Name())
		}
		if parameter == "" {
			return declaration, p.diagnostic(field.Pos(), "path tag requires a parameter name")
		}
		if _, exists := fields[parameter]; exists {
			return declaration, p.diagnostic(field.Pos(), "multiple fields bind the same path parameter")
		}
		presentation, err := parsePresentation(tags["client"])
		if err != nil {
			return declaration, p.diagnostic(field.Pos(), err.Error())
		}
		fields[parameter] = pathField{name: field.Name(), parameter: parameter, typ: field.Type(), position: field.Pos(), presentation: presentation}
	}
	// Emit in pattern order, independently of struct declaration order. Every
	// binding must be consumed once by the same grammar the runtime uses.
	for _, segment := range segments {
		if segment.Name == "" {
			continue
		}
		field, exists := fields[segment.Name]
		if !exists {
			return declaration, p.diagnostic(spec.Pos(), "path parameter has no matching field: "+segment.Name)
		}
		declaration.fields = append(declaration.fields, field)
		delete(fields, segment.Name)
	}
	if len(fields) != 0 {
		return declaration, p.diagnostic(spec.Pos(), "path field does not occur in the pattern")
	}
	return declaration, nil
}

func resolvePathCodecs(p *packageInput, declaration *pathDeclaration) error {
	for i := range declaration.fields {
		field := &declaration.fields[i]
		scalar, err := resolveURLScalar(p, field.typ, field.position, "path")
		if err != nil {
			return err
		}
		field.codec, field.owner = scalar.kind, scalar.owner
	}
	return nil
}
