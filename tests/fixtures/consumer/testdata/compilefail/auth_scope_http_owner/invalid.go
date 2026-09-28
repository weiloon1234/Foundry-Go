package invalid

import (
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func bad(endpoint authenticating.ProfileEndpoint, scopes auth.AccessScopes[models.Group]) {
	_ = endpoint.WithScopes(scopes)
}
