package codemod

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

const validationPath = frameworkinfo.ModulePath + "/validation"

type Validation struct{}

var validationMethods = map[string]map[string]string{
	"Field": {"Rules": "Rules", "Validate": "ValidateField", "Name": "FieldName", "Select": "Select", "WithLabel": "WithLabel", "WithLabelKey": "WithLabelKey"},
	"Rule":  {"Validate": "ValidateRule", "Description": "DescribeRule", "Check": "Check", "CheckProhibitions": "CheckProhibitions", "WithMessage": "WithMessage"},
}

// Match the selected method's origin to the declaration object in its defining
// package. Name lookalikes and consumer-defined methods are never candidates.
func validationMethod(sel *types.Selection) (string, bool) {
	if sel == nil {
		return "", false
	}
	method, ok := sel.Obj().(*types.Func)
	if !ok || method.Pkg() == nil || method.Pkg().Path() != validationPath {
		return "", false
	}
	origin := method.Origin()
	for typeName, methods := range validationMethods {
		obj := method.Pkg().Scope().Lookup(typeName)
		if obj == nil {
			continue
		}
		named, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok {
			continue
		}
		for i := 0; i < named.NumMethods(); i++ {
			if named.Method(i).Origin() == origin {
				target, ok := methods[origin.Name()]
				return target, ok
			}
		}
	}
	return "", false
}

func (Validation) Rewrite(fc *FileContext, node ast.Node) (ast.Node, string) {
	if message := interfaceBoundary(fc, node); message != "" {
		return nil, message
	}
	var selector *ast.SelectorExpr
	var call *ast.CallExpr
	switch n := node.(type) {
	case *ast.CallExpr:
		call = n
		selector, _ = unparen(n.Fun).(*ast.SelectorExpr)
	case *ast.SelectorExpr:
		selector = n
		parent := fc.Parents[node]
		for {
			p, ok := parent.(*ast.ParenExpr)
			if !ok {
				break
			}
			parent = fc.Parents[p]
		}
		if c, ok := parent.(*ast.CallExpr); ok && unparen(c.Fun) == n {
			return nil, ""
		}
	default:
		return nil, ""
	}
	if selector == nil {
		return nil, ""
	}
	selection := fc.Info.Selections[selector]
	target, ok := validationMethod(selection)
	if !ok {
		return nil, ""
	}
	return rewriteSelectedMethod(fc, selector, call, selection, target, validationPath)
}

func rewriteSelectedMethod(fc *FileContext, selector *ast.SelectorExpr, call *ast.CallExpr, selection *types.Selection, target, path string) (ast.Node, string) {
	return rewriteMethodAt(fc, selector, call, selection, target, path, 0)
}

