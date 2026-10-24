package invalid

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

var executor database.Executor
var _ = databasevalidation.ExistsAll(executor, models.Users.Query(), models.Users.ID).Check(context.Background(), []model.ID[models.Order]{}, validation.DefaultLimits())
