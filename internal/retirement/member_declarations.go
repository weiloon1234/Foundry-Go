package retirement

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// Duplicate declarations in mutually exclusive files need not survive type
// checking. Inspect every declaration against the ledger before resolving types.
func checkMemberDeclarations(fset *token.FileSet, sources map[string]*sourcePackage, denied map[string][]string) error {
	var failures []error
	for _, key := range sortedKeys(sources) {
		source := sources[key]
		aliases := make(map[string][]string)
		for _, file := range source.files {
			for _, decl := range file.Decls {
				group, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, spec := range group.Specs {
					typ, ok := spec.(*ast.TypeSpec)
					if !ok || !typ.Assign.IsValid() {
						continue
					}
					if target := localTypeName(typ.Type); target != "" {
						// Aliases preserve identity in either direction. Keep all
						// edges, including competing declarations in other files.
						aliases[typ.Name.Name] = append(aliases[typ.Name.Name], target)
						aliases[target] = append(aliases[target], typ.Name.Name)
					}
				}
			}
		}
		for _, selector := range denied[strings.TrimPrefix(source.owner, framework)] {
			owner, member, ok := strings.Cut(selector, ".")
			if !ok {
				continue
			}
			names := make(map[string]bool)
			pending := []string{owner}
			for len(pending) != 0 {
				name := pending[len(pending)-1]
				pending = pending[:len(pending)-1]
				if !names[name] {
					names[name] = true
					pending = append(pending, aliases[name]...)
				}
			}
			reject := func(pos token.Pos) {
				failures = append(failures, fmt.Errorf("%s: retired public selector %s.%s (declaration independent of build selection)", fset.Position(pos), source.owner, selector))
			}
			for _, file := range source.files {
				for _, decl := range file.Decls {
					switch decl := decl.(type) {
					case *ast.FuncDecl:
						if decl.Recv != nil && names[localTypeName(decl.Recv.List[0].Type)] && decl.Name.Name == member {
							reject(decl.Name.Pos())
						}
					case *ast.GenDecl:
						for _, spec := range decl.Specs {
							typ, ok := spec.(*ast.TypeSpec)
							if !ok || !names[typ.Name.Name] {
								continue
							}
							var fields *ast.FieldList
							var anonymousFields bool
							switch body := ast.Unparen(typ.Type).(type) {
							case *ast.StructType:
								fields, anonymousFields = body.Fields, true
							case *ast.InterfaceType:
								fields = body.Methods
							}
							if fields == nil {
								continue
							}
							for _, field := range fields.List {
								for _, name := range field.Names {
									if name.Name == member {
										reject(name.Pos())
									}
								}
								if anonymousFields && len(field.Names) == 0 {
									if name := embeddedName(field.Type); name != nil && name.Name == member {
										reject(name.Pos())
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return errors.Join(failures...)
}

func localTypeName(expr ast.Expr) string {
	switch expr := ast.Unparen(expr).(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return localTypeName(expr.X)
	case *ast.IndexExpr:
		return localTypeName(expr.X)
	case *ast.IndexListExpr:
		return localTypeName(expr.X)
	}
	return ""
}
