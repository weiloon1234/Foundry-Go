package invalid

import "github.com/weiloon1234/Foundry-Go/validation"

type Code string
type Other string

var _ = validation.StartsWith[Code](Other("00"))
