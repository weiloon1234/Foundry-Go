package compilefail

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	h "github.com/weiloon1234/Foundry-Go/http"
)

var route h.AuthenticatedRoute[h.NoPath, models.User]
var _ = route.WithPermissions(auth.Permission[models.Order]{})
