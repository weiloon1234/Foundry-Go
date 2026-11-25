package apicomment

import (
	"go/types"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/internal/retirement"
)

// direction says who supplies a value: the application (a parameter of an
// exported function, a field of an exported variable, a result of a callback
// the application writes) or the framework (a result, a parameter of a callback
// the framework calls). Only a struct the application supplies has its fields
// required.
type direction int

const (
	appSupplies direction = iota
	frameworkSupplies
)

func (d direction) flip() direction {
	if d == appSupplies {
		return frameworkSupplies
	}
	return appSupplies
}

// walkKey memoizes one named type reached in one direction. embedded is true
// when the type is reached as an embedded field, which makes an unexported
// struct's promoted fields settable.
type walkKey struct {
	typ      string
	dir      direction
	embedded bool
}

// walkRoots walks the exported API of the importable packages of one build
// configuration. Roots: every exported function (parameters are supplied by the
// application, results by the framework), every exported method of a type that
// is not an interface (the same), and the type of every exported variable
// (supplied by the application). An exported named function type or interface
// that no root reaches is walked as application-supplied, because the
// application writes the handlers and implementations of the public callback
// and interface types.
func (a *analysis) walkRoots(importable []retirement.SurfacePackage) {
	var callbacks []*types.Named
	candidate := make(map[string]bool)
	for _, pkg := range importable {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if !obj.Exported() {
				continue
			}
			switch obj := obj.(type) {
			case *types.Const:
				a.require(pkg.Dir, KindConst, name, obj.Pos(), name, obj.Type())
			case *types.Var:
				a.require(pkg.Dir, KindVar, name, obj.Pos(), name, obj.Type())
				a.visit(obj.Type(), appSupplies, false, name)
			case *types.Func:
				a.signature(obj.Type().(*types.Signature), frameworkSupplies, name)
			case *types.TypeName:
				named, ok := types.Unalias(obj.Type()).(*types.Named)
				if !ok || !a.isFramework(named.Obj().Pkg()) {
					continue
				}
				switch named.Underlying().(type) {
				case *types.Signature, *types.Interface:
					if key := types.TypeString(named.Origin(), nil); !candidate[key] {
						candidate[key] = true
						callbacks = append(callbacks, named)
					}
				default:
					a.methods(named, frameworkSupplies, named.Obj().Name())
				}
			}
		}
	}
	// The snapshot keeps the answer independent of declaration order: a type
	// the fallback walk itself reaches does not count as reached by a root.
	reachedByRoots := make(map[string]bool, len(a.reached))
	for key := range a.reached {
		reachedByRoots[key] = true
	}
	for _, named := range callbacks {
		if !reachedByRoots[types.TypeString(named.Origin(), nil)] {
			a.visit(named, appSupplies, false, named.Obj().Name())
		}
	}
}

// methods walks every exported method of a type, promoted ones included, as a
// function the application calls.
func (a *analysis) methods(named *types.Named, dir direction, prefix string) {
	set := types.NewMethodSet(types.NewPointer(named))
	for i := 0; i < set.Len(); i++ {
		if m, ok := set.At(i).Obj().(*types.Func); ok && m.Exported() {
			a.signature(m.Type().(*types.Signature), dir, prefix+"."+m.Name())
		}
	}
}

// signature walks a function type reached in direction dir: its parameters are
// supplied by the other side, its results by the same side.
func (a *analysis) signature(sig *types.Signature, dir direction, prefix string) {
	for i := 0; i < sig.Params().Len(); i++ {
		a.visit(sig.Params().At(i).Type(), dir.flip(), false, prefix+".p"+strconv.Itoa(i))
	}
	for i := 0; i < sig.Results().Len(); i++ {
		a.visit(sig.Results().At(i).Type(), dir, false, prefix+".r"+strconv.Itoa(i))
	}
}

// visit follows a type, supplied in direction dir, to the struct types and
// callbacks it reaches.
func (a *analysis) visit(t types.Type, dir direction, embedded bool, prefix string) {
	switch t := t.(type) {
	case *types.Alias:
		if _, anonymous := types.Unalias(t).(*types.Struct); anonymous {
			prefix = t.Obj().Name()
		}
		a.visit(types.Unalias(t), dir, embedded, prefix)
	case *types.Named:
		a.named(t, dir, embedded)
	case *types.Struct:
		a.structure(t, prefix, true, dir)
	case *types.Pointer:
		a.visit(t.Elem(), dir, embedded, prefix)
	case *types.Slice:
		a.visit(t.Elem(), dir, false, prefix)
	case *types.Array:
		a.visit(t.Elem(), dir, false, prefix)
	case *types.Chan:
		a.visit(t.Elem(), dir, false, prefix)
	case *types.Map:
		a.visit(t.Key(), dir, false, prefix)
		a.visit(t.Elem(), dir, false, prefix)
	case *types.Signature:
		a.signature(t, dir, prefix)
	case *types.Interface:
		a.interfaceMethods(t, dir, prefix)
	}
}

func (a *analysis) named(t *types.Named, dir direction, embedded bool) {
	key := walkKey{typ: types.TypeString(t, nil), dir: dir, embedded: embedded}
	if a.seen[key] {
		return
	}
	a.seen[key] = true
	for i := 0; i < t.TypeArgs().Len(); i++ {
		a.visit(t.TypeArgs().At(i), dir, false, "")
	}
	if !a.isFramework(t.Obj().Pkg()) {
		return
	}
	name := t.Origin().Obj().Name()
	switch under := t.Underlying().(type) {
	case *types.Struct:
		a.structure(under, name, t.Obj().Exported() || embedded, dir)
	case *types.Interface:
		a.reach(t)
		a.interfaceMethods(under, dir, name)
		return
	case *types.Signature:
		a.reach(t)
		a.signature(under, dir, name)
	default:
		a.visit(under, dir, false, name)
	}
	if dir == frameworkSupplies {
		a.methods(t, frameworkSupplies, name)
	}
}

// reach records that a root or another type reached an exported function type
// or interface, so it is not walked again as an application-written callback.
func (a *analysis) reach(t *types.Named) {
	if t.Obj().Exported() {
		a.reached[types.TypeString(t.Origin(), nil)] = true
	}
}

// interfaceMethods walks each exported method of an interface as a function
// type in the interface's direction: an interface the application implements
// receives framework-supplied parameters and returns application-supplied
// results; an interface the framework hands out is the reverse.
func (a *analysis) interfaceMethods(it *types.Interface, dir direction, prefix string) {
	for i := 0; i < it.NumMethods(); i++ {
		if m := it.Method(i); m.Exported() {
			a.signature(m.Type().(*types.Signature), dir, prefix+"."+m.Name())
		}
	}
}

// structure requires a comment on every exported field of a struct the
// application supplies, and follows every field the application can set or
// embed. A struct whose type name the application cannot write (unexported and
// not embedded in a nameable struct) requires nothing when the application
// supplies it.
func (a *analysis) structure(st *types.Struct, owner string, nameable bool, dir direction) {
	if dir == appSupplies && !nameable {
		return
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		origin := f.Origin()
		if dir == appSupplies && f.Exported() {
			if declared, ok := a.dirs[origin.Pkg().Path()]; ok {
				a.require(declared, KindField, owner+"."+f.Name(), origin.Pos(), f.Name(), origin.Type())
			}
		}
		if f.Exported() || f.Embedded() {
			a.visit(f.Type(), dir, f.Embedded(), owner+"."+f.Name())
		}
	}
}
