package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/metadata"
)

func invalid(manager *metadata.Manager) {
	key := metadata.Define(a.Owner, "seo", 1, contract.StringJSON[string]())
	_ = key.Set(context.Background(), manager, a.Owner.Reference(1), 42)
}
