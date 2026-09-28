package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(ctx context.Context, sessions *authenticating.UserSessions, proof auth.Proof[models.Group, model.ID[models.Group]]) {
	_, _ = sessions.Issue(ctx, proof, session.IssueOptions{})
}
