package invalid

import (
	"foundry.test/consumer/models"
	h "github.com/weiloon1234/Foundry-Go/http"
	"net/http"
)

var route h.AuthenticatedRoute[h.NoPath, models.User]
var _ = route.HandleRaw(func(http.ResponseWriter, *http.Request, models.Order, h.NoPath) {})
