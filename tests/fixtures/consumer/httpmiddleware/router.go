// Package httpmiddleware composes native middleware with generated endpoints.
package httpmiddleware

import (
	"context"
	"net/http"
	"net/netip"

	"foundry.test/consumer/httpendpoints"
	"github.com/weiloon1234/Foundry-Go/application"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

// doc-example:begin middleware-scope
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
var Update = foundryhttp.ConfigureEndpoint(httpendpoints.Update, foundryhttp.Within(API))

func Router(service httpendpoints.Service) (*foundryhttp.Router, error) {
	return foundryhttp.NewRouter(Update.Handle(service.Update))
}

// doc-example:end middleware-scope

// doc-example:begin middleware-route
func Probe(policy foundryhttp.Middleware) foundryhttp.RouteRegistration {
	route := foundryhttp.ConfigureRoute(foundryhttp.DefineRoute(foundryhttp.RouteSpec{
		ID: "probe", Method: foundryhttp.GET, Access: foundryhttp.Public,
	}, foundryhttp.StaticPath("/probe")), foundryhttp.Use(policy))
	endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	return endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	})
}

// doc-example:end middleware-route

// doc-example:begin middleware-global
func GlobalApplication(ctx context.Context, settings application.Settings, policies ...foundryhttp.Middleware) (*application.App, error) {
	return application.New(settings).Use(policies...).Build(ctx)
}

// doc-example:end middleware-global

// doc-example:begin middleware-credential-group
func CredentialGroup(parent foundryhttp.Scope, limiter ratelimit.Limiter[netip.Addr]) foundryhttp.Scope {
	return foundryhttp.DefineScope("/credentials", "credentials").Within(parent).
		WithMiddleware(foundryhttp.RateLimitByIP(limiter))
}

// doc-example:end middleware-credential-group