// receiverIndex is the receiver's position in the destination argument list.
func rewriteMethodAt(fc *FileContext, selector *ast.SelectorExpr, call *ast.CallExpr, selection *types.Selection, target, path string, receiverIndex int) (ast.Node, string) {
	message := selection.Obj().Name() + " → " + path[strings.LastIndex(path, "/")+1:] + "." + target
	if len(selection.Index()) != 1 {
		return nil, message + ": promoted receiver requires an explicit adapter"
	}
	receiverType := types.Unalias(selection.Recv())
	if _, ok := receiverType.(*types.Pointer); ok {
		return nil, message + ": pointer receiver requires manual snapshot/dereference"
	}
	named, ok := receiverType.(*types.Named)
	if !ok {
		return nil, message + ": unsupported receiver type"
	}
	alias, ok := packageAlias(fc, path, selector.Pos())
	if !ok {
		return nil, message + ": missing, dot-imported or shadowed package import"
	}
	function := ast.Expr(&ast.SelectorExpr{X: ast.NewIdent(alias), Sel: ast.NewIdent(target)})
	if selection.Kind() == types.MethodExpr && receiverIndex == 0 {
		var arguments []ast.Expr
		for i := 0; i < named.TypeArgs().Len(); i++ {
			expr, ok := typeExpr(fc, named.TypeArgs().At(i), selector.Pos())
			if !ok {
				return nil, message + ": type arguments cannot be named safely"
			}
			arguments = append(arguments, expr)
		}
		left, right := selector.Pos(), selector.Pos()
		var original []ast.Expr
		switch receiver := unparen(selector.X).(type) {
		case *ast.IndexExpr:
			left, right, original = receiver.Lbrack, receiver.Rbrack, []ast.Expr{receiver.Index}
		case *ast.IndexListExpr:
			left, right, original = receiver.Lbrack, receiver.Rbrack, receiver.Indices
		}
		matching := len(original) == len(arguments)
		for i := 0; matching && i < len(original); i++ {
			matching = types.Identical(fc.Info.TypeOf(original[i]), named.TypeArgs().At(i))
		}
		if matching {
			// Brackets and elements must come from the same source coordinates.
			arguments = original
		} else {
			// Aliases can hide or reorder type arguments. Anchor newly parsed
			// types together on the source line, not in the parser's private input.
			left, right = selector.Pos(), selector.Pos()
			for _, argument := range arguments {
				anchorTypePositions(reflect.ValueOf(argument), selector.Pos())
			}
		}
		if len(arguments) == 1 {
			function = &ast.IndexExpr{X: function, Lbrack: left, Index: arguments[0], Rbrack: right}
		} else if len(arguments) > 1 {
			function = &ast.IndexListExpr{X: function, Lbrack: left, Indices: arguments, Rbrack: right}
		}
		if call != nil {
			return &ast.CallExpr{Fun: function, Args: call.Args, Ellipsis: call.Ellipsis, Lparen: call.Lparen, Rparen: call.Rparen}, message
		}
		return function, message
	}
	if call != nil && selection.Kind() != types.MethodExpr {
		if len(call.Args) == 1 {
			if tuple, ok := fc.Info.TypeOf(call.Args[0]).(*types.Tuple); ok && tuple.Len() > 1 {
				return nil, message + ": multi-result argument requires manual expansion"
			}
		}
		reorderSafe := true
		for i := 0; i < receiverIndex; i++ {
			if !httpReplayStable(fc, call.Args[i]) {
				reorderSafe = false
			}
		}
		if !effectfulArguments(call.Args) && reorderSafe {
			return &ast.CallExpr{Fun: function, Args: insertReceiver(call.Args, selector.X, receiverIndex), Ellipsis: call.Ellipsis, Lparen: call.Lparen, Rparen: call.Rparen}, message
		}
	}

	signature, ok := selection.Type().(*types.Signature)
	if !ok {
		return nil, message + ": unsupported callback signature"
	}
	receiver, ok := typeExpr(fc, selection.Recv(), selector.Pos())
	if !ok {
		return nil, message + ": receiver type cannot be named safely"
	}
	// Names are unique across the entire source, including imports and type params.
	bound := freshName(fc, "foundryBound")
	parameters := &ast.FieldList{}
	var args []ast.Expr
	for i := 0; i < signature.Params().Len(); i++ {
		typ, ok := typeExpr(fc, signature.Params().At(i).Type(), selector.Pos())
		if !ok {
			return nil, message + ": callback parameter type cannot be named safely"
		}
		if signature.Variadic() && i == signature.Params().Len()-1 {
			array, ok := typ.(*ast.ArrayType)
			if !ok {
				return nil, message + ": unsupported variadic type"
			}
			typ = &ast.Ellipsis{Elt: array.Elt}
		}
		name := freshName(fc, fmt.Sprintf("foundryArg%d", i))
		parameters.List = append(parameters.List, &ast.Field{Names: []*ast.Ident{ast.NewIdent(name)}, Type: typ})
		args = append(args, ast.NewIdent(name))
	}
	results := &ast.FieldList{}
	for i := 0; i < signature.Results().Len(); i++ {
		typ, ok := typeExpr(fc, signature.Results().At(i).Type(), selector.Pos())
		if !ok {
			return nil, message + ": callback result type cannot be named safely"
		}
		results.List = append(results.List, &ast.Field{Type: typ})
	}
	callbackType := &ast.FuncType{Params: parameters, Results: results}
	invocationArgs := insertReceiver(args, ast.NewIdent(bound), receiverIndex)
	if selection.Kind() == types.MethodExpr {
		invocationArgs = insertReceiver(args[1:], args[0], receiverIndex)
	}
	invocation := &ast.CallExpr{Fun: function, Args: invocationArgs}
	if signature.Variadic() {
		invocation.Ellipsis = token.Pos(1)
	}
	var statement ast.Stmt = &ast.ReturnStmt{Results: []ast.Expr{invocation}}
	if signature.Results().Len() == 0 {
		statement = &ast.ExprStmt{X: invocation}
	}
	callback := &ast.FuncLit{Type: callbackType, Body: &ast.BlockStmt{List: []ast.Stmt{statement}}}
	if selection.Kind() == types.MethodExpr {
		if call != nil {
			return &ast.CallExpr{Fun: callback, Args: call.Args, Ellipsis: call.Ellipsis, Lparen: call.Lparen, Rparen: call.Rparen}, message
		}
		return callback, message + " (receiver-first forwarding closure)"
	}
	capture := &ast.FuncLit{
		Type: &ast.FuncType{Params: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{ast.NewIdent(bound)}, Type: receiver}}}, Results: &ast.FieldList{List: []*ast.Field{{Type: callbackType}}}},
		Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{Results: []ast.Expr{callback}}}},
	}
	boundCall := &ast.CallExpr{Fun: capture, Args: []ast.Expr{selector.X}}
	if call != nil {
		return &ast.CallExpr{Fun: boundCall, Args: call.Args, Ellipsis: call.Ellipsis, Lparen: call.Lparen, Rparen: call.Rparen}, message + " (capture before argument effects)"
	}
	return boundCall, message + " (capture receiver once)"
}
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}
func freshName(fc *FileContext, base string) string {
	used := map[string]bool{}
	for name := range fc.httpReplayNames {
		used[name] = true
	}
	for _, alias := range fc.AddedImports {
		used[alias] = true
	}
	ast.Inspect(fc.File, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	for name, i := base, 0; ; i++ {
		if !used[name] {
			return name
		}
		name = fmt.Sprintf("%s%d", base, i)
	}
}
func lookup(fc *FileContext, name string, pos token.Pos) types.Object {
	scope := fc.Package.Scope().Innermost(pos)
	if scope == nil {
		return nil
	}
	_, obj := scope.LookupParent(name, pos)
	return obj
}
func packageAlias(fc *FileContext, path string, pos token.Pos) (string, bool) {
	if alias := fc.AddedImports[path]; alias != "" {
		return alias, true
	}
	for _, spec := range fc.File.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil || imported != path {
			continue
		}
		var obj *types.PkgName
		if spec.Name != nil {
			obj, _ = fc.Info.Defs[spec.Name].(*types.PkgName)
		} else {
			obj, _ = fc.Info.Implicits[spec].(*types.PkgName)
		}
		if obj == nil || obj.Name() == "." || obj.Name() == "_" {
			return "", false
		}
		return obj.Name(), lookup(fc, obj.Name(), pos) == obj
	}
	alias := freshName(fc, "foundry"+path[strings.LastIndex(path, "/")+1:])
	fc.AddedImports[path] = alias
	return alias, true
}
func typeExpr(fc *FileContext, typ types.Type, pos token.Pos) (ast.Expr, bool) {
	valid := true
	source := types.TypeString(typ, func(pkg *types.Package) string {
		if pkg == fc.Package {
			return ""
		}
		alias, ok := packageAlias(fc, pkg.Path(), pos)
		if !ok {
			valid = false
		}
		return alias
	})
	if !valid {
		return nil, false
	}
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil, false
	}
	// Reject shadowed local type names, including predeclared names.
	ast.Inspect(expr, func(n ast.Node) bool {
		if selector, ok := n.(*ast.SelectorExpr); ok {
			if !ast.IsExported(selector.Sel.Name) {
				valid = false
			}
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			obj := lookup(fc, id.Name, pos)
			if expected := types.Universe.Lookup(id.Name); expected != nil && obj != expected {
				valid = false
			}
			if expected := fc.Package.Scope().Lookup(id.Name); expected != nil && obj != expected {
				valid = false
			}
			if _, ok := obj.(*types.TypeName); !ok {
				valid = false
			}
		}
		return true
	})
	return expr, valid
}

