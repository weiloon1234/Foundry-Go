// Package authenticating verifies model-first framework usage, not a login app.
package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type UserProvider = auth.Provider[models.User, model.ID[models.User]]
type UserStrategy = auth.Strategy[models.User, model.ID[models.User]]
type ProfileInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]
type ProfileEndpoint = foundryhttp.AuthenticatedEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.User, foundryhttp.NoContent]
type OptionalProfileEndpoint = foundryhttp.OptionalAuthenticationEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, models.User, foundryhttp.NoContent]

// Users reuses the generated stored primary-key codec and ordinary typed query.
// No Actor hydration, key string conversion or custom persistence schema exists.
func Users(executor database.Executor) UserProvider {
	return auth.DefineProvider("users", (models.User{}).FoundryReference(), func(ctx context.Context, id model.ID[models.User]) (value.Optional[models.User], error) {
		return models.QueryUsers().Find(ctx, executor, id)
	}, func(_ context.Context, user models.User) (bool, error) {
		return user.Status == models.StatusActive, nil
	})
}

var ReadOrder = auth.DefinePolicy("orders.read", func(_ context.Context, user models.User, order models.Order) (bool, error) {
	return order.BuyerID == user.ID, nil
})

// AccountGuards shows two independently verified strategies sharing one model
// provider. Built-in session persistence is demonstrated in sessions.go; these
// arguments are verified strategy adapters, not a request-level credential bypass.
func AccountGuards(provider UserProvider, token UserStrategy, session UserStrategy) (*auth.Registry, auth.Guard[models.User], auth.Guard[models.User], error) {
	api := auth.DefineGuard("users.api", provider, token)
	web := auth.DefineGuard("users.web", provider, session)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), api.Registration(), web.Registration(), ReadOrder.Registration(), ViewAccount.Registration())
	return registry, api, web, err
}

func CurrentUser(ctx context.Context, guard auth.Guard[models.User]) (models.User, error) {
	return guard.Require(ctx)
}
func CheckOrder(ctx context.Context, guard auth.Guard[models.User], order models.Order) error {
	return ReadOrder.Authorize(ctx, guard, order)
}
func RestoreIdentity(provider UserProvider, identity model.Identity) (model.Reference[models.User, model.ID[models.User]], error) {
	return provider.Parse(identity)
}

func Profile(transport *foundryhttp.Authentication, guard auth.Guard[models.User]) ProfileEndpoint {
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "profile", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/profile")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	return foundryhttp.RequireAuthentication(endpoint, transport, guard)
}
func ProfileRoute(endpoint ProfileEndpoint, guard auth.Guard[models.User]) foundryhttp.RouteRegistration {
	return endpoint.Handle(func(ctx context.Context, user models.User, _ ProfileInput) (foundryhttp.NoContent, error) {
		// Business code receives the concrete model. Reusing a guard in a service or
		// policy keeps this request's already resolved user; no second query runs.
		again, err := guard.Require(ctx)
		if err != nil {
			return foundryhttp.NoContent{}, err
		}
		return foundryhttp.NoContent{}, ReadOrder.Authorize(ctx, guard, models.Order{BuyerID: again.ID, ID: model.ID[models.Order]{}})
	})
}
