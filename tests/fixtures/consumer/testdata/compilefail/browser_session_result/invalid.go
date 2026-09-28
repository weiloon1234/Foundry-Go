package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(ctx context.Context, web *authenticating.Browser) {
	var wrong session.Info[models.Group, model.ID[models.Group]]
	wrong, _ = web.Rotate(ctx)
	_ = wrong
}
