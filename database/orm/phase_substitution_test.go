package orm

import (
	"go/token"
	"go/types"
	"testing"
)

// A layout match solves equations between the two declarations' type parameters.
// Nested named types keep their identities; only the outer conversion's names
// are erased. Constraints are deliberately not solved: an impossible-constraint
// layout must still be explicitly classified by the conversion guard.
type phaseSubstitution map[*types.TypeParam]types.Type

func phaseTypeParams(typ types.Type) *types.TypeParamList {
	switch typ := typ.(type) {
	case *types.Named:
		return typ.TypeParams()
	case *types.Alias:
		return typ.TypeParams()
	}
	return nil
}

func (s phaseSubstitution) resolve(typ types.Type) types.Type {
	typ = types.Unalias(typ)
	for {
		param, ok := typ.(*types.TypeParam)
		if !ok || s[param] == nil {
			return typ
		}
		typ = types.Unalias(s[param])
	}
}

func (s phaseSubstitution) match(a, b types.Type) bool {
	a, b = s.resolve(a), s.resolve(b)
	if types.Identical(a, b) {
		return true
	}
	if p, ok := a.(*types.TypeParam); ok {
		if s.occurs(p, b) {
			return false
		}
		s[p] = b
		return true
	}
	if _, ok := b.(*types.TypeParam); ok {
		return s.match(b, a)
	}
	switch a := a.(type) {
	case *types.Named:
		b, ok := b.(*types.Named)
		if !ok || !types.Identical(a.Origin(), b.Origin()) || a.TypeArgs().Len() != b.TypeArgs().Len() {
			return false
		}
		for i := 0; i < a.TypeArgs().Len(); i++ {
			if !s.match(a.TypeArgs().At(i), b.TypeArgs().At(i)) {
				return false
			}
		}
		return true
	case *types.Pointer:
		b, ok := b.(*types.Pointer)
		return ok && s.match(a.Elem(), b.Elem())
	case *types.Slice:
		b, ok := b.(*types.Slice)
		return ok && s.match(a.Elem(), b.Elem())
	case *types.Array:
		b, ok := b.(*types.Array)
		return ok && a.Len() == b.Len() && s.match(a.Elem(), b.Elem())
	case *types.Map:
		b, ok := b.(*types.Map)
		return ok && s.match(a.Key(), b.Key()) && s.match(a.Elem(), b.Elem())
	case *types.Chan:
		b, ok := b.(*types.Chan)
		return ok && a.Dir() == b.Dir() && s.match(a.Elem(), b.Elem())
	case *types.Struct:
		b, ok := b.(*types.Struct)
		if !ok || a.NumFields() != b.NumFields() {
			return false
		}
		for i := 0; i < a.NumFields(); i++ {
			x, y := a.Field(i), b.Field(i)
			if x.Id() != y.Id() || x.Embedded() != y.Embedded() || !s.match(x.Type(), y.Type()) {
				return false
			}
		}
		return true // Go conversions ignore struct tags.
	case *types.Tuple:
		b, ok := b.(*types.Tuple)
		if !ok || a.Len() != b.Len() {
			return false
		}
		for i := 0; i < a.Len(); i++ {
			if !s.match(a.At(i).Type(), b.At(i).Type()) {
				return false
			}
		}
		return true
	case *types.Signature:
		b, ok := b.(*types.Signature)
		return ok && a.Variadic() == b.Variadic() && a.TypeParams().Len() == 0 && b.TypeParams().Len() == 0 && s.match(a.Params(), b.Params()) && s.match(a.Results(), b.Results())
	case *types.Interface:
		b, ok := b.(*types.Interface)
		if !ok || !a.IsMethodSet() || !b.IsMethodSet() || a.NumMethods() != b.NumMethods() {
			return false
		}
		for i := 0; i < a.NumMethods(); i++ {
			x, y := a.Method(i), b.Method(i)
			if x.Id() != y.Id() || !s.match(x.Type(), y.Type()) {
				return false
			}
		}
		return true
	}
	return false
}

