package invalid

import (
	"foundry.test/consumer/validationrules"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.Confirmed(validationrules.RegistrationValidationFields().Email, validation.DefineField("name", func(v int) string { return "" }))
