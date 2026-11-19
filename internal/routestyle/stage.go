package routestyle

import (
	"go/types"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

// stageTypes derives named origins from catalogue results and first arguments.
// Scopes/specifications configure stages; registrations terminate them. A
// datatable.Table is a reusable declaration, not a route stage, even when passed
// inline to QueryEndpoint. These exclusions are explicit at this shared owner.
func stageTypes(pkg *types.Package) map[*types.TypeName]bool {
	result := make(map[*types.TypeName]bool)
	add := func(typ types.Type) {
		named := stageOrigin(typ)
		if named == nil || named.Obj().Pkg() != pkg {
			return
		}
		switch pkg.Path() + "." + named.Obj().Name() {
		case frameworkinfo.ModulePath + "/http.RouteRegistration",
			frameworkinfo.ModulePath + "/http.Scope",
			frameworkinfo.ModulePath + "/http.RouteSpec",
			frameworkinfo.ModulePath + "/datatable.Table":
			return
		}
		result[named.Obj()] = true
	}
	for _, name := range pkg.Scope().Names() {
		fn, ok := pkg.Scope().Lookup(name).(*types.Func)
		if !ok || !IsDeclaration(fn) {
			continue
		}
		sig := fn.Type().(*types.Signature)
		for i := 0; i < sig.Results().Len(); i++ {
			add(sig.Results().At(i).Type())
		}
		if sig.Params().Len() > 0 {
			add(sig.Params().At(0).Type())
		}
	}
	return result
}

// stageOrigin unwraps aliases and pointers while retaining nominal identity.
func stageOrigin(typ types.Type) *types.Named {
	if typ == nil {
		return nil
	}
	switch t := types.Unalias(typ).(type) {
	case *types.Pointer:
		return stageOrigin(t.Elem())
	case *types.Named:
		return t.Origin()
	}
	return nil
}

// IsStage recognizes stage values, pointers and result tuples containing a stage,
// independently of the called function or its generic instantiation.
func (a *Analysis) IsStage(typ types.Type) bool {
	if typ == nil {
		return false
	}
	if tuple, ok := types.Unalias(typ).(*types.Tuple); ok {
		for i := 0; i < tuple.Len(); i++ {
			if a.IsStage(tuple.At(i).Type()) {
				return true
			}
		}
		return false
	}
	named := stageOrigin(typ)
	if named == nil || named.Obj().Pkg() == nil {
		return false
	}
	pkg := named.Obj().Pkg()
	stages, ok := a.stages[pkg]
	if !ok {
		stages = stageTypes(pkg)
		a.stages[pkg] = stages
	}
	return stages[named.Obj()]
}
