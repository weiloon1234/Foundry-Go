package invalid

import (
	"context"

	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var endpoint authenticating.ProfileEndpoint
var _ = http.ConfigureAuthenticated[http.NoPath, http.NoQuery, http.NoBody, models.User, http.NoContent](endpoint, http.Permissions(auth.DefinePermission("accounts.view", func(context.Context, models.Order) (bool, error) { return true, nil })))
