package invalid

import (
	"net/http"

	"foundry.test/consumer/models"
	h "github.com/weiloon1234/Foundry-Go/http"
)

var route h.AuthenticatedRoute[h.NoPath, models.User]
var _ = h.HandleAuthenticatedRaw[h.NoPath, models.User](route, func(http.ResponseWriter, *http.Request, models.Order, h.NoPath) {})
