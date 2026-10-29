package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _, _ = orm.RequireLocked(nil, nil, orm.Lock(models.Users.Query().Where(orm.KeyEq(models.Users.ID, model.ID[models.Order]{}))).NoWait())
