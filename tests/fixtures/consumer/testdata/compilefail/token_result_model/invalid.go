package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func bad(ctx context.Context, tokens *authenticating.UserTokens, raw secret.String) {
	var wrong token.Issued[models.Group, model.ID[models.Group]]
	wrong, _ = tokens.Refresh(ctx, raw)
	_ = wrong
}
