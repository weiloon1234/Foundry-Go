package authenticating

import (
	"context"
	stdhttp "net/http"

	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

func signedBoundOrder(endpoint foundryhttp.Endpoint[OrderPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent], binding foundryhttp.GuardBinding[models.User], signer foundryhttp.URLSigner, orders modelbinding.Resolver[OrderPath, models.Order]) foundryhttp.RouteRegistration {
	// doc-example:begin signed-bound-order
	secured := foundryhttp.Authenticated(endpoint, binding, foundryhttp.AllOf(foundryhttp.Permissions(ViewAccount)))
	signed := foundryhttp.SignedAuthenticated(secured, signer)
	transport := foundryhttp.ForSignedAuthenticatedBinding(signed)
	bound := modelbinding.BindAuthenticated(transport, orders)
	registration := bound.Handle(func(ctx context.Context, user models.User,
		in modelbinding.Input[OrderPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.Order],
	) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, ReadOrder.Authorize(ctx, binding.Guard(), in.Model)
	})
	// doc-example:end signed-bound-order
	return registration
}

func authenticatedRawRoute(route foundryhttp.Route[OrderPath], binding foundryhttp.GuardBinding[models.User], handle func(stdhttp.ResponseWriter, *stdhttp.Request, models.User, OrderPath)) foundryhttp.RouteRegistration {
	// doc-example:begin authenticated-raw-route
	secured := foundryhttp.RouteAuthenticated(route, binding, foundryhttp.AllOf(foundryhttp.Permissions(ViewAccount)))
	registration := foundryhttp.HandleAuthenticatedRaw(secured, func(w stdhttp.ResponseWriter,
		r *stdhttp.Request, user models.User, path OrderPath,
	) {
		handle(w, r, user, path)
	})
	// doc-example:end authenticated-raw-route
	return registration
}

func actorMiddlewareOrder(endpoint foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, foundryhttp.NoContent], binding foundryhttp.GuardBinding[models.User], store *ratelimit.Store, tenantOf func(context.Context, models.User) (TenantID, error)) (ProfileEndpoint, error) {
	// doc-example:begin actor-middleware-order
	tenant := foundryhttp.DefineMiddleware("app.tenant", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			user, err := binding.Guard().Require(r.Context())
			if err != nil {
				foundryhttp.WriteError(w, r, err)
				return
			}
			tenantID, err := tenantOf(r.Context(), user)
			if err != nil {
				foundryhttp.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithTenant(r.Context(), tenantID)))
		}), nil
	})
	limiter, err := ratelimit.Define("api.users", foundryhttp.ActorKeys(), ratelimit.PerMinute(120)).Bind(store)
	if err != nil {
		return ProfileEndpoint{}, err
	}
	secured := foundryhttp.Authenticated(endpoint, binding, foundryhttp.AllOf(foundryhttp.Permissions(ViewAccount)),
		foundryhttp.ActorMiddleware[models.User](foundryhttp.RateLimitByActor(limiter, binding.Guard()), tenant))
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
