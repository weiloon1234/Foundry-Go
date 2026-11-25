// Package httpmiddleware composes native middleware with generated endpoints.
package httpmiddleware

import (
	"context"
	stdhttp "net/http"
	"net/netip"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	"github.com/weiloon1234/Foundry-Go/application"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

type UpdateEndpoint = foundryhttp.Endpoint[httpkernel.UserPath, httpquery.NearbyInput, httpdto.UpdateUser, httpdto.UserResponse]

type UpdateHandler = func(context.Context, httpendpoints.UpdateRequest) (httpdto.UserResponse, error)

// doc-example:begin middleware-scope
const RouteTrace foundryhttp.MiddlewareID = "app.route-trace"

var trace = foundryhttp.DefineMiddleware(RouteTrace, func(next stdhttp.Handler) (stdhttp.Handler, error) {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if matched, ok := foundryhttp.MatchedRoute(r.Context()); ok {
			w.Header().Set("X-Matched-Route", string(matched.ID))
		}
		next.ServeHTTP(w, r)
	}), nil
})

var API = foundryhttp.DefineScope("/api", "api").WithMiddleware(trace)

func Router(update UpdateEndpoint, handle UpdateHandler) (*foundryhttp.Router, error) {
	scoped := foundryhttp.ConfigureEndpoint(update, foundryhttp.Within(API))
	return foundryhttp.NewRouter(scoped.Handle(handle))
}

// doc-example:end middleware-scope

var Update = foundryhttp.ConfigureEndpoint(httpendpoints.Update, foundryhttp.Within(API))

// doc-example:begin middleware-route
func Probe(policy foundryhttp.Middleware) foundryhttp.RouteRegistration {
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{
		ID: "probe", Method: foundryhttp.GET, Access: foundryhttp.Public,
	}, foundryhttp.StaticPath("/probe"))
	route = foundryhttp.ConfigureRoute(route, foundryhttp.Use(policy))

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
