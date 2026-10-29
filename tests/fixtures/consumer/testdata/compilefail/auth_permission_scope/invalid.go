package invalid

import (
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var endpoint authenticating.ProfileEndpoint
var _ = http.ConfigureAuthenticated(endpoint, http.Permissions[models.User](auth.DefineAccessScope[models.User]("accounts.view")))
