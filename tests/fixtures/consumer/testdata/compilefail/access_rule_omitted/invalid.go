package invalid

import (
	"foundry.test/consumer/models"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var endpoint http.Endpoint[http.NoPath, http.NoQuery, http.NoBody, http.NoContent]
var binding http.GuardBinding[models.User]
var _ = http.Authenticated(endpoint, binding)
