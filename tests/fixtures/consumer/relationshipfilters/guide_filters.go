package relationshipfilters

import (
	"context"

	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func buyersAndOthers(ctx context.Context, db database.Executor) error {
	// doc-example:begin model-relationship-filters-1
	orders := models.UserLinks.Orders.
		Where(orm.Gte(models.Orders.TotalCents, 1000))

	buyers, err := orm.All(ctx, db, models.Users.Query().Where(orm.Has(orders)))
	if err != nil {
		return err
	}
	others, err := orm.All(ctx, db, models.Users.Query().Where(orm.Not(orm.Has(orders))))
	if err != nil {
		return err
	}
	// doc-example:end model-relationship-filters-1
	_ = buyers
	_ = others
	return err
}

func buyersOrActive(ctx context.Context, db database.Executor, orders orm.HasMany[models.User, models.Order]) ([]models.User, error) {
	// doc-example:begin model-relationship-filters-2
	u := models.Users
	users, err := orm.All(ctx, db, models.Users.Query().Where(orm.Or(
		orm.Has(orders),
		orm.Eq(u.Status, models.StatusActive),
	)))
	// doc-example:end model-relationship-filters-2
	return users, err
}

func referralBuyers(ctx context.Context, db database.Executor) ([]models.User, error) {
	// doc-example:begin model-relationship-filters-3
	referralsWithOrders := models.UserLinks.Referrals.
		Where(orm.Has(models.UserLinks.Orders.
			Where(orm.Gte(models.Orders.TotalCents, 1000))))

	return orm.All(ctx, db, models.Users.Query().Where(orm.Has(referralsWithOrders)))
	// doc-example:end model-relationship-filters-3
}

func teamMembers(ctx context.Context, db database.Executor) ([]models.User, error) {
	// doc-example:begin model-relationship-filters-4
	groups := models.UserLinks.Groups.
		Where(orm.Like(models.Groups.Name, "team%")).
		WherePivot(orm.Gte(models.Memberships.Priority, 2))

	return orm.All(ctx, db, models.Users.Query().Where(orm.Has(groups)))
	// doc-example:end model-relationship-filters-4
}

func loadedBuyers(ctx context.Context, db database.Executor, orders orm.HasMany[models.User, models.Order]) ([]models.User, error) {
	// doc-example:begin model-relationship-filters-5
	return orm.All(ctx, db, models.Users.Query().Where(orm.Has(orders)).With(orders))
	// doc-example:end model-relationship-filters-5
}
