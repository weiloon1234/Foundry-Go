package generate

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// TransportDeclaration exposes existing directive metadata to read-only source
// tools and the external migration. It does not create a second wire schema.
type TransportDeclaration struct {
	Key, Name, Kind, Role, HTTP string
	Position, Directive         token.Position
	Comment                     string
	Generic                     bool
}

func TransportDeclarations(info *types.Info, fset *token.FileSet, file *ast.File) []TransportDeclaration {
	path := ""
	for _, object := range info.Defs {
		if object != nil && object.Pkg() != nil {
			path = object.Pkg().Path()
			break
		}
	}
	return transportDeclarations(path, fset, file)
}

func transportDeclarations(path string, fset *token.FileSet, file *ast.File) []TransportDeclaration {
	var result []TransportDeclaration
	if path == "" {
		return nil
	}
	if ast.IsGenerated(file) {
		return result
	}
	for _, node := range file.Decls {
		gen, ok := node.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, item := range gen.Specs {
			spec := item.(*ast.TypeSpec)
			group := spec.Doc
			if group == nil {
				group = gen.Doc
			}
			kind, args, err := directive(group)
			if err != nil || kind == "" {
				continue
			}
			d := TransportDeclaration{Key: path + "." + spec.Name.Name, Name: spec.Name.Name, Kind: kind, Role: args["role"], HTTP: args["http"], Position: fset.Position(spec.Pos()), Generic: spec.TypeParams != nil}
			for _, comment := range group.List {
				if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(comment.Text, "//")), "foundry:") {
					d.Directive = fset.Position(comment.Pos())
					d.Comment = comment.Text
				}
			}
			result = append(result, d)
		}
	}
	return result
}
