package main

import (
	"go/ast"
	"go/token"
	"strings"
)

// migrateTerminals follows explicit constructor and local declaration types.
// Unknown expressions are left for source review; a name alone never selects a
// method rewrite. Each function has its own environment.
func migrateTerminals(file *ast.File, local string, aliases map[string]string, use func(string) string) bool {
	changed := false
	symbol := func(e ast.Expr) (string, string) {
		for {
			switch v := e.(type) {
			case *ast.IndexExpr:
				e = v.X
			case *ast.IndexListExpr:
				e = v.X
			default:
				goto done
			}
		}
	done:
		switch v := e.(type) {
		case *ast.Ident:
			return local, v.Name
		case *ast.SelectorExpr:
			if p, ok := v.X.(*ast.Ident); ok {
				return aliases[p.Name], v.Sel.Name
			}
		}
		return "", ""
	}
	typeKind := func(e ast.Expr) string {
		pkg, name := symbol(e)
		if pkg == framework+"database/orm/expr" && name == "JSONPath" {
			return "path"
		}
		if pkg == framework+"database/orm/rows" {
			switch name {
			case "RecordRead":
				return "ReadRecord"
			case "SetQuery":
				return "ReadSet"
			case "ValueSetQuery":
				return "ReadValues"
			case "ValueCursorQuery":
				return "cursor"
			}
		}
		return ""
	}
	results := map[string]string{}
	for _, decl := range file.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Type.Results != nil && len(f.Type.Results.List) > 0 {
			results[f.Name.Name] = typeKind(f.Type.Results.List[0].Type)
		}
	}
	for _, decl := range file.Decls {
		f, ok := decl.(*ast.FuncDecl)
		if !ok || f.Body == nil {
			continue
		}
		vars := map[string]string{}
		if f.Type.Params != nil {
			for _, field := range f.Type.Params.List {
				for _, name := range field.Names {
					vars[name.Name] = typeKind(field.Type)
				}
			}
		}
		var kind func(ast.Expr) string
		kind = func(e ast.Expr) string {
			switch e := e.(type) {
			case *ast.ParenExpr:
				return kind(e.X)
			case *ast.Ident:
				return vars[e.Name]
			case *ast.CompositeLit:
				return typeKind(e.Type)
			case *ast.CallExpr:
				pkg, name := symbol(e.Fun)
				if pkg == framework+"database/orm/rows" {
					switch name {
					case "SelectRecord", "WithValue", "DistinctRecord", "DistinctOnRecord":
						return "ReadRecord"
					case "Union", "UnionAll", "Intersect", "IntersectAll", "Except", "ExceptAll":
						return "ReadSet"
					case "UnionValues", "UnionAllValues", "IntersectValues", "IntersectAllValues", "ExceptValues", "ExceptAllValues", "DistinctValues", "DistinctOnValueSet":
						return "ReadValues"
					case "ValueCursorFor":
						return "cursor"
					}
				}
				if pkg == framework+"database/orm/expr" {
					switch name {
					case "JSONRoot", "JSONNullableRoot", "JSONProperty", "JSONIndex", "JSONMapKey", "JSONOptional", "JSONNullable":
						return "path"
					}
				}
				if id, ok := e.Fun.(*ast.Ident); ok {
					if k := results[id.Name]; k != "" {
						return k
					}
				}
				if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
					switch sel.Sel.Name {
					case "Where", "OrderBy", "Limit", "Offset", "UniqueBy":
						return kind(sel.X)
					}
				}
			}
			return ""
		}
		qualified := func(pkg, name string) ast.Expr {
			if local == pkg {
				return ast.NewIdent(name)
			}
			return &ast.SelectorExpr{X: ast.NewIdent(use(pkg)), Sel: ast.NewIdent(name)}
		}
		method := func(x ast.Expr, name string) ast.Expr {
			return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: ast.NewIdent(name)}}
		}
		ast.Inspect(f.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for i, lhs := range n.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && i < len(n.Rhs) {
						vars[id.Name] = kind(n.Rhs[i])
					}
				}
			case *ast.ValueSpec:
				for i, id := range n.Names {
					vars[id.Name] = typeKind(n.Type)
					if i < len(n.Values) {
						vars[id.Name] = kind(n.Values[i])
					}
				}
			case *ast.RangeStmt:
				if array, ok := n.X.(*ast.CompositeLit); ok {
					if typ, ok := array.Type.(*ast.ArrayType); ok {
						if id, ok := n.Value.(*ast.Ident); ok {
							vars[id.Name] = typeKind(typ.Elt)
						}
					}
				}
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				k := kind(sel.X)
				if sel.Sel.Name == "All" && strings.HasPrefix(k, "Read") {
					sel.X = &ast.CallExpr{Fun: qualified(framework+"database/orm/rows", k), Args: []ast.Expr{sel.X}}
					changed = true
				}
				if k == "cursor" && sel.Sel.Name == "Key" && len(n.Args) == 0 {
					*n = ast.CallExpr{Fun: qualified(framework+"database/orm/expr", "Group"), Args: []ast.Expr{method(sel.X, "Value")}}
					changed = true
				}
				if k == "path" && len(n.Args) == 0 {
					root := method(sel.X, "JSON")
					switch sel.Sel.Name {
					case "Kind", "Length":
						operation := "JSONTypeNullable"
						if sel.Sel.Name == "Length" {
							operation = "JSONLengthNullable"
						}
						*n = ast.CallExpr{Fun: qualified(framework+"database/orm/expr", operation), Args: []ast.Expr{root}}
						changed = true
					case "Exists", "IsMissing":
						operation := "IsNotNull"
						if sel.Sel.Name == "IsMissing" {
							operation = "IsNull"
						}
						*n = *(method(root, operation).(*ast.CallExpr))
						changed = true
					case "IsJSONNull":
						jsonType := &ast.CallExpr{Fun: qualified(framework+"database/orm/expr", "JSONTypeNullable"), Args: []ast.Expr{root}}
						*n = ast.CallExpr{Fun: &ast.SelectorExpr{X: jsonType, Sel: ast.NewIdent("Eq")}, Args: []ast.Expr{&ast.CallExpr{Fun: qualified(framework+"value", "Of"), Args: []ast.Expr{qualified(framework+"database", "JSONNull")}}}}
						changed = true
					}
				}
			}
			return true
		})
	}
	// A retained import may become unused after an alias-only conversion. Remove
	// only imports with no identifier references; blank/dot imports keep semantics.
	used := map[string]bool{}
	for _, decl := range file.Decls {
		if g, ok := decl.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			continue
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				used[id.Name] = true
			}
			return true
		})
	}
	var decls []ast.Decl
	for _, decl := range file.Decls {
		if g, ok := decl.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			var specs []ast.Spec
			for _, spec := range g.Specs {
				imp := spec.(*ast.ImportSpec)
				path := strings.Trim(imp.Path.Value, "\"")
				name := path[strings.LastIndex(path, "/")+1:]
				if imp.Name != nil {
					name = imp.Name.Name
				}
				if name == "_" || name == "." || used[name] || !knownImport(path) {
					specs = append(specs, spec)
				} else {
					changed = true
				}
			}
			g.Specs = specs
			if len(specs) == 0 {
				continue
			}
		}
		decls = append(decls, decl)
	}
	file.Decls = decls
	return changed
}

// Import paths need not end with their declared package name. Only prune the
// packages this migration knows, never infer names for third-party packages.
func knownImport(path string) bool {
	switch path {
	case "context", "errors", "fmt", "reflect", "strings", "time", framework + "database", framework + "database/codec", framework + "database/orm", framework + "database/orm/expr", framework + "database/orm/rows", framework + "database/query", framework + "fault", framework + "model", framework + "value":
		return true
	}
	return false
}
