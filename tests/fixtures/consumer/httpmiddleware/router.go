// Package httpmiddleware composes native middleware with generated endpoints.
package httpmiddleware

import (
	"net/http"

	"foundry.test/consumer/httpendpoints"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

const RouteTrace foundryhttp.MiddlewareID = "fixture.route-trace"

var trace = foundryhttp.DefineMiddleware(RouteTrace, func(next http.Handler) (http.Handler, error) {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if matched, ok := foundryhttp.MatchedRoute(r.Context()); ok {
			w.Header().Set("X-Matched-Route", string(matched.ID))
		}
		next.ServeHTTP(w, r)
	}), nil
})

var API = foundryhttp.DefineScope("/api", "api").WithMiddleware(trace)
var Update = httpendpoints.Update.Within(API)

func Router(service httpendpoints.Service) (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(Update.Handle(service.Update))
}
