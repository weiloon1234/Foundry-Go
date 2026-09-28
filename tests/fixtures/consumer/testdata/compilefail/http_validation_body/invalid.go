package invalid

import (
	"foundry.test/consumer/httpendpoints"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var wrong validation.Rule[string]
var _ = httpendpoints.Update.WithBodyValidation(wrong)
