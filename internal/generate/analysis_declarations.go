package generate

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
)

// ModelDeclaration is a //foundry:model struct of an analysed package. Directives
// that generation would reject are not returned: generation reports them.
type ModelDeclaration struct {
	Spec  *ast.TypeSpec
	Table string
}

// ModelDeclarations lists the model structs declared in files, in source order.
func ModelDeclarations(files []*ast.File) []ModelDeclaration {
	var result []ModelDeclaration
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				comments := typeSpec.Doc
				if comments == nil {
					comments = gen.Doc
				}
				kind, args, err := directive(comments)
				if err == nil && kind == "model" {
					result = append(result, ModelDeclaration{Spec: typeSpec, Table: args["table"]})
				}
			}
		}
	}
	return result
}

// EnumCase is one wire value of an enum type. Value is the string itself for a
// string enum and the decimal digits for an integer enum.
type EnumCase struct {
	Name    string
	Value   string
	Integer bool
	Pos     token.Pos
}

// EnumCases returns the cases of the enum type typeName declared in the package,
// in source order, by the rules generation uses. ok is false when the package
// declares no such enum or one of its constants cannot be classified.
func (p *AnalysisPackage) EnumCases(typeName string) (cases []EnumCase, ok bool) {
	object, isType := p.Types.Scope().Lookup(typeName).(*types.TypeName)
	if !isType || object.IsAlias() {
		return nil, false
	}
	named, isNamed := object.Type().(*types.Named)
	if !isNamed {
		return nil, false
	}
	base, isBasic := named.Underlying().(*types.Basic)
	if !isBasic || base.Kind() == types.Uintptr || (base.Info()&types.IsInteger == 0 && base.Kind() != types.String) {
		return nil, false
	}
	constants, unexported := enumCaseConstants(p.Types.Scope(), enumConstants(p.Syntax), named)
	if unexported != nil {
		return nil, false
	}
	for _, c := range constants {
		entry := EnumCase{Name: c.Name(), Pos: c.Pos(), Integer: base.Kind() != types.String}
		if entry.Integer {
			entry.Value = c.Val().ExactString()
		} else {
			entry.Value = constant.StringVal(c.Val())
		}
		cases = append(cases, entry)
	}
	sort.SliceStable(cases, func(i, j int) bool {
		a, b := p.Fset.Position(cases[i].Pos), p.Fset.Position(cases[j].Pos)
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Offset < b.Offset
	})
	return cases, len(cases) != 0
}
