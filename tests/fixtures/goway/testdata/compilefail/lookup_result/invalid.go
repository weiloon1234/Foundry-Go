package invalid

import (
	"context"
	"foundry.test/goway/models"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var db *database.DB
var row models.Order

func invalid() { row, _ = orm.FirstOrCreate(context.Background(), db, h.Members.Query()) }
