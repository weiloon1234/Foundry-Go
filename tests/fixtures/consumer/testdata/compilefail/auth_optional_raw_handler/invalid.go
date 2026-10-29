package invalid

import (
	"net/http"

	"foundry.test/consumer/models"
	h "github.com/weiloon1234/Foundry-Go/http"
)

var route h.OptionalAuthenticationRoute[h.NoPath, models.User]
var _ = h.HandleOptionalRaw[h.NoPath, models.User](route, func(http.ResponseWriter, *http.Request, models.User, h.NoPath) {})
