package authenticating

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// EditorialProfile pairs each credential scope set with its current permission.
// Both permission declarations must belong to the endpoint's guard registry.
// doc-example:begin editorial-alternatives
func EditorialProfile(endpoint ProfileEndpoint, writeScopes, approveScopes auth.AccessScopes[models.User], write, approve auth.Permission[models.User]) ProfileEndpoint {
	return foundryhttp.ConfigureAuthenticated(endpoint, foundryhttp.AnyOf(
		foundryhttp.AllOf(foundryhttp.Scopes(writeScopes), foundryhttp.Permissions(write)),
		foundryhttp.AllOf(foundryhttp.Scopes(approveScopes), foundryhttp.Permissions(approve)),
	))
}

// doc-example:end editorial-alternatives

// Existing exact function values can preserve their old signature with a wrapper.
var ExactScopeOption func(auth.AccessScopes[models.User]) foundryhttp.GuardOption[models.User] = func(required auth.AccessScopes[models.User]) foundryhttp.GuardOption[models.User] {
	return foundryhttp.Scopes(required)
}
