package contract

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// RequireObjectRoot checks that a normalized schema's root is directly a
// non-nullable object. Nested fields keep their own shapes.
// Example: err := contract.RequireObjectRoot(schema).
func RequireObjectRoot(schema Schema) error {
	types := make(map[TypeID]Type, len(schema.Types))
	for _, typ := range schema.Types {
		types[typ.ID] = typ
	}
	return requireObjectRoot(schema.Root, types)
}

func requireObjectRoot(root TypeID, types map[TypeID]Type) error {
	typ, ok := types[root]
	if ok && typ.Kind == ObjectKind && !typ.Nullable {
		return nil
	}
	return safeerror.NewFault(fault.Invalid, "JSON root must be a non-nullable object")
}
