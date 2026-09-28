package invalid

import (
	"foundry.test/consumer/validationinput"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.Compare(validationinput.Name, validationinput.Age, validation.Same[string]())
