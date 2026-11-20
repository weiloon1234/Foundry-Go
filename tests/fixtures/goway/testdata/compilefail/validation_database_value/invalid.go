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
var _ = context.Background
var _ model.ID[models.Order]
var _ = validation.Check(databasevalidation.Unique(executor, models.Users.Query(), models.Users.Email), context.Background(), 123, validation.DefaultLimits())
