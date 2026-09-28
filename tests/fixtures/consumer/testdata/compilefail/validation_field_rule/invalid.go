package invalid

import (
	"foundry.test/consumer/validationinput"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validationinput.Name.Rules(validation.Min[int](1))
