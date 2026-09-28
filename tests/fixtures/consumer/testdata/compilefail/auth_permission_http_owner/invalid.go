package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

var endpoint authenticating.ProfileEndpoint
var _ = endpoint.WithPermissions(auth.DefinePermission("accounts.view", func(context.Context, models.Order) (bool, error) { return true, nil }))
