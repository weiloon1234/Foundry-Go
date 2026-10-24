package invalid

import (
	"context"
	"foundry.test/goway/models"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var db *database.DB
var _, _ = orm.CreateEach(context.Background(), db, h.Members.Query(), [][]orm.Assignment[models.Order]{})
