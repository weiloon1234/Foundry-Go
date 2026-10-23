package lifecycle_write

import (
	"context"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Update(ctx context.Context, db *database.DB) (h.Member, error) {
	return h.Members.Update(ctx, db, 1, h.Members.Name.Set("name"))
}
func Register(observer orm.Observer[h.Member]) {
	_, _ = observer.Declare(func() orm.Hooks[h.Member] { return h.LocalHooks() })
}
