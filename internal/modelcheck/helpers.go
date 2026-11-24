package modelcheck

import (
	"go/ast"
	"go/types"
)

// Imported functions and their source declarations have distinct go/types objects
// in independently loaded packages. Use their package and receiver identities.
type declarationFunction struct{ pkg, receiver, name string }

func declarationFunctionOf(fn *types.Func) declarationFunction {
	fn = fn.Origin()
	key := declarationFunction{name: fn.Name()}
	if fn.Pkg() != nil {
		key.pkg = fn.Pkg().Path()
	}
	if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
		typ := types.Unalias(recv.Type())
		if pointer, ok := typ.(*types.Pointer); ok {
			typ = types.Unalias(pointer.Elem())
		}
		if named, ok := typ.(*types.Named); ok {
			key.receiver = named.Origin().Obj().Name()
		}
	}
	return key
}

// declarationNameParameters follows one helper level: only a parameter
// passed directly as Define's name is interpreted at the helper's call sites.
func (a *Analysis) declarationNameParameters(syntax []*ast.File) map[declarationFunction][]int {
	result := make(map[declarationFunction][]int)
	for _, file := range syntax {
		if isTestFile(a.Files.Position(file.Pos()).Filename) {
			continue
		}
		for _, decl := range file.Decls {
			f, ok := decl.(*ast.FuncDecl)
			if !ok || f.Body == nil {
				continue
			}
			fn, ok := a.Info.Defs[f.Name].(*types.Func)
			if !ok {
				continue
			}
			sig := fn.Type().(*types.Signature)
			declarations := make(map[*ast.CallExpr][]int)
			ast.Inspect(f.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !isDeclarationFunction(a.Function(call.Fun)) || len(call.Args) == 0 {
					return true
				}
				declarations[call] = nil
				id, ok := unparen(call.Args[0]).(*ast.Ident)
				if !ok {
					return true
				}
				for i := 0; i < sig.Params().Len(); i++ {
					if a.Info.ObjectOf(id) == sig.Params().At(i) {
						declarations[call] = append(declarations[call], i)
					}
				}
				return true
			})
			if len(declarations) == 0 {
				continue
			}
			parameters, known := a.returnedNameParameters(f.Body, declarations)
			if !known {
				for _, indices := range declarations {
					parameters = append(parameters, indices...)
				}
			}
			if len(parameters) != 0 {
				result[declarationFunctionOf(fn)] = parameters
			}
		}
	}
	return result
}

// returnedNameParameters narrows the helper to tables it returns directly or
// through a uniquely assigned local. Unresolved returns keep all name parameters.
func (a *Analysis) returnedNameParameters(body *ast.BlockStmt, declarations map[*ast.CallExpr][]int) ([]int, bool) {
	var resolve func(ast.Expr, map[types.Object]bool) ([]int, bool)
	resolve = func(expr ast.Expr, seen map[types.Object]bool) ([]int, bool) {
		switch expr := unparen(expr).(type) {
		case *ast.CallExpr:
			parameters, ok := declarations[expr]
			return parameters, ok
		case *ast.Ident:
			object := a.Info.ObjectOf(expr)
			values := a.aliases[object]
			if object == nil || seen[object] || len(values) != 1 {
				return nil, false
			}
			seen[object] = true
			return resolve(values[0], seen)
		}
		return nil, false
	}
	var parameters []int
	found, known := false, true
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if ret, ok := n.(*ast.ReturnStmt); ok {
			if len(ret.Results) == 0 {
				known = false
			}
			for _, expr := range ret.Results {
				if embeddedTable(a.Info.TypeOf(expr), make(map[types.Type]bool)) == nil {
					continue
				}
				found = true
				indices, resolved := resolve(expr, make(map[types.Object]bool))
				known = known && resolved
				parameters = append(parameters, indices...)
			}
		}
		return true
	})
	return parameters, found && known
}
