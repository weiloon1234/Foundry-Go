package compilefail

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var _ = foundryhttp.DescribeURL[string](
	foundryhttp.StringQuery[string](),
	contract.DefineScalar[int](contract.Type{ID: "count", Kind: contract.IntegerKind, Signed: true}),
)
