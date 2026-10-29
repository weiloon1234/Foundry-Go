package invalid

import (
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	http "github.com/weiloon1234/Foundry-Go/http"
)

func bad(endpoint authenticating.ProfileEndpoint, scopes auth.AccessScopes[models.Group]) {
	_ = http.ConfigureAuthenticated[http.NoPath, http.NoQuery, http.NoBody, models.User, http.NoContent](endpoint, http.Scopes(scopes))
}
