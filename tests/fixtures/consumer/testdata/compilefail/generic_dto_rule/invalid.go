package invalid

import (
	"foundry.test/consumer/genericdto"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = genericdto.EnvelopeValidationFields[genericdto.UserDTO]().Data.Rules(validation.Rule[genericdto.ProjectDTO]{})
