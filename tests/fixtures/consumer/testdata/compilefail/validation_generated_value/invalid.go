package invalid

import (
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

var wrong validation.Rule[value.Optional[int]]
var _ = httpdto.UpdateUserValidationFields().Email.Rules(wrong)
