package relation_write_attach

import (
	"context"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F(ctx context.Context, db database.Transactor, p j.Parent, t j.Target) (j.Pivot, error) {
	return orm.Attach(ctx, db, j.Peers, p, t, j.Pivots.ID.Set(100), j.Pivots.Kind.Set("parent"), j.Pivots.Role.Set("member"))
}
