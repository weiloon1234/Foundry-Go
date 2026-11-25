package invalid

import (
	"foundry.test/consumer/models"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var route http.Route[http.NoPath]
var binding http.GuardBinding[models.User]
var _ = http.AuthenticatedRoute(route, binding, binding.AnyActor())
