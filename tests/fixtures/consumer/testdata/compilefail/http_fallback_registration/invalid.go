package invalid

import h "github.com/weiloon1234/Foundry-Go/http"

func wrong(router *h.Router, route h.RouteRegistration) { router.WithFallbacks(route) }
