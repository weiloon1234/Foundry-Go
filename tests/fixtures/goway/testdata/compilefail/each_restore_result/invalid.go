package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type A struct{ ID int }
type B struct{ ID int }

func invalid(ctx context.Context, db *database.DB, q orm.Query[A], soft orm.SoftDeletable[A], foreign orm.SoftDeletable[B]) {
	items, _ := orm.RestoreEach(ctx, db, q, soft, 10)
	var wrong []B = items
	_ = wrong
}
