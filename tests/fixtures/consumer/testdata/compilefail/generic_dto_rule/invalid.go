package invalid

import (
	"foundry.test/consumer/genericdto"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.Rules(genericdto.EnvelopeValidationFields[genericdto.UserDTO]().Data, validation.Rule[genericdto.ProjectDTO]{})
