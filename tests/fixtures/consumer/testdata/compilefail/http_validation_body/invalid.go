package invalid

import (
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	http "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var wrong validation.Rule[string]
var _ = http.ValidateBody[httpkernel.UserPath, httpquery.NearbyInput, httpdto.UpdateUser, httpdto.UserResponse](httpendpoints.Update, wrong)
