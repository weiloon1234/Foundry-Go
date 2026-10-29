package invalid

import (
	"foundry.test/consumer/validationrules"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var fields = validationrules.RegistrationValidationFields()
var _ = validation.Rules(fields.Email, validation.Enum(validationrules.Business.EnumDescriptor()))
