package advancedqueries_test

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin locked-write
func incrementAge(ctx context.Context, db *database.DB, visible orm.Query[models.User], id model.ID[models.User]) error {
	return db.Transaction(ctx, func(tx *database.Tx) error {
		user, err := orm.RequireLocked(ctx, tx, orm.Lock(visible.Where(orm.KeyEq(models.Users.Key(), id))))
		if err != nil {
			return err
		}
		_, err = orm.Update(ctx, tx, visible, models.Users.Key(), user.ID, orm.NumberSet(models.Users.Age, user.Age+1))
		return err
	})
}

// doc-example:end locked-write

// doc-example:begin skip-locked
func claimUsers(ctx context.Context, tx *database.Tx, visible orm.Query[models.User]) ([]models.User, error) {
	return orm.AllLocked(ctx, tx, orm.Lock(visible.OrderBy(orm.KeyAsc(models.Users.Key())).Limit(20)).SkipLocked())
}

// doc-example:end skip-locked

type lockedUserAlias struct{}
type lockedOrderAlias struct{}

// doc-example:begin lock-joined-record
func lockBuyers(ctx context.Context, tx *database.Tx) ([]models.User, error) {
	users := rows.As[lockedUserAlias](models.Users.Query(), "u")
	orders := rows.As[lockedOrderAlias](models.Orders.Query(), "o")
	joined := rows.LeftJoin(users, orders,
		rows.On(expr.At(rows.Scope(users), orm.KeyExpr(models.Users.Key())),
			expr.At(rows.Scope(orders), orm.ColumnExpr(models.Orders.BuyerID))))
	userScope := rows.LeftScope(joined, rows.Scope(users))
	return rows.ForUpdate(rows.ReadRecord(rows.SelectRecord(joined, userScope))).Of(rows.Target(userScope)).All(ctx, tx)
}

// doc-example:end lock-joined-record

// doc-example:begin independent-lock-policies
func lockBuyerOrders(ctx context.Context, tx *database.Tx) ([]models.User, error) {
	users := rows.As[lockedUserAlias](models.Users.Query(), "u")
	orders := rows.As[lockedOrderAlias](models.Orders.Query(), "o")
	joined := rows.InnerJoin(users, orders,
		rows.On(expr.At(rows.Scope(users), orm.KeyExpr(models.Users.Key())),
			expr.At(rows.Scope(orders), orm.ColumnExpr(models.Orders.BuyerID))))
	userScope := rows.LeftScope(joined, rows.Scope(users))
	orderScope := rows.RightScope(joined, rows.Scope(orders))
	locked := rows.LockRows(rows.ReadRecord(rows.SelectRecord(joined, userScope)),
		rows.UpdateLock(rows.Target(userScope)).NoWait(),
		rows.ShareLock(rows.Target(orderScope)).SkipLocked())
	return locked.All(ctx, tx)
}

// doc-example:end independent-lock-policies
