package guideadapters

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

// doc-example:begin database-rules
func availableEmail(db database.Executor) validation.Rule[string] {
	return databasevalidation.Unique(db, models.Users.Query(), models.Users.Email)
}

func activeUsers(db database.Executor) validation.Rule[[]model.ID[models.User]] {
	return databasevalidation.ExistsAll(db,
		models.Users.Query().Where(orm.Eq(models.Users.Status, models.StatusActive)),
		models.Users.Key())
}

// doc-example:end database-rules

// doc-example:begin validation-email-change
type EmailChange struct {
	ID    model.ID[models.User]
	Email string
}

func updateRules(db database.Executor) validation.Rule[EmailChange] {
	current := validation.NewSlot[model.ID[models.User]]()
	email := databasevalidation.UniqueIgnoring(db, models.Users.Query(), models.Users.Email, models.Users.Key(), current)
	field := validation.DefineField("email", func(in EmailChange) string { return in.Email })
	return validation.Provide(current,
		func(_ context.Context, in EmailChange) (model.ID[models.User], error) { return in.ID, nil },
		validation.Rules(field, email))
}

// doc-example:end validation-email-change

// doc-example:begin validation-expanded-assignment
type Assignment struct{ Assignee model.ID[models.User] }

func assignees(db database.Executor) validation.Rule[[]Assignment] {
	field := validation.DefineField("assignee_id", func(a Assignment) model.ID[models.User] { return a.Assignee })
	return databasevalidation.ExistsEach(db,
		models.Users.Query().Where(orm.Eq(models.Users.Status, models.StatusActive)),
		models.Users.Key(), field)
}

// doc-example:end validation-expanded-assignment
