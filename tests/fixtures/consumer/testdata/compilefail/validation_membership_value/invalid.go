package invalid

import (
	"github.com/weiloon1234/Foundry-Go/validation"
)

var text string
var _ = validation.OneOf[int](text)
