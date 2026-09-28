package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

var routeCode foundryhttp.RouteID = "seats.unavailable"
var _ = foundryhttp.DefineError(routeCode, 409, "Unavailable")
