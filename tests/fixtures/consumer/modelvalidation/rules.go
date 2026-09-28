// Package modelvalidation exercises model-aware advisory validation through
// generated model fields. Applications inject a pool or existing transaction.
package modelvalidation

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

func AvailableEmail(executor database.Executor) validation.Rule[string] {
	return databasevalidation.Unique(executor, models.QueryUsers(), models.UserFields().Email)
}

func AvailableEmailForUpdate(executor database.Executor, current model.ID[models.User]) validation.Rule[string] {
	fields := models.UserFields()
	return databasevalidation.Unique(executor, models.QueryUsers().Where(fields.ID.Ne(current)), fields.Email)
}

func ActiveUser(executor database.Executor) validation.Rule[model.ID[models.User]] {
	fields := models.UserFields()
	return databasevalidation.Exists(executor, models.QueryUsers().Where(fields.Status.Eq(models.StatusActive)), fields.ID)
}

// ActiveUsers accepts concrete user IDs and checks the entire list in batches.
func ActiveUsers(executor database.Executor) validation.Rule[[]model.ID[models.User]] {
	fields := models.UserFields()
	return databasevalidation.ExistsAll(executor, models.QueryUsers().Where(fields.Status.Eq(models.StatusActive)), fields.ID)
}