// Walk only type arguments of named types, never their recursive definitions.
func phaseChildren(typ types.Type) []types.Type {
	var children []types.Type
	switch typ := types.Unalias(typ).(type) {
	case *types.Named:
		for i := 0; i < typ.TypeArgs().Len(); i++ {
			children = append(children, typ.TypeArgs().At(i))
		}
	case *types.Pointer:
		children = append(children, typ.Elem())
	case *types.Slice:
		children = append(children, typ.Elem())
	case *types.Array:
		children = append(children, typ.Elem())
	case *types.Map:
		children = append(children, typ.Key(), typ.Elem())
	case *types.Chan:
		children = append(children, typ.Elem())
	case *types.Struct:
		for i := 0; i < typ.NumFields(); i++ {
			children = append(children, typ.Field(i).Type())
		}
	case *types.Tuple:
		for i := 0; i < typ.Len(); i++ {
			children = append(children, typ.At(i).Type())
		}
	case *types.Signature:
		children = append(children, typ.Params(), typ.Results())
	case *types.Interface:
		for i := 0; i < typ.NumMethods(); i++ {
			children = append(children, typ.Method(i).Type())
		}
	}
	return children
}

func (s phaseSubstitution) occurs(param *types.TypeParam, typ types.Type) bool {
	typ = s.resolve(typ)
	if typ == param {
		return true
	}
	for _, child := range phaseChildren(typ) {
		if s.occurs(param, child) {
			return true
		}
	}
	return false
}

func (s phaseSubstitution) concrete(t *testing.T, typ types.Type) types.Type {
	t.Helper()
	typ = s.resolve(typ)
	switch typ := typ.(type) {
	case *types.TypeParam:
		return types.Typ[types.Int]
	case *types.Named:
		if typ.TypeArgs().Len() == 0 {
			return typ
		}
		args := make([]types.Type, typ.TypeArgs().Len())
		for i := range args {
			args[i] = s.concrete(t, typ.TypeArgs().At(i))
		}
		instance, err := types.Instantiate(nil, typ.Origin(), args, false)
		if err != nil {
			t.Fatal(err)
		}
		return instance
	case *types.Pointer:
		return types.NewPointer(s.concrete(t, typ.Elem()))
	case *types.Slice:
		return types.NewSlice(s.concrete(t, typ.Elem()))
	case *types.Array:
		return types.NewArray(s.concrete(t, typ.Elem()), typ.Len())
	case *types.Map:
		return types.NewMap(s.concrete(t, typ.Key()), s.concrete(t, typ.Elem()))
	case *types.Chan:
		return types.NewChan(typ.Dir(), s.concrete(t, typ.Elem()))
	case *types.Struct:
		fields := make([]*types.Var, typ.NumFields())
		tags := make([]string, len(fields))
		for i := range fields {
			f := typ.Field(i)
			fields[i] = types.NewField(token.NoPos, f.Pkg(), f.Name(), s.concrete(t, f.Type()), f.Embedded())
			tags[i] = typ.Tag(i)
		}
		return types.NewStruct(fields, tags)
	case *types.Tuple:
		vars := make([]*types.Var, typ.Len())
		for i := range vars {
			v := typ.At(i)
			vars[i] = types.NewVar(token.NoPos, v.Pkg(), v.Name(), s.concrete(t, v.Type()))
		}
		return types.NewTuple(vars...)
	case *types.Signature:
		return types.NewSignatureType(nil, nil, nil, s.concrete(t, typ.Params()).(*types.Tuple), s.concrete(t, typ.Results()).(*types.Tuple), typ.Variadic())
	case *types.Interface:
		methods := make([]*types.Func, typ.NumMethods())
		for i := range methods {
			m := typ.Method(i)
			methods[i] = types.NewFunc(token.NoPos, m.Pkg(), m.Name(), s.concrete(t, m.Type()).(*types.Signature))
		}
		return types.NewInterfaceType(methods, nil).Complete()
	}
	return typ
}