func interfaceBoundary(fc *FileContext, node ast.Node) string {
	return methodInterfaceBoundary(fc, node, validationMethod, "validation")
}

func methodInterfaceBoundary(fc *FileContext, node ast.Node, match func(*types.Selection) (string, bool), family string) string {
	affectedType := func(source types.Type, target types.Type) bool {
		if target == nil {
			return false
		}
		iface, ok := target.Underlying().(*types.Interface)
		if !ok || iface.NumMethods() == 0 {
			return false
		}
		if source == nil {
			return false
		}
		methods := types.NewMethodSet(source)
		for i := 0; i < iface.NumMethods(); i++ {
			method := iface.Method(i)
			selection := methods.Lookup(method.Pkg(), method.Name())
			if _, ok := match(selection); ok {
				return true
			}
		}
		return false
	}
	affected := func(expr ast.Expr, target types.Type) bool { return affectedType(fc.Info.TypeOf(expr), target) }
	matches := func(values []ast.Expr, targets []types.Type) bool {
		var sources []types.Type
		if len(values) == 1 {
			if tuple, ok := fc.Info.TypeOf(values[0]).(*types.Tuple); ok {
				for i := 0; i < tuple.Len(); i++ {
					sources = append(sources, tuple.At(i).Type())
				}
			}
		}
		if sources == nil {
			for _, value := range values {
				sources = append(sources, fc.Info.TypeOf(value))
			}
		}
		if len(sources) != len(targets) {
			return false
		}
		for i, source := range sources {
			if affectedType(source, targets[i]) {
				return true
			}
		}
		return false
	}
	message := family + " interface satisfaction requires an explicit consumer adapter"
	switch n := node.(type) {
	case *ast.Ident:
		instance, ok := fc.Info.Instances[n]
		if !ok {
			return ""
		}
		object := fc.Info.Uses[n]
		if object == nil {
			return ""
		}
		var parameters *types.TypeParamList
		switch typ := types.Unalias(object.Type()).(type) {
		case *types.Signature:
			parameters = typ.TypeParams()
		case *types.Named:
			parameters = typ.TypeParams()
		}
		if parameters != nil && parameters.Len() == instance.TypeArgs.Len() {
			for i := 0; i < parameters.Len(); i++ {
				if affectedType(instance.TypeArgs.At(i), parameters.At(i).Constraint()) {
					return message
				}
			}
		}
	case *ast.SendStmt:
		if channel, ok := fc.Info.TypeOf(n.Chan).Underlying().(*types.Chan); ok && affected(n.Value, channel.Elem()) {
			return message
		}
	case *ast.CompositeLit:
		typ := fc.Info.TypeOf(n)
		if typ == nil {
			return ""
		}
		for i, element := range n.Elts {
			value := element
			pair, keyed := element.(*ast.KeyValueExpr)
			if keyed {
				value = pair.Value
			}
			var target types.Type
			switch container := typ.Underlying().(type) {
			case *types.Slice:
				target = container.Elem()
			case *types.Array:
				target = container.Elem()
			case *types.Map:
				target = container.Elem()
				if keyed && affected(pair.Key, container.Key()) {
					return message
				}
			case *types.Struct:
				index := i
				if keyed {
					index = -1
					if key, ok := pair.Key.(*ast.Ident); ok {
						for j := 0; j < container.NumFields(); j++ {
							if container.Field(j).Name() == key.Name {
								index = j
								break
							}
						}
					}
				}
				if index >= 0 && index < container.NumFields() {
					target = container.Field(index).Type()
				}
			}
			if affected(value, target) {
				return message
			}
		}
	case *ast.ValueSpec:
		if n.Type != nil {
			var targets []types.Type
			for range n.Names {
				targets = append(targets, fc.Info.TypeOf(n.Type))
			}
			if matches(n.Values, targets) {
				return message
			}
		}
	case *ast.AssignStmt:
		var targets []types.Type
		for _, lhs := range n.Lhs {
			targets = append(targets, fc.Info.TypeOf(lhs))
		}
		if matches(n.Rhs, targets) {
			return message
		}
	case *ast.CallExpr:
		typ := fc.Info.TypeOf(n.Fun)
		if typ == nil {
			return ""
		}
		if fc.Info.Types[n.Fun].IsType() {
			for _, arg := range n.Args {
				if affected(arg, typ) {
					return message
				}
			}
			return ""
		}
		sig, ok := typ.Underlying().(*types.Signature)
		if !ok {
			return ""
		}
		if len(n.Args) == 1 {
			if tuple, ok := fc.Info.TypeOf(n.Args[0]).(*types.Tuple); ok {
				for i := 0; i < tuple.Len(); i++ {
					index := i
					if sig.Variadic() && index >= sig.Params().Len()-1 {
						index = sig.Params().Len() - 1
					}
					if index < 0 || index >= sig.Params().Len() {
						continue
					}
					target := sig.Params().At(index).Type()
					if sig.Variadic() && index == sig.Params().Len()-1 {
						target = target.(*types.Slice).Elem()
					}
					if affectedType(tuple.At(i).Type(), target) {
						return message
					}
				}
			}
		}
		for i, arg := range n.Args {
			index := i
			if sig.Variadic() && index >= sig.Params().Len()-1 {
				index = sig.Params().Len() - 1
			}
			if index < 0 || index >= sig.Params().Len() {
				continue
			}
			target := sig.Params().At(index).Type()
			if sig.Variadic() && index == sig.Params().Len()-1 && !n.Ellipsis.IsValid() {
				target = target.(*types.Slice).Elem()
			}
			if affected(arg, target) {
				return message
			}
		}
	case *ast.ReturnStmt:
		for parent := fc.Parents[node]; parent != nil; parent = fc.Parents[parent] {
			var sig *types.Signature
			switch f := parent.(type) {
			case *ast.FuncDecl:
				if obj := fc.Info.Defs[f.Name]; obj != nil {
					sig, _ = obj.Type().(*types.Signature)
				}
			case *ast.FuncLit:
				sig, _ = fc.Info.TypeOf(f).(*types.Signature)
			}
			if sig != nil {
				var targets []types.Type
				for i := 0; i < sig.Results().Len(); i++ {
					targets = append(targets, sig.Results().At(i).Type())
				}
				if matches(n.Results, targets) {
					return message
				}
				break
			}
		}
	}
	return ""
}

