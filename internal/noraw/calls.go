package noraw

import (
	"go/ast"
	"go/types"
)

type callResolver struct {
	info         *types.Info
	contextType  types.Type
	executorType types.Type
	signatures   []rawSignature
}

func (r callResolver) target(expr ast.Expr) string {
	// Named functions and method selections retain symbol/constraint matching.
	// Everything else is a callable value, classified only by its static type.
	direct := expr
	for {
		switch e := direct.(type) {
		case *ast.ParenExpr:
			direct = e.X
		case *ast.IndexExpr:
			if id := instantiationIdent(e.X); id != nil {
				if _, ok := r.info.Instances[id]; ok {
					direct = e.X
					continue
				}
			}
			return r.valueTarget(expr)
		case *ast.IndexListExpr:
			direct = e.X
		default:
			goto resolved
		}
	}
resolved:
	switch e := direct.(type) {
	case *ast.SelectorExpr:
		if selection := r.info.Selections[e]; selection != nil {
			if f, ok := selection.Obj().(*types.Func); ok {
				if symbol := frameworkRaw(f); symbol != "" {
					return symbol
				}
				if symbol := executorMethod(f, selection.Recv(), r.contextType, r.executorType); symbol != "" {
					return symbol
				}
				if recv := f.Type().(*types.Signature).Recv(); recv != nil {
					return executorMethod(f, recv.Type(), r.contextType, r.executorType)
				}
				return ""
			}
		} else if f, ok := r.info.Uses[e.Sel].(*types.Func); ok {
			return frameworkRaw(f)
		}
	case *ast.Ident:
		if f, ok := r.info.Uses[e].(*types.Func); ok {
			return frameworkRaw(f)
		}
	}
	return r.valueTarget(expr)
}
func instantiationIdent(expr ast.Expr) *ast.Ident {
	switch e := expr.(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	}
	return nil
}
func (r callResolver) valueTarget(expr ast.Expr) string {
	typ := r.info.TypeOf(expr)
	if typ == nil {
		return ""
	}
	sig := callableSignature(typ)
	if sig == nil {
		return ""
	}
	for _, raw := range r.signatures {
		if raw.matches(sig) {
			return raw.symbol
		}
	}
	return ""
}

// The caller is a type-checked call expression. For callable type parameters Go
// requires every member of the constraint's type set to have the same underlying
// signature, so any function term supplies that signature. Embedded constraints
// and unions may introduce it indirectly.
func callableSignature(typ types.Type) *types.Signature {
	switch t := typ.Underlying().(type) {
	case *types.Signature:
		return t
	case *types.Interface:
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if sig := callableSignature(t.EmbeddedType(i)); sig != nil {
				return sig
			}
		}
	case *types.Union:
		for i := 0; i < t.Len(); i++ {
			if sig := callableSignature(t.Term(i).Type()); sig != nil {
				return sig
			}
		}
	}
	return nil
}
