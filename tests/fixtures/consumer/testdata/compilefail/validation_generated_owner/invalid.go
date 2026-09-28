package invalid

import (
	"context"
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = httpdto.UpdateUserValidationFields().Email.Rules(validation.Optional(validation.NonBlank[string]())).Check(context.Background(), httpdto.UserResponse{}, validation.DefaultLimits())
