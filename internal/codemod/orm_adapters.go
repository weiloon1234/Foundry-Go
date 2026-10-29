package codemod

import (
	"go/ast"
	"go/types"
	"strings"
)

// Inspect the original generic signature: an instantiated C parameter would
// hide its sealed constraint behind the concrete v0.1 column type.
func ormSealedFunction(fc *FileContext, call *ast.CallExpr) *types.Func {
	function := httpPackageFunction(fc, call.Fun)
	if function == nil {
		return nil
	}
	path := function.Pkg().Path()
	if path != ormPath && path != ormPath+"/rows" && path != ormPath+"/expr" {
		return nil
	}
	signature := function.Type().(*types.Signature)
	for i := 0; i < signature.Params().Len(); i++ {
		typ := signature.Params().At(i).Type()
		if signature.Variadic() && i == signature.Params().Len()-1 {
			typ = typ.(*types.Slice).Elem()
		}
		if ormSealedParameter(typ) {
			return function
		}
	}
	return nil
}

func ormSealedParameter(typ types.Type) bool {
	iface, ok := typ.Underlying().(*types.Interface)
	if !ok {
		return false
	}
	for i := 0; i < iface.NumMethods(); i++ {
		method := iface.Method(i)
		if !method.Exported() && method.Pkg() != nil && method.Pkg().Path() == ormPath {
			switch method.Name() {
			case "expressionColumn", "ormColumn", "orderedColumn":
				return true
			}
		}
	}
	return false
}

func rewriteORMAdapters(fc *FileContext, call *ast.CallExpr) (ast.Node, string) {
	function := ormSealedFunction(fc, call)
	if function == nil {
		return nil, ""
	}
	signature := function.Type().(*types.Signature)
	args := append([]ast.Expr{}, call.Args...)
	changed := false
	for i, arg := range args {
		index := i
		if signature.Variadic() && index >= signature.Params().Len()-1 {
			index = signature.Params().Len() - 1
		}
		if index < 0 || index >= signature.Params().Len() {
			continue
		}
		typ := signature.Params().At(index).Type()
		variadic := signature.Variadic() && index == signature.Params().Len()-1
		if variadic {
			typ = typ.(*types.Slice).Elem()
		}
		if !ormSealedParameter(typ) {
			continue
		}
		if variadic && call.Ellipsis.IsValid() {
			return nil, "ORM sealed column slice requires per-element expression adapters"
		}
		named := ormNamed(fc.Info.TypeOf(arg))
		if named == nil || ormOperators[named.Obj().Name()] == nil {
			return nil, "ORM sealed column argument requires a concrete receiver family and role before choosing an expression adapter"
		}
		// Explicit generic arguments can fix C to the old handle, which the new
		// adapter no longer satisfies. Do not silently discard consumer type choices.
		explicit := 0
		switch fun := unparen(call.Fun).(type) {
		case *ast.IndexExpr:
			explicit = 1
		case *ast.IndexListExpr:
			explicit = len(fun.Indices)
		}
		for j := 0; j < explicit && j < signature.TypeParams().Len(); j++ {
			if ormSealedParameter(signature.TypeParams().At(j).Constraint()) {
				return nil, "ORM sealed API fixes a column type argument; review its expression adapter type"
			}
		}
		alias, ok := packageAlias(fc, ormPath, arg.Pos())
		if !ok {
			return nil, "ORM expression adapter requires an unshadowed package import"
		}
		prefix := ""
		for method, target := range ormOperators[named.Obj().Name()] {
			prefix = strings.TrimSuffix(target, method)
			break
		}
		args[i] = &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent(alias), Sel: ast.NewIdent(prefix + "Expr")}, Args: []ast.Expr{arg}}
		changed = true
	}
	if !changed {
		return nil, ""
	}
	result := *call
	result.Args = args
	return &result, "ORM sealed column arguments → per-family expression adapters"
}
