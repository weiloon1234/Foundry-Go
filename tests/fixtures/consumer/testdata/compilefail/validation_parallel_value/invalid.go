package invalid

import (
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.Parallel[string](validation.Min(1))