// A method receiver is captured before argument calls or receives. Retain that
// ordering explicitly when moving the receiver into a package-function argument.
func effectfulArguments(args []ast.Expr) bool {
	effectful := false
	for _, arg := range args {
		ast.Inspect(arg, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.CallExpr:
				effectful = true
			case *ast.UnaryExpr:
				if n.Op == token.ARROW {
					effectful = true
				}
			}
			return !effectful
		})
	}
	return effectful
}

// Only freshly parsed type syntax reaches this helper. Preserve NoPos fields,
// which distinguish absent syntax, while relocating present tokens together.
func anchorTypePositions(v reflect.Value, pos token.Pos) {
	if !v.IsValid() {
		return
	}
	if v.Type() == reflect.TypeOf(token.NoPos) {
		if v.CanSet() && v.Int() != 0 {
			v.SetInt(int64(pos))
		}
		return
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if !v.IsNil() {
			anchorTypePositions(v.Elem(), pos)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			anchorTypePositions(v.Field(i), pos)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			anchorTypePositions(v.Index(i), pos)
		}
	}
}

func insertReceiver(args []ast.Expr, receiver ast.Expr, index int) []ast.Expr {
	result := append([]ast.Expr{}, args[:index]...)
	result = append(result, receiver)
	return append(result, args[index:]...)
}
