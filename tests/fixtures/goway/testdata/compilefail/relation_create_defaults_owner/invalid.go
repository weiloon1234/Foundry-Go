package invalid

import (
	"context"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(ctx context.Context, db *database.DB) {
	defaults := []orm.Assignment[j.Parent]{j.Parents.Name.Set("wrong")}
	_, _ = orm.AttachMany(ctx, db, j.Peers, j.Parent{}, []j.Target{}, defaults...)
}
