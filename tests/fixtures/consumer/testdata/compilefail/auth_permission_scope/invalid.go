package invalid

import (
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

var endpoint authenticating.ProfileEndpoint
var _ = endpoint.WithPermissions(auth.DefineAccessScope[models.User]("accounts.view"))
