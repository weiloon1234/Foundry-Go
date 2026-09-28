package generate

import "go/types"

func canonicalDTOInstantiation(typ types.Type) types.Type {
	named, ok := types.Unalias(typ).(*types.Named)
	if !ok || named.TypeArgs().Len() == 0 {
		return typ
	}
	arguments := make([]types.Type, named.TypeArgs().Len())
	for i := range arguments {
		arguments[i] = canonicalDTOArgument(named.TypeArgs().At(i))
	}
	concrete, err := types.Instantiate(nil, named.Origin(), arguments, false)
	if err != nil {
		return typ
	} // go/types already checked the consumer declaration.
	return concrete
}

func canonicalDTOArgument(typ types.Type) types.Type {
	switch t := types.Unalias(typ).(type) {
	case *types.Basic:
		return types.Typ[t.Kind()]
	case *types.Named:
		return canonicalDTOInstantiation(t)
	case *types.Pointer:
		return types.NewPointer(canonicalDTOArgument(t.Elem()))
	case *types.Slice:
		return types.NewSlice(canonicalDTOArgument(t.Elem()))
	case *types.Array:
		return types.NewArray(canonicalDTOArgument(t.Elem()), t.Len())
	case *types.Map:
		return types.NewMap(canonicalDTOArgument(t.Key()), canonicalDTOArgument(t.Elem()))
	case *types.Struct:
		fields, tags := make([]*types.Var, t.NumFields()), make([]string, t.NumFields())
		for i := range fields {
			f := t.Field(i)
			fields[i] = types.NewField(f.Pos(), f.Pkg(), f.Name(), canonicalDTOArgument(f.Type()), f.Embedded())
			tags[i] = t.Tag(i)
		}
		return types.NewStruct(fields, tags)
	default:
		return typ
	}
}
