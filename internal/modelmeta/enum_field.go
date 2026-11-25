package modelmeta

import (
	"go/types"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

// HasEnumDescriptor reports a named type carrying the generated EnumDescriptor
// method. Imported enum metadata is an ordinary typed method, so consumers and
// gopls see the same contract; a matching name alone is not sufficient.
func HasEnumDescriptor(named *types.Named) bool {
	for i := 0; i < named.NumMethods(); i++ {
		method := named.Method(i)
		if method.Name() != "EnumDescriptor" {
			continue
		}
		sig, ok := method.Type().(*types.Signature)
		if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
			continue
		}
		result, ok := types.Unalias(sig.Results().At(0).Type()).(*types.Named)
		if ok && IsFrameworkType(result, "/enum", "Descriptor") && result.TypeArgs().Len() == 1 && types.Identical(result.TypeArgs().At(0), named) {
			return true
		}
	}
	return false
}

// IsFrameworkType reports the named framework type pkg.name, where pkg is the
// path below the framework module such as "/value".
func IsFrameworkType(named *types.Named, pkg, name string) bool {
	return named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == frameworkinfo.ModulePath+pkg && named.Obj().Name() == name
}

// FieldEnum returns the enum a persisted field holds: the field's own type, or
// the element of a value.Nullable, value.JSON, pointer, slice or array around
// it. isEnum decides which named types are enums. The result is nil for a
// field that holds no enum, including maps and structs that contain one.
func FieldEnum(t types.Type, isEnum func(*types.Named) bool) *types.Named {
	enum, _ := FieldEnumKind(t, isEnum)
	return enum
}

// FieldEnumKind is FieldEnum that also reports whether the field is a
// collection of the enum: a slice or array on the way to it.
func FieldEnumKind(t types.Type, isEnum func(*types.Named) bool) (enum *types.Named, collection bool) {
	for depth := 0; depth < 8; depth++ {
		switch current := types.Unalias(t).(type) {
		case *types.Pointer:
			t = current.Elem()
		case *types.Slice:
			t, collection = current.Elem(), true
		case *types.Array:
			t, collection = current.Elem(), true
		case *types.Named:
			if isEnum(current) {
				return current, collection
			}
			if current.TypeArgs().Len() == 1 && (IsFrameworkType(current, "/value", "Nullable") || IsFrameworkType(current, "/value", "JSON")) {
				t = current.TypeArgs().At(0)
				continue
			}
			return nil, false
		default:
			return nil, false
		}
	}
	return nil, false
}
