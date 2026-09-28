package compilefail

import (
	"github.com/weiloon1234/Foundry-Go/diagnostics"
	h "github.com/weiloon1234/Foundry-Go/http"
)

var route = h.DefineRoute(h.RouteSpec{ID: "diagnostics", Method: h.GET, Access: h.Public}, h.StaticPath("/status"))
var _ = diagnostics.Route[int](nil, route, diagnostics.Status)
