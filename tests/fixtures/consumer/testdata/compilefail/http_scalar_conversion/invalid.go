package compilefail

import "github.com/weiloon1234/Foundry-Go/contract"

var _ = contract.Scalar[int](contract.DefineScalar[string](contract.Type{ID: "code", Kind: contract.StringKind}))
