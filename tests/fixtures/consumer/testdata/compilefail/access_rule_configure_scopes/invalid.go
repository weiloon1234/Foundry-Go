package invalid

import (
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var endpoint authenticating.ProfileEndpoint
var scopes auth.AccessScopes[models.User]
var _ = http.ConfigureAuthenticated(endpoint, http.Scopes(scopes))
