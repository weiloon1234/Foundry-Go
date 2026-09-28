package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth/token"
)

func bad(ctx context.Context, tokens *authenticating.UserTokens, user models.User, id token.ID[models.Group]) {
	_, _ = tokens.RevokeID(ctx, user.FoundryReference(), id)
}
