package invalid

import (
	"context"
	a "foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(ctx context.Context, reset *passwordreset.Reset[a.Member, model.ID[a.Member]], other a.Other) {
	reset.Issue(ctx, a.OtherIdentity.ReferenceOf(other))
}
