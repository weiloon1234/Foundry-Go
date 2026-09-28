package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

var executor database.Executor
var _ = context.Background
var _ model.ID[models.Order]
var _ = databasevalidation.Exists[models.User, string](executor, models.QueryUsers(), models.CountryFields().Name)
