package distinct_scalar_type

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ rows.Query[models.User, model.ID[models.User]] = rows.Distinct(rows.SelectValue(models.Users.Query(), orm.TextExpr(models.Users.Email)))
