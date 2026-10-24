package invalid

import (
	"context"
	a "foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(ctx context.Context, tx *database.Tx, f *mfa.Factors[a.Member, model.ID[a.Member]], other a.Other) {
	_, _ = f.RetireIn(ctx, tx, a.OtherIdentity.ReferenceOf(other))
}
