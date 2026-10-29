package codemod

import (
	"go/ast"
	"go/types"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

const ormPath = frameworkinfo.ModulePath + "/database/orm"

// ORM migrates objects from the consumer's installed dependency, without loading
// the framework implementation into the codemod.
type ORM struct{}

type ormRule struct {
	target        string
	receiverIndex int
	table         bool
	scoped        bool
}

var ormTerminals = map[string]map[string]string{
	"Query":       {"All": "All", "First": "First", "Require": "Require", "Count": "Count", "Exists": "Exists", "Each": "Each", "Chunk": "Chunk", "EachChunked": "EachChunked", "Paginate": "Paginate", "SimplePaginate": "SimplePaginate", "UpdateAll": "UpdateAll", "DeleteAll": "DeleteAll", "ForceDeleteAll": "ForceDeleteAll", "ForUpdate": "Lock", "ForShare": "ForShare", "ForNoKeyUpdate": "ForNoKeyUpdate", "ForKeyShare": "ForKeyShare"},
	"LockedQuery": {"All": "AllLocked", "First": "FirstLocked", "Require": "RequireLocked", "Each": "EachLocked"},
	"Table":       {"Find": "Find", "Require": "RequireKey", "Insert": "Insert", "InsertMany": "InsertMany", "Restore": "Restore", "Raw": "Raw", "Update": "Update", "Delete": "Delete", "ForceDelete": "ForceDelete"},
}

func ormNamed(typ types.Type) *types.Named {
	if typ == nil {
		return nil
	}
	named, _ := types.Unalias(typ).(*types.Named)
	if named == nil || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != ormPath || named.Obj().Pkg().Scope().Lookup(named.Obj().Name()) != named.Obj() {
		return nil
	}
	return named
}

func ormMethod(selection *types.Selection) (ormRule, bool) {
	if selection == nil {
		return ormRule{}, false
	}
	method, ok := selection.Obj().(*types.Func)
	if !ok {
		return ormRule{}, false
	}
	origin := method.Origin()
	signature, ok := origin.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return ormRule{}, false
	}
	named := ormNamed(signature.Recv().Type())
	if named == nil {
		return ormRule{}, false
	}
	found := false
	for i := 0; i < named.NumMethods(); i++ {
		if named.Method(i).Origin() == origin {
			found = true
			break
		}
	}
	if !found {
		return ormRule{}, false
	}
	family := named.Obj().Name()
	if target := ormOperators[family][origin.Name()]; target != "" {
		return ormRule{target: target}, true
	}
	target := ormTerminals[family][origin.Name()]
	if target == "" {
		return ormRule{}, false
	}
	index := 2
	if family == "Query" && strings.HasPrefix(origin.Name(), "For") && origin.Name() != "ForceDeleteAll" {
		index = 0
	}
	return ormRule{target: target, receiverIndex: index, table: family == "Table", scoped: family == "Table" && (target == "Update" || target == "Delete" || target == "ForceDelete")}, true
}

func ormRemovedMethod(selection *types.Selection) (string, bool) {
	if rule, ok := ormMethod(selection); ok {
		return rule.target, true
	}
	// Methodless handles also lose the sealed bridges on their old method sets.
	if selection != nil {
		if method, ok := selection.Obj().(*types.Func); ok {
			if sig, ok := method.Origin().Type().(*types.Signature); ok && sig.Recv() != nil {
				if named := ormNamed(sig.Recv().Type()); named != nil && ormOperators[named.Obj().Name()] != nil {
					for i := 0; i < named.NumMethods(); i++ {
						if named.Method(i).Origin() == method.Origin() {
							return "column expression adapter", true
						}
					}
				}
			}
		}
	}
	return "", false
}

