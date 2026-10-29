package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	h "github.com/weiloon1234/Foundry-Go/http"
)

var route h.AuthenticatedRoute[h.NoPath, models.User]
var _ = h.ConfigureAuthenticatedRoute[h.NoPath, models.User](route, h.Scopes(auth.AccessScopes[models.Order]{}))
