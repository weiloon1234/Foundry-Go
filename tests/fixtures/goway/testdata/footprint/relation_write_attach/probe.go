package relation_write_attach

import (
	"context"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F(ctx context.Context, db database.Transactor, p j.Parent, t j.Target) (j.Pivot, error) {
	return orm.Attach(ctx, db, j.Peers, p, t, orm.Set(j.Pivots.ID, 100), orm.Set(j.Pivots.Kind, "parent"), orm.Set(j.Pivots.Role, "member"))
}
