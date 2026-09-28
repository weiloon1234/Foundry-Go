package invalid

import (
	"foundry.test/consumer/httpendpoints"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var wrong validation.Rule[foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]]
var _ = httpendpoints.Update.WithValidation(wrong)
