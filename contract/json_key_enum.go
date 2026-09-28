package contract

import "github.com/weiloon1234/Foundry-Go/enum"

// EnumJSONKey reuses the same enum cases as value contracts. String enums use
// their exact names; integer enums use canonical decimal names. A custom enum
// text representation needs its own DefineJSONKey declaration.
func EnumJSONKey[K enum.Scalar](descriptor enum.Descriptor[K]) JSONKey[K] {
	id := TypeID(descriptor.PackagePath() + "." + descriptor.Name())
	return defineJSONKey(DefineScalar[K](EnumType(id, descriptor)), EnumJSONKeySyntax, false)
}
