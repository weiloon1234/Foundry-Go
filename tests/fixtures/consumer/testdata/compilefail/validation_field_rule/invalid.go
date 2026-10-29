package invalid

import (
	"foundry.test/consumer/validationinput"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.Rules(validationinput.Name, validation.Min[int](1))
