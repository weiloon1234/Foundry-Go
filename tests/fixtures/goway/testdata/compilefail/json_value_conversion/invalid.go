package json_value_conversion

import (
	value "github.com/weiloon1234/Foundry-Go/value"
)

var _ = value.JSON[string](value.JSON[int]{})
