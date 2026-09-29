// Package modelvalidation exercises model-aware advisory validation through
// generated model fields. Applications inject a pool or existing transaction.
package modelvalidation

import (
	"context"

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

// AvailableEmailExceptCurrent is declared once. An enclosing validation.Provide
// supplies the trusted key of the user being updated at check time.
func AvailableEmailExceptCurrent(executor database.Executor, current validation.Slot[model.ID[models.User]]) validation.Rule[string] {
	fields := models.UserFields()
	return databasevalidation.UniqueIgnoring(executor, models.QueryUsers(), fields.Email, fields.ID, current)
}

// UserInStatus derives its model scope from a slot at every check, the shape of
// a tenant- or actor-scoped lookup.
func UserInStatus(executor database.Executor, status validation.Slot[models.Status]) validation.Rule[model.ID[models.User]] {
	fields := models.UserFields()
	lookup := databasevalidation.Scoped(executor, func(ctx context.Context) (models.UserQuery, error) {
		selected, err := status.Value(ctx)
		return models.QueryUsers().Where(fields.Status.Eq(selected)), err
	}, fields.ID)
	return validation.Requires(status, validation.Exists(lookup))
}

// Assignment is one element of a bulk request body.
type Assignment struct{ Assignee model.ID[models.User] }

// ActiveAssignees checks every assignee with batched statements and reports a
// missing one at its element path, such as /2/assignee_id.
func ActiveAssignees(executor database.Executor) validation.Rule[[]Assignment] {
	fields := models.UserFields()
	assignee := validation.DefineField("assignee_id", func(item Assignment) model.ID[models.User] { return item.Assignee })
	return databasevalidation.ExistsEach(executor, models.QueryUsers().Where(fields.Status.Eq(models.StatusActive)), fields.ID, assignee)
}
