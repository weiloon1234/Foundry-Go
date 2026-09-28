package compilefail

import (
	"github.com/weiloon1234/Foundry-Go/diagnostics"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/observability"
)

var route h.AuthenticatedRoute[h.NoPath, int]
var _ = diagnostics.Route(nil, route, observability.HTTP)
