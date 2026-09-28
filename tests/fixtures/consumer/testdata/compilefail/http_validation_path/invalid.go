package invalid

import (
	"foundry.test/consumer/httpendpoints"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var wrong validation.Rule[foundryhttp.NoPath]
var _ = httpendpoints.Update.WithPathValidation(wrong)
