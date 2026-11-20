package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var invalid = orm.Eq(models.Users.ID, model.ID[models.Order]{})
