package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/http"
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
// doc-example:begin verified-scoped-user
func VerifiedScopedUser(user models.User, grants auth.AccessScopes[models.User]) (auth.Proof[models.User, model.ID[models.User]], error) {
	return auth.NewScopedProof(user.FoundryReference(), auth.Authenticated, grants)
}

// doc-example:end verified-scoped-user

// ReadScopedOrder combines the credential ceiling with the current domain policy.
// Both checks reuse the request's concrete model; neither needs raw field names.
// doc-example:begin read-scoped-order
func ReadScopedOrder(ctx context.Context, api auth.Guard[models.User], readOrder auth.Policy[models.User, models.Order], required auth.AccessScopes[models.User], order models.Order) (models.User, error) {
	user, err := api.RequireScopes(ctx, required)
	if err != nil {
		return models.User{}, err
	}
	if err := readOrder.Authorize(ctx, api, order); err != nil {
		return models.User{}, err
	}
	return user, nil
}

// doc-example:end read-scoped-order

// doc-example:begin configure-scopes
func ScopedProfile(endpoint http.AuthenticatedEndpoint[http.NoPath, http.NoQuery, http.NoBody, models.User, http.NoContent], required auth.AccessScopes[models.User]) http.AuthenticatedEndpoint[http.NoPath, http.NoQuery, http.NoBody, models.User, http.NoContent] {
	return http.ConfigureAuthenticated(endpoint, http.Scopes(required))
}

// doc-example:end configure-scopes

func GrantedScopeNames(grants auth.AccessScopes[models.User]) []auth.AccessScopeName {
	return grants.Names()
}
