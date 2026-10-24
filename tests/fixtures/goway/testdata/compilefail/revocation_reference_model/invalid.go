package invalid

import (
	"context"
	a "foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(ctx context.Context, tx *database.Tx, sessions *session.Sessions[a.Member, model.ID[a.Member]], other a.Other) {
	sessions.RevokeAllIn(ctx, tx, a.OtherIdentity.ReferenceOf(other))
}
