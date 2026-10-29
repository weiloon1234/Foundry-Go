package invalid

import (
	"context"
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.Check(validation.Rules(httpdto.UpdateUserValidationFields().Email, validation.Optional(validation.NonBlank[string]())), context.Background(), httpdto.UserResponse{}, validation.DefaultLimits())
