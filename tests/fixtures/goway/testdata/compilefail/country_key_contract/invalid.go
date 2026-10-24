package invalid

import (
	"context"
	a "foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/extensions"
)

func invalid(ctx context.Context, store *extensions.Store, member a.Member) {
	_, _ = countries.Find(ctx, store, member.ID)
}
