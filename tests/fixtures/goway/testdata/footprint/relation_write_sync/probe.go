package relation_write_sync

import (
	"context"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F(ctx context.Context, db database.Transactor, p j.Parent, t []j.Target) (orm.PivotChanges[j.Pivot], error) {
	return orm.Sync(ctx, db, j.Peers, p, t, []orm.Assignment[j.Pivot]{orm.KeySet(j.Pivots.ID, 100), orm.ColumnSet(j.Pivots.Kind, "parent"), orm.TextSet(j.Pivots.Role, "member")}, nil)
}
