package invalid

import (
	"context"

	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var endpoint authenticating.ProfileDeclaration
var binding http.GuardBinding[models.User]
var _ = http.Authenticated(endpoint, binding, http.AllOf(http.Permissions(auth.DefinePermission("accounts.view", func(context.Context, models.Order) (bool, error) { return true, nil }))))
