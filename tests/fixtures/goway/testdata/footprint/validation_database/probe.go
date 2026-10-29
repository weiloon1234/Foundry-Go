package validation_database

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	dbvalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

func Rules(db database.Executor) (validation.Rule[string], validation.Rule[[]string]) {
	current := validation.NewSlot[model.ID[models.User]]()
	one := validation.All(dbvalidation.Unique(db, models.Users.Query(), orm.TextExpr(models.Users.Email)), dbvalidation.Exists(db, models.Users.Query(), orm.NullableTextExpr(models.Users.Nickname)), dbvalidation.UniqueIgnoring(db, models.Users.Query(), orm.TextExpr(models.Users.Email), orm.KeyExpr(models.Users.ID), current))
	scoped := dbvalidation.Scoped(db, func(context.Context) (orm.Query[models.User], error) { return models.Users.Query(), nil }, orm.TextExpr(models.Users.Email))
	many := validation.All(dbvalidation.ExistsAll(db, models.Users.Query(), orm.TextExpr(models.Users.Email)), dbvalidation.UniqueAll(db, models.Users.Query(), orm.TextExpr(models.Users.Email)), validation.ExistsAll[[]string](scoped))
	return one, many
}
