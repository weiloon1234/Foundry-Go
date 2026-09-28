package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func bad(ctx context.Context, guard auth.Guard[models.User], required auth.AccessScopes[models.Group]) {
	_, _ = guard.RequireScopes(ctx, required)
}
