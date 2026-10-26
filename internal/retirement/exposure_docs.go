package retirement

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
)

// Imported export data and locally checked syntax have different token.Pos
// values. Declaration locations join both to the original source comments.
type declarationLocation struct {
	file, name string
	line       int
}

func declarationAt(fset *token.FileSet, pos token.Pos, name string) declarationLocation {
	p := fset.Position(pos)
	file, _ := filepath.Abs(p.Filename)
	return declarationLocation{file: file, name: name, line: p.Line}
}

func declarationDocs(fset *token.FileSet, sources map[string]*sourcePackage) map[declarationLocation]bool {
	docs := make(map[declarationLocation]bool)
	mark := func(id *ast.Ident, comments ...*ast.CommentGroup) {
		if id == nil {
			return
		}
		for _, group := range comments {
			if group != nil && strings.Contains(group.Text(), "Deprecated:") {
				docs[declarationAt(fset, id.Pos(), id.Name)] = true
			}
		}
	}
	for _, source := range sources {
		for _, file := range source.files {
			ast.Inspect(file, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.FuncDecl:
					mark(n.Name, n.Doc)
				case *ast.GenDecl:
					for _, spec := range n.Specs {
						switch spec := spec.(type) {
						case *ast.TypeSpec:
							mark(spec.Name, n.Doc, spec.Doc, spec.Comment)
						case *ast.ValueSpec:
							for _, name := range spec.Names {
								mark(name, n.Doc, spec.Doc, spec.Comment)
							}
						}
					}
				case *ast.Field:
					for _, name := range n.Names {
						mark(name, n.Doc, n.Comment)
					}
					if len(n.Names) == 0 {
						mark(embeddedName(n.Type), n.Doc, n.Comment)
					}
				}
				return true
			})
		}
	}
	return docs
}

func embeddedName(expr ast.Expr) *ast.Ident {
	switch e := expr.(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.StarExpr:
		return embeddedName(e.X)
	case *ast.ParenExpr:
		return embeddedName(e.X)
	case *ast.IndexExpr:
		return embeddedName(e.X)
	case *ast.IndexListExpr:
		return embeddedName(e.X)
	}
	return nil
}

// An anonymous interface embedding has no field object in go/types. Attach its
// annotation to the interface surface that includes it, including inline types.
func interfaceDocs(files []*ast.File, info *types.Info) map[types.Type][]token.Pos {
	docs := make(map[types.Type][]token.Pos)
	if len(info.Types) == 0 {
		return docs
	}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			iface, ok := node.(*ast.InterfaceType)
			if !ok {
				return true
			}
			for _, field := range iface.Methods.List {
				if len(field.Names) != 0 {
					continue
				}
				for _, group := range []*ast.CommentGroup{field.Doc, field.Comment} {
					if group != nil && strings.Contains(group.Text(), "Deprecated:") {
						if typ := info.TypeOf(iface); typ != nil {
							docs[typ.Underlying()] = append(docs[typ.Underlying()], field.Pos())
						}
					}
				}
			}
			return true
		})
	}
	return docs
}
