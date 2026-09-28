package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = models.UserFields().ID.Param(model.ID[models.Order]{})
