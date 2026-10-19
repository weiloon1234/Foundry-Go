package invalid

import (
	"foundry.test/consumer/models"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = foundryquery.Param(models.UserFields().ID, model.ID[models.Order]{})
