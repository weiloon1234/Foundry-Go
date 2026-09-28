package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(ctx context.Context, sessions *authenticating.UserSessions, ref model.Reference[models.Group, model.ID[models.Group]]) {
	_, _ = sessions.List(ctx, ref)
}
