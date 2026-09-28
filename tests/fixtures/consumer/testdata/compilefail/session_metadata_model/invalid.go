package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(ctx context.Context, sessions *authenticating.UserSessions) {
	var info session.Info[models.Group, model.ID[models.Group]]
	rows, _ := sessions.List(ctx, (models.User{}).FoundryReference())
	info = rows[0]
	_ = info
}
