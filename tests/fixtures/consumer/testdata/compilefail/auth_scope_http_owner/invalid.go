package invalid

import (
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	http "github.com/weiloon1234/Foundry-Go/http"
)

func bad(endpoint authenticating.ProfileDeclaration, binding http.GuardBinding[models.User], scopes auth.AccessScopes[models.Group]) {
	_ = http.Authenticated(endpoint, binding, http.AllOf(http.Scopes(scopes)))
}