func (ORM) Rewrite(fc *FileContext, node ast.Node) (ast.Node, string) {
	if call, ok := node.(*ast.CallExpr); ok {
		if replacement, message := rewriteORMAdapters(fc, call); message != "" {
			return replacement, message
		}
	}
	// Generic arguments at a direct sealed API call are handled with that call.
	skipBoundary := false
	if id, ok := node.(*ast.Ident); ok {
		for parent := fc.Parents[id]; parent != nil; parent = fc.Parents[parent] {
			if call, ok := parent.(*ast.CallExpr); ok {
				skipBoundary = ormSealedFunction(fc, call) != nil && ormInCallee(call.Fun, id)
				break
			}
		}
	}
	if !skipBoundary {
		if message := methodInterfaceBoundary(fc, node, ormRemovedMethod, "ORM"); message != "" {
			return nil, message
		}
	}
	var selector *ast.SelectorExpr
	var call *ast.CallExpr
	switch n := node.(type) {
	case *ast.CallExpr:
		call = n
		selector, _ = unparen(n.Fun).(*ast.SelectorExpr)
	case *ast.SelectorExpr:
		selector = n
		parent := fc.Parents[n]
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
	rule, ok := ormMethod(selection)
	if !ok {
		return nil, ""
	}
	if rule.scoped || rule.table && len(selection.Index()) != 1 {
		return rewriteORMTable(fc, selector, call, selection, rule)
	}
	return rewriteMethodAt(fc, selector, call, selection, rule.target, ormPath, rule.receiverIndex)
}

func ormInCallee(expr ast.Expr, id *ast.Ident) bool {
	switch n := unparen(expr).(type) {
	case *ast.Ident:
		return n == id
	case *ast.SelectorExpr:
		return n.Sel == id
	case *ast.IndexExpr:
		return ormInCallee(n.X, id)
	case *ast.IndexListExpr:
		return ormInCallee(n.X, id)
	}
	return false
}

func rewriteORMTable(fc *FileContext, selector *ast.SelectorExpr, call *ast.CallExpr, selection *types.Selection, rule ormRule) (ast.Node, string) {
	message := "Table." + selection.Obj().Name() + " → orm." + rule.target
	if call == nil || selection.Kind() != types.MethodVal {
		return nil, message + ": embedded or scoped table callback requires an explicit receiver capture"
	}
	if effectfulArguments(call.Args) || effectfulArguments([]ast.Expr{selector.X}) {
		return nil, message + ": scoped or embedded table evaluation requires an explicit receiver capture before argument effects"
	}
	for i := 0; i < 2; i++ {
		if !httpReplayStable(fc, call.Args[i]) {
			return nil, message + ": table arguments require capture before reordering potentially panicking expressions"
		}
	}
	receiver := selector.X
	typ := types.Unalias(selection.Recv())
	// Only a single, resolved, value embedding is supported. Never guess a field
	// from the method spelling or call the consumer's potentially overridden Key.
	if len(selection.Index()) > 1 {
		named, ok := typ.(*types.Named)
		if !ok {
			return nil, message + ": embedded field path requires manual review"
		}
		fields, ok := named.Underlying().(*types.Struct)
		if !ok || len(selection.Index()) != 2 {
			return nil, message + ": embedded field path requires manual review"
		}
		field := fields.Field(selection.Index()[0])
		if !field.Embedded() || ormNamed(field.Type()) == nil || ormNamed(field.Type()).Obj().Name() != "Table" {
			return nil, message + ": embedded field path requires manual review"
		}
		receiver = &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent(field.Name())}
	} else if ormNamed(typ) == nil {
		return nil, message + ": pointer receiver requires an explicit snapshot"
	}
	alias, ok := packageAlias(fc, ormPath, selector.Pos())
	if !ok {
		return nil, message + ": missing, dot-imported or shadowed ORM import"
	}
	args := insertReceiver(call.Args, receiver, 2)
	if rule.scoped {
		// Repeated access is safe only for identifiers and field paths with no
		// pointer traversal, indexing, calls, or receives.
		if !ormStableTable(fc, selector.X) {
			return nil, message + ": scoped write requires a stable table value or an explicit capture"
		}
		query := ormTableAccessor(fc, selector.X, receiver, "Query")
		key := ormTableAccessor(fc, selector.X, receiver, "Key")
		args = append([]ast.Expr{}, call.Args[:2]...)
		args = append(args, query)
		rest := call.Args[2:]
		if rule.target == "ForceDelete" {
			args = append(args, rest[0])
			rest = rest[1:]
		}
		args = append(args, key)
		args = append(args, rest...)
	}
	return &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent(alias), Sel: ast.NewIdent(rule.target)}, Args: args, Ellipsis: call.Ellipsis, Lparen: call.Lparen, Rparen: call.Rparen}, message
}

func ormStableTable(fc *FileContext, expr ast.Expr) bool {
	if _, ok := types.Unalias(fc.Info.TypeOf(expr)).(*types.Pointer); ok {
		return false
	}
	switch n := unparen(expr).(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		if selection := fc.Info.Selections[n]; selection != nil {
			return selection.Kind() == types.FieldVal && !selection.Indirect() && ormStableTable(fc, n.X)
		}
		return false
	}
	return false
}

// Prefer promoted access when it resolves to Table's own retained method. An
// application override must not alter the semantics of the old Table write.
func ormTableAccessor(fc *FileContext, original, projected ast.Expr, name string) ast.Expr {
	receiver := projected
	object, _, _ := types.LookupFieldOrMethod(fc.Info.TypeOf(original), false, fc.Package, name)
	if method, ok := object.(*types.Func); ok {
		signature := method.Origin().Type().(*types.Signature)
		if signature.Recv() != nil {
			if named := ormNamed(signature.Recv().Type()); named != nil && named.Obj().Name() == "Table" {
				for i := 0; i < named.NumMethods(); i++ {
					if named.Method(i).Origin() == method.Origin() {
						receiver = original
						break
					}
				}
			}
		}
	}
	return &ast.CallExpr{Fun: &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent(name)}}
}
