package invalid

import (
	"github.com/weiloon1234/Foundry-Go/value"
)

func invalid() { _ = value.JSON[string](value.JSON[int]{}) }
