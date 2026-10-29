package lifecycle_write

import (
	"context"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Update(ctx context.Context, db *database.DB) (h.Member, error) {
	return orm.Update(ctx, db, h.Members.Table.Query(), h.Members.Table.Key(), 1, orm.InputTextSet(h.Members.Name, "name"))
}
func Register(observer orm.Observer[h.Member]) {
	_, _ = observer.Declare(func() orm.Hooks[h.Member] { return h.LocalHooks() })
}
