package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var invalid = models.Users.ID.Eq(model.ID[models.Order]{})
