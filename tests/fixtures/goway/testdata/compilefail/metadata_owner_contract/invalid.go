package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/metadata"
)

func invalid(manager *metadata.Manager) {
	key := metadata.Define(a.Owner, "seo", 1, contract.StringJSON[string]())
	_, _ = key.Get(context.Background(), manager, orm.IdentityOf(a.Authors, a.Authors.Key()).Reference(1))
}
