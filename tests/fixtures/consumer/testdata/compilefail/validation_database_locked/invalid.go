package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/modelvalidation"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

var executor database.Executor
var _ = context.Background
var _ model.ID[models.Order]
var _ = databasevalidation.Unique[models.User, string](executor, modelvalidation.Users.Query().ForUpdate(), modelvalidation.Users.Email)
