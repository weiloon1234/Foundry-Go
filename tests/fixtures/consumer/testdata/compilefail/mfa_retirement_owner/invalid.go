package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(ctx context.Context, tx *database.Tx, f *multifactor.Factors, u profiles.Profile) {
	_, _ = f.RetireIn(ctx, tx, profiles.ProfileIdentity.ReferenceOf(u))
}
