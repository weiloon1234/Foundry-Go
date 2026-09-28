package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

var scope auth.AccessScopeName = "accounts.view"
var _ = auth.DefinePermission(scope, func(context.Context, models.User) (bool, error) { return true, nil })
