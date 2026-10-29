package codemod

import (
	"go/ast"
	"go/types"
	"strings"
)

const validationDatabasePath = validationPath + "/database"

// Inspect the original generic signature: an instantiated C parameter would
// hide its sealed constraint behind the concrete v0.1 column type.
func ormSealedFunction(fc *FileContext, call *ast.CallExpr) *types.Func {
	function := httpPackageFunction(fc, call.Fun)
	if function == nil {
		return nil
	}
	path := function.Pkg().Path()
	if path != ormPath && path != ormPath+"/rows" && path != ormPath+"/expr" && path != validationDatabasePath {
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

func isORMAdapterType(typ types.Type) bool {
	named := ormNamed(typ)
	if named == nil || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	if named.Obj().Pkg().Path() != ormPath {
		return false
	}
	return strings.HasSuffix(named.Obj().Name(), "Adapter")
}

func isSafeUniqueIgnoringExclude(fc *FileContext, exclude ast.Expr, key ast.Expr) bool {
	fn, ok := unparen(exclude).(*ast.FuncLit)
	if !ok {
		return false
	}
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	paramName := ""
	if len(fn.Type.Params.List[0].Names) == 1 {
		paramName = fn.Type.Params.List[0].Names[0].Name
	}
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return false
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	call, ok := unparen(ret.Results[0]).(*ast.CallExpr)
	if !ok {
		return false
	}
	calledFunc := httpPackageFunction(fc, call.Fun)
	if calledFunc == nil || calledFunc.Pkg() == nil || calledFunc.Pkg().Path() != ormPath {
		return false
	}
	if calledFunc.Name() != "KeyNe" && calledFunc.Name() != "KeyColumnNe" {
		return false
	}
	if len(call.Args) != 2 {
		return false
	}
	if !sameExpressionNode(call.Args[0], key) {
		return false
	}
	idArg, ok := unparen(call.Args[1]).(*ast.Ident)
	if !ok || (paramName != "" && idArg.Name != paramName) {
		return false
	}
	return true
}

func sameExpressionNode(a, b ast.Expr) bool {
	a = unparen(a)
	b = unparen(b)
	switch aTyped := a.(type) {
	case *ast.Ident:
		bTyped, ok := b.(*ast.Ident)
		return ok && aTyped.Name == bTyped.Name
	case *ast.SelectorExpr:
		bTyped, ok := b.(*ast.SelectorExpr)
		return ok && aTyped.Sel.Name == bTyped.Sel.Name && sameExpressionNode(aTyped.X, bTyped.X)
	default:
		return false
	}
}

func rewriteORMAdapters(fc *FileContext, call *ast.CallExpr) (ast.Node, string) {
	function := ormSealedFunction(fc, call)
	if function == nil {
		return nil, ""
	}
	signature := function.Type().(*types.Signature)
	args := append([]ast.Expr{}, call.Args...)
	changed := false

	isUniqueIgnoring := function.Pkg().Path() == validationDatabasePath && function.Name() == "UniqueIgnoring"
	if isUniqueIgnoring && len(args) == 6 {
		if !isSafeUniqueIgnoringExclude(fc, args[4], args[3]) {
			return nil, "UniqueIgnoring call with custom exclusion callback cannot be rewritten safely"
		}
		args = append(args[:4], args[5])
		changed = true
	}

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
		if isORMAdapterType(fc.Info.TypeOf(arg)) {
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
