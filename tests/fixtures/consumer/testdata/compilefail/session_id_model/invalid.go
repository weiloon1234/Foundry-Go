package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(ctx context.Context, sessions *authenticating.UserSessions, ref model.Reference[models.User, model.ID[models.User]], id session.ID[models.Group]) {
	_, _ = sessions.RevokeID(ctx, ref, id)
}
