package authenticating

import (
	"context"
	stdhttp "net/http"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

func signedBoundOrder(endpoint http.Endpoint[OrderPath, http.NoQuery, http.NoBody, http.NoContent], authentication *http.Authentication, api auth.Guard[models.User], signer http.URLSigner, orders modelbinding.Resolver[OrderPath, models.Order]) http.RouteRegistration {
	// doc-example:begin signed-bound-order
	secured := http.RequireAuthentication(endpoint, authentication, api)
	secured = http.ConfigureAuthenticated(secured, http.Permissions(ViewAccount))
	signed := http.SignedAuthenticated(secured, signer)
	binding := http.ForSignedAuthenticatedBinding(signed)
	bound := modelbinding.BindAuthenticated(binding, orders)
	registration := bound.Handle(func(ctx context.Context, user models.User,
		in modelbinding.Input[OrderPath, http.NoQuery, http.NoBody, models.Order],
	) (http.NoContent, error) {
		return http.NoContent{}, ReadOrder.Authorize(ctx, api, in.Model)
	})
	// doc-example:end signed-bound-order
	return registration
}

func authenticatedRawRoute(route http.Route[OrderPath], authentication *http.Authentication, api auth.Guard[models.User], handle func(stdhttp.ResponseWriter, *stdhttp.Request, models.User, OrderPath)) http.RouteRegistration {
	// doc-example:begin authenticated-raw-route
	secured := http.RequireRouteAuthentication(route, authentication, api)
	secured = http.ConfigureAuthenticatedRoute(secured, http.Permissions(ViewAccount))
	registration := http.HandleAuthenticatedRaw(secured, func(w stdhttp.ResponseWriter,
		r *stdhttp.Request, user models.User, path OrderPath,
	) {
		handle(w, r, user, path)
	})
	// doc-example:end authenticated-raw-route
	return registration
}

func actorMiddlewareOrder(endpoint http.Endpoint[http.NoPath, http.NoQuery, http.NoBody, http.NoContent], transport *http.Authentication, api auth.Guard[models.User], store *ratelimit.Store, tenantOf func(context.Context, models.User) (TenantID, error)) (http.AuthenticatedEndpoint[http.NoPath, http.NoQuery, http.NoBody, models.User, http.NoContent], error) {
	// doc-example:begin actor-middleware-order
	tenant := http.DefineMiddleware("app.tenant", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			user, err := api.Require(r.Context())
			if err != nil {
				http.WriteError(w, r, err)
				return
			}
			tenantID, err := tenantOf(r.Context(), user)
			if err != nil {
				http.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithTenant(r.Context(), tenantID)))
		}), nil
	})
	limiter, err := ratelimit.Define("api.users", http.ActorKeys(), ratelimit.PerMinute(120)).Bind(store)
	if err != nil {
		return http.AuthenticatedEndpoint[http.NoPath, http.NoQuery, http.NoBody, models.User, http.NoContent]{}, err
	}
	secured := http.RequireAuthentication(endpoint, transport, api,
		http.ActorMiddleware[models.User](http.RateLimitByActor(limiter, api), tenant))
	// doc-example:end actor-middleware-order
	return secured, nil
}

// doc-example:begin tenant-request-metadata
// TenantID identifies the tenant selected by application authorization.
type TenantID string

type tenantContextKey struct{}

// WithTenant attaches the selected tenant to this request.
func WithTenant(ctx context.Context, tenant TenantID) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenant)
}

// doc-example:end tenant-request-metadata
