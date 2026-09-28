package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(ctx context.Context, tokens *authenticating.UserTokens, proof auth.Proof[models.Group, model.ID[models.Group]]) {
	_, _ = tokens.Issue(ctx, proof, token.IssueOptions[models.User]{})
}
