package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var endpoint http.Endpoint[http.NoPath, http.NoQuery, http.NoBody, http.NoContent]
var binding http.GuardBinding[models.User]
var scopes auth.AccessScopes[models.User]
var _ = http.Authenticated(endpoint, binding, http.Scopes(scopes))
