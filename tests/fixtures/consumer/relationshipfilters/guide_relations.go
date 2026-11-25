package relationshipfilters

import (
	"context"

	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// doc-example:begin model-relations-1
var introducer = orm.BelongsToOf(models.Users.Introducer, models.Users.IntroducerID, models.Users.ID)
var orders = orm.HasManyOf(models.Users.Orders, models.Users.ID, models.Orders.BuyerID)

// doc-example:end model-relations-1

func eagerUsers(ctx context.Context, db database.Executor) ([]models.User, error) {
	// doc-example:begin model-relations-2
	return orm.All(ctx, db, models.Users.Query().With(models.UserLinks.Introducer,
		models.UserLinks.Orders.OrderBy(orm.Desc(models.Orders.TotalCents))))
	// doc-example:end model-relations-2
}

func activeIntroducers(ctx context.Context, db database.Executor) ([]models.User, error) {
	// doc-example:begin model-relations-6
	introducer := models.UserLinks.Introducer.
		Where(orm.Eq(models.Users.Status, models.StatusActive)).With(models.UserLinks.Introducer)
	return orm.All(ctx, db, models.Users.Query().With(introducer))
	// doc-example:end model-relations-6
}

func orderedMemberships(ctx context.Context, db database.Executor) ([]models.User, error) {
	// doc-example:begin model-relations-7
	groups := models.UserLinks.Groups.
		Where(orm.Eq(models.Groups.Name, "same")).
		WherePivot(orm.Gt(models.Memberships.Priority, 0)).
		OrderByPivot(orm.Asc(models.Memberships.Priority)).
		OrderBy(orm.Desc(models.Groups.Code)).
		With(models.GroupLinks.Owner).WithPivot(models.MembershipLinks.Inviter)
	return orm.All(ctx, db, models.Users.Query().With(groups))
	// doc-example:end model-relations-7
}
