package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin declare-scopes
var OrderReadScope = auth.DefineAccessScope[models.User]("orders.read")

func OrderReadScopes() (auth.AccessScopes[models.User], error) {
	return auth.NewAccessScopes(OrderReadScope)
}

// doc-example:end declare-scopes

// VerifiedScopedUser is a strategy boundary. A token adapter must verify the
// persisted credential before supplying this proof; a submitted ID is not proof.
func VerifiedScopedUser(user models.User, grants auth.AccessScopes[models.User]) (auth.Proof[models.User, model.ID[models.User]], error) {
	// doc-example:begin verified-scoped-user
	return auth.NewScopedProof(user.FoundryReference(), auth.Authenticated, grants)
	// doc-example:end verified-scoped-user
}

// ReadScopedOrder combines the credential ceiling with the current domain policy.
// Both checks reuse the request's concrete model; neither needs raw field names.
func ReadScopedOrder(ctx context.Context, api auth.Guard[models.User], readOrder auth.Policy[models.User, models.Order], required auth.AccessScopes[models.User], order models.Order) (models.User, error) {
	// doc-example:begin read-scoped-order
	user, err := api.RequireScopes(ctx, required)
	if err != nil {
		return models.User{}, err
	}
	// Then authorize the concrete resource with its current model policy.
	if err := readOrder.Authorize(ctx, api, order); err != nil {
		return models.User{}, err
	}
	// doc-example:end read-scoped-order
	return user, nil
}

// doc-example:begin configure-scopes
func ScopedProfile(endpoint ProfileDeclaration, binding foundryhttp.GuardBinding[models.User], required auth.AccessScopes[models.User]) ProfileEndpoint {
	return foundryhttp.Authenticated(endpoint, binding, foundryhttp.AllOf(foundryhttp.Scopes(required)))
}

// doc-example:end configure-scopes

func GrantedScopeNames(grants auth.AccessScopes[models.User]) []auth.AccessScopeName {
	return grants.Names()
}
