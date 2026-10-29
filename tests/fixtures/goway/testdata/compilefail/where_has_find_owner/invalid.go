package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var id model.ID[models.Order]
var _, _ = orm.Require(nil, nil, models.Users.Query().Where(orm.KeyEq(models.Users.ID, id)))
