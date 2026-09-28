package authenticating

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

// ViewAccount is a current model capability. A real domain may query its typed
// role relations in this callback; it never trusts a token claim as a live role.
var ViewAccount = auth.DefinePermission("accounts.view", func(_ context.Context, user models.User) (bool, error) {
	return user.Status == models.StatusActive, nil
})

func CheckAccountPermission(ctx context.Context, guard auth.Guard[models.User]) error {
	return ViewAccount.Authorize(ctx, guard)
}
func PermissionProfile(endpoint ProfileEndpoint) ProfileEndpoint {
	return endpoint.WithPermissions(ViewAccount)
}
