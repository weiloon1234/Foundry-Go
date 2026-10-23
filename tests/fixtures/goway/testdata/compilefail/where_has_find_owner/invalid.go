package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var id model.ID[models.Order]
var _, _ = models.Users.Query().Where(models.Users.ID.Eq(id)).Require(nil, nil)
