package authenticating

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	http "github.com/weiloon1234/Foundry-Go/http"
)

// EditorialProfile pairs each credential scope set with its current permission.
// Both permission declarations must belong to the endpoint's guard registry.
// doc-example:begin editorial-alternatives
func EditorialProfile(endpoint ProfileEndpoint, writeScopes, approveScopes auth.AccessScopes[models.User], write, approve auth.Permission[models.User]) ProfileEndpoint {
	return http.ConfigureAuthenticated(endpoint, http.AnyOf(
		http.AllOf(http.Scopes(writeScopes), http.Permissions(write)),
		http.AllOf(http.Scopes(approveScopes), http.Permissions(approve)),
	))
}

// doc-example:end editorial-alternatives

// Existing exact function values can preserve their old signature with a wrapper.
var LegacyScopeOption func(auth.AccessScopes[models.User]) http.GuardOption[models.User] = func(required auth.AccessScopes[models.User]) http.GuardOption[models.User] {
	return http.Scopes(required)
}
