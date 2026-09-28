package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func bad(ctx context.Context, g auth.Guard[models.User]) {
	var group models.Group
	group, _ = g.Require(ctx)
	_ = group
}
