package relation_write_sync

import (
	"context"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F(ctx context.Context, db database.Transactor, p j.Parent, t []j.Target) (orm.PivotChanges[j.Pivot], error) {
	return orm.Sync(ctx, db, j.Peers, p, t, []orm.Assignment[j.Pivot]{orm.Set(j.Pivots.ID, 100), orm.Set(j.Pivots.Kind, "parent"), orm.Set(j.Pivots.Role, "member")}, nil)
}
