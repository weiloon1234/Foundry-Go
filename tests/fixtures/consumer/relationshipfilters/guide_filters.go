package relationshipfilters

import (
	"context"

	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// doc-example:begin model-relationship-filters-1
func buyersAndOthers(ctx context.Context, db database.Executor) error {
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
	_ = buyers
	_ = others
	return err
}

// doc-example:end model-relationship-filters-1

// doc-example:begin model-relationship-filters-2
func buyersOrActive(ctx context.Context, db database.Executor, orders orm.HasMany[models.User, models.Order]) ([]models.User, error) {
	u := models.Users
	users, err := orm.All(ctx, db, models.Users.Query().Where(orm.Or(
		orm.Has(orders),
		orm.Eq(u.Status, models.StatusActive),
	)))
	return users, err
}

// doc-example:end model-relationship-filters-2

// doc-example:begin model-relationship-filters-3
func referralBuyers(ctx context.Context, db database.Executor) ([]models.User, error) {
	referralsWithOrders := models.UserLinks.Referrals.
		Where(orm.Has(models.UserLinks.Orders.
			Where(orm.Gte(models.Orders.TotalCents, 1000))))

	introducers, err := orm.All(ctx, db, models.Users.Query().Where(orm.Has(referralsWithOrders)))
	return introducers, err
}

// doc-example:end model-relationship-filters-3

// doc-example:begin model-relationship-filters-4
func teamMembers(ctx context.Context, db database.Executor) ([]models.User, error) {
	groups := models.UserLinks.Groups.
		Where(orm.Like(models.Groups.Name, "team%")).
		WherePivot(orm.Gte(models.Memberships.Priority, 2))

	members, err := orm.All(ctx, db, models.Users.Query().Where(orm.Has(groups)))
	return members, err
}

// doc-example:end model-relationship-filters-4

// doc-example:begin model-relationship-filters-5
func loadedBuyers(ctx context.Context, db database.Executor, orders orm.HasMany[models.User, models.Order]) ([]models.User, error) {
	users, err := orm.All(ctx, db, models.Users.Query().Where(orm.Has(orders)).With(orders))
	return users, err
}

// doc-example:end model-relationship-filters-5
