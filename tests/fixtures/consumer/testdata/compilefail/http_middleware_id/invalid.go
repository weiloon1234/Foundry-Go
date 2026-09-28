package invalid

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"net/http"
)

var route foundryhttp.RouteID
var _ = foundryhttp.DefineMiddleware(route, func(next http.Handler) (http.Handler, error) { return next, nil })
