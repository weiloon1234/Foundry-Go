package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func bad(ctx context.Context, tokens *authenticating.UserTokens, user models.User, id token.ID[models.Group]) {
	_, _ = tokens.RevokeID(ctx, orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf(user), id)
}
