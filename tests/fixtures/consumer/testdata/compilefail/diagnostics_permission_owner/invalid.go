package compilefail

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	h "github.com/weiloon1234/Foundry-Go/http"
)

var route h.Route[h.NoPath]
var guard auth.Guard[models.User]
var _ = h.RequireRouteAuthentication(route, nil, guard, h.AllOf(h.Permissions(auth.Permission[models.Order]{})))
