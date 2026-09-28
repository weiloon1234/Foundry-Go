package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func bad(ctx context.Context, p auth.Policy[models.User, models.Order], g auth.Guard[models.User], user models.User) {
	_ = p.Authorize(ctx, g, user)
}
