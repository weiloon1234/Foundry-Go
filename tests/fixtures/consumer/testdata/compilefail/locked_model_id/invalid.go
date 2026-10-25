package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _, _ = models.Users.Query().ForUpdate().NoWait().Where(models.Users.ID.Eq(model.ID[models.Order]{})).First(nil, nil)
