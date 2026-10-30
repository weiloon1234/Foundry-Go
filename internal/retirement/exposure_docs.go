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
	pkg, file, name string
	line            int
}

func identLocation(fset *token.FileSet, owner string, id *ast.Ident) declarationLocation {
	p := fset.Position(id.Pos())
	return declarationLocation{pkg: owner, file: filepath.Base(p.Filename), name: id.Name, line: p.Line}
}

func objectLocation(fset *token.FileSet, obj types.Object) (declarationLocation, bool) {
	pkg := obj.Pkg()
	if pkg == nil {
		return declarationLocation{}, false
	}
	p := fset.Position(obj.Pos())
	return declarationLocation{pkg: pkg.Path(), file: filepath.Base(p.Filename), name: obj.Name(), line: p.Line}, true
}

func declarationDocs(fset *token.FileSet, sources map[string]*sourcePackage) map[declarationLocation]bool {
	docs := make(map[declarationLocation]bool)
	for _, source := range sources {
		mark := func(id *ast.Ident, comments ...*ast.CommentGroup) {
			if id == nil {
				return
			}
			for _, group := range comments {
				if group != nil && strings.Contains(group.Text(), "Deprecated:") {
					docs[identLocation(fset, source.owner, id)] = true
				}
			}
		}
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
