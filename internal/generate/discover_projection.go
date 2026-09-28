package generate

import (
	"go/ast"
	"go/token"
	"go/types"
)

type projection struct {
	name     string
	fields   []field
	position token.Position
	dto      bool
}

func discoverProjection(p *packageInput, spec *ast.TypeSpec, named *types.Named, args map[string]string) (projection, error) {
	result := projection{name: spec.Name.Name, position: p.fset.Position(spec.Pos())}
	if len(args) != 0 && (len(args) != 1 || args["dto"] != "true") {
		return result, p.diagnostic(spec.Pos(), "projection declarations do not declare a table or primary key; the only optional setting is dto=true")
	}
	result.dto = args["dto"] == "true"
	structure, ok := named.Underlying().(*types.Struct)
	if !ok || structure.NumFields() == 0 {
		return result, p.diagnostic(spec.Pos(), "projection requires a non-empty result struct")
	}
	columns := make(map[string]bool)
	for i := 0; i < structure.NumFields(); i++ {
		v := structure.Field(i)
		if !v.Exported() || v.Embedded() {
			return result, p.diagnostic(v.Pos(), "projection fields must be exported and non-embedded")
		}
		tag, err := fieldTag(structure.Tag(i))
		if err != nil {
			return result, p.diagnostic(v.Pos(), err.Error())
		}
		if tag["-"] != "" || tag["default"] != "" {
			return result, p.diagnostic(v.Pos(), "projection fields cannot be ignored or declare database defaults")
		}
		f, err := discoverValueField(p, v, tag)
		if err != nil {
			return result, err
		}
		if columns[f.column] {
			return result, p.diagnostic(v.Pos(), "duplicate projection column "+f.column)
		}
		columns[f.column] = true
		result.fields = append(result.fields, f)
	}
	return result, nil
}
