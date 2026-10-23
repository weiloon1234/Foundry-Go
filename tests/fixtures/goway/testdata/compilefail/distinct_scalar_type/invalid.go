package distinct_scalar_type

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ rows.Query[models.User, model.ID[models.User]] = rows.Distinct(rows.SelectValue(models.Users.Query(), models.Users.Email))
