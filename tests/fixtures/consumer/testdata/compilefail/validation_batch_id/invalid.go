package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/modelvalidation"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

var executor database.Executor
var _ = validation.Check(databasevalidation.ExistsAll(executor, modelvalidation.Users.Query(), orm.KeyExpr(modelvalidation.Users.ID)), context.Background(), []model.ID[models.Order]{}, validation.DefaultLimits())
