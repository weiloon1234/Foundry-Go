package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
)

var OrderReadScope = auth.DefineAccessScope[models.User]("orders.read")

func OrderReadScopes() (auth.AccessScopes[models.User], error) {
	return auth.NewAccessScopes(OrderReadScope)
}

// VerifiedScopedUser is a strategy boundary. A token adapter must verify the
// persisted credential before supplying this proof; a submitted ID is not proof.
func VerifiedScopedUser(user models.User, grants auth.AccessScopes[models.User]) (auth.Proof[models.User, model.ID[models.User]], error) {
	return auth.NewScopedProof(user.FoundryReference(), auth.Authenticated, grants)
}

// ReadScopedOrder combines the credential ceiling with the current domain policy.
// Both checks reuse the request's concrete model; neither needs raw field names.
func ReadScopedOrder(ctx context.Context, guard auth.Guard[models.User], required auth.AccessScopes[models.User], order models.Order) (models.User, error) {
	user, err := guard.RequireScopes(ctx, required)
	if err != nil {
		return models.User{}, err
	}
	if err := ReadOrder.Authorize(ctx, guard, order); err != nil {
		return models.User{}, err
	}
	return user, nil
}

func ScopedProfile(endpoint ProfileEndpoint, required auth.AccessScopes[models.User]) ProfileEndpoint {
	return endpoint.WithScopes(required)
}

func GrantedScopeNames(grants auth.AccessScopes[models.User]) []auth.AccessScopeName {
	return grants.Names()
}
