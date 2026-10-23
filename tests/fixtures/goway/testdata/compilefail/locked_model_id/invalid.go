package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _, _ = models.Users.Query().Where(models.Users.ID.Eq(model.ID[models.Order]{})).ForUpdate().NoWait().Require(nil, nil)
