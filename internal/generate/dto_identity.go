package generate

import (
	"go/types"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/gotype"
)

// dtoNamedIdentity mirrors Go's runtime name for named values instantiated with
// named/scalar arguments. TypeString adds spaces between generic arguments;
// reflect.Type.Name does not. Building the arguments avoids editing quoted tags
// or other anonymous type syntax. Composite fallback identities remain unchanged.
func dtoNamedIdentity(named *types.Named) (string, bool) {
	object := named.Obj()
	if object.Pkg() == nil {
		return "", false
	}
	name := object.Pkg().Path() + "." + object.Name()
	args := named.TypeArgs()
	if args.Len() == 0 {
		return name, true
	}
	parts := make([]string, args.Len())
	for i := range args.Len() {
		switch arg := types.Unalias(args.At(i)).(type) {
		case *types.Named:
			var ok bool
			parts[i], ok = dtoNamedIdentity(arg)
			if !ok {
				return "", false
			}
		case *types.Basic:
			if arg.Info()&types.IsUntyped != 0 || arg.Kind() == types.Invalid || arg.Kind() == types.UnsafePointer {
				return "", false
			}
			// byte/rune aliases have the runtime names uint8/int32.
			parts[i] = types.Typ[arg.Kind()].Name()
		default:
			parts[i] = gotype.Compact(types.TypeString(arg, func(p *types.Package) string { return p.Path() }))
		}
	}
	return name + "[" + strings.Join(parts, ",") + "]", true
}
