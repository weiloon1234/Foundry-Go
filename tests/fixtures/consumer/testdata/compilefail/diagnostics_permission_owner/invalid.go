package compilefail

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	h "github.com/weiloon1234/Foundry-Go/http"
)

var route h.Route[h.NoPath]
var binding h.GuardBinding[models.User]
var _ = h.RouteAuthenticated(route, binding, h.AllOf(h.Permissions(auth.Permission[models.Order]{})))
