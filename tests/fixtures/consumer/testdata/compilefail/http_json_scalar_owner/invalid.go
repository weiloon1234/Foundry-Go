package invalid

import "github.com/weiloon1234/Foundry-Go/contract"

type First string
type Second string

var _ = contract.ScalarJSON[First](contract.Scalar[Second]{})
