package invalid

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
var _ = context.Background
var _ model.ID[models.Order]
var _ = databasevalidation.Exists(executor, modelvalidation.Users.Query(), modelvalidation.Users.ID).Check(context.Background(), model.ID[models.Order]{}, validation.DefaultLimits())