func (s phaseSubstitution) instantiate(t *testing.T, typ types.Type) types.Type {
	t.Helper()
	params := phaseTypeParams(typ)
	if params.Len() == 0 {
		return typ
	}
	args := make([]types.Type, params.Len())
	for i := range args {
		args[i] = s.concrete(t, params.At(i))
	}
	instance, err := types.Instantiate(nil, typ, args, false)
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

func phaseConversionWitness(t *testing.T, a, b types.Type) (types.Type, types.Type, bool) {
	t.Helper()
	s := phaseSubstitution{}
	if !s.match(a.Underlying(), b.Underlying()) {
		return nil, nil, false
	}
	left, right := s.instantiate(t, a), s.instantiate(t, b)
	return left, right, !types.Identical(left, right) && types.ConvertibleTo(left, right) && types.ConvertibleTo(right, left)
}

func TestPhaseSweepMatchesConcreteAndNestedArguments(t *testing.T) {
	pkg := surfacePackage(t, `package orm
 import (
 "github.com/weiloon1234/Foundry-Go/internal/querycore"
 "github.com/weiloon1234/Foundry-Go/temporal"
 "github.com/weiloon1234/Foundry-Go/value"
 )
 type GeneralInputProbe[M,I,V any] struct { _ [0]*M; stored columnData[V]; input func(I) querycore.AssignmentValue }
 type IntervalInputProbe[M,I any] struct { _ [0]*M; stored columnData[temporal.Interval]; input func(I) querycore.AssignmentValue }
 type JSONInputProbe[M,I,T any] struct { _ [0]*M; stored columnData[value.JSON[T]]; input func(I) querycore.AssignmentValue }
 type GeneralNullableInputProbe[M,I,V any] struct { _ [0]*M; stored nullableColumnData[V]; input func(I) querycore.AssignmentValue }
 type IntervalNullableInputProbe[M,I any] struct { _ [0]*M; stored nullableColumnData[temporal.Interval]; input func(I) querycore.AssignmentValue }
 type JSONNullableInputProbe[M,I,T any] struct { _ [0]*M; stored nullableColumnData[value.JSON[T]]; input func(I) querycore.AssignmentValue }
 type PairProbe[X,Y any] struct { first X; second Y }
 type NestedProbe[Z any] struct { first []Z; second map[string]Z }
 type RecursiveLeftProbe[X any] struct { first X; second []X }
 type RecursiveRightProbe[Y any] struct { first []Y; second Y }
 `)
	for _, pair := range [][2]string{
		{"GeneralInputProbe", "IntervalInputProbe"},
		{"GeneralInputProbe", "JSONInputProbe"},
		{"GeneralNullableInputProbe", "IntervalNullableInputProbe"},
		{"GeneralNullableInputProbe", "JSONNullableInputProbe"},
		{"PairProbe", "NestedProbe"},
	} {
		a, b := pkg.Scope().Lookup(pair[0]).Type(), pkg.Scope().Lookup(pair[1]).Type()
		// The old int-only sweep misses every witness exercised here.
		x, y := (phaseSubstitution{}).instantiate(t, a), (phaseSubstitution{}).instantiate(t, b)
		if types.ConvertibleTo(x, y) && types.ConvertibleTo(y, x) {
			t.Fatalf("probe does not expose the old sampling gap: %v", pair)
		}
		left, right, ok := phaseConversionWitness(t, a, b)
		if !ok {
			t.Fatalf("missed matching substitutions: %v", pair)
		}
		// These probes have unconstrained parameters: require genuine valid Go
		// instantiations as well as the guard's conservative shape comparison.
		for _, witness := range []types.Type{left, right} {
			named := witness.(*types.Named)
			args := make([]types.Type, named.TypeArgs().Len())
			for i := range args {
				args[i] = named.TypeArgs().At(i)
			}
			if _, err := types.Instantiate(nil, named.Origin(), args, true); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("matching witness: %s <-> %s", left, right)
	}
	a, b := pkg.Scope().Lookup("RecursiveLeftProbe").Type(), pkg.Scope().Lookup("RecursiveRightProbe").Type()
	if _, _, ok := phaseConversionWitness(t, a, b); ok {
		t.Fatal("accepted an infinite recursive substitution")
	}
}
