package invalid

import (
	"context"
	"foundry.test/goway/models"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var db *database.DB
var _, _ = orm.UpdateOrCreate(context.Background(), db, h.Members.Query(), []orm.Assignment[models.Order]{}, func(context.Context, *database.Tx, h.Member) ([]orm.Assignment[h.Member], error) { return nil, nil })
