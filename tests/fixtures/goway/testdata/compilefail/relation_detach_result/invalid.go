package invalid

import (
	"context"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(ctx context.Context, db *database.DB) {
	rows, _ := orm.Detach(ctx, db, j.Peers, j.Parent{}, j.Target{})
	var wrong []j.Target = rows
	_ = wrong
}
