package invalid

import (
	"foundry.test/consumer/validationrules"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var fields = validationrules.RegistrationValidationFields()
var _ = fields.Email.Rules(validation.Enum(validationrules.Business.EnumDescriptor()))
