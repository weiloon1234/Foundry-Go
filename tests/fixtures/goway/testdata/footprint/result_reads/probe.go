package result_reads

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database/orm"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type alias struct{}

func Page(ctx context.Context, db database.Executor) (orm.PageResult[int64], error) {
	return rows.Read(rows.SelectValue(models.Orders.Query(), orm.NumberExpr(models.Orders.TotalCents))).OrderBy(expr.Asc(expr.Value(orm.KeyExpr(models.Orders.ID)))).Paginate(ctx, db, orm.PageRequest{Number: 1, Size: 10})
}
func Lock(ctx context.Context, tx *database.Tx) ([]int64, error) {
	a := rows.AsTransaction[alias](rows.TransactionOf(rows.ModelRecord(models.Orders.Query())), "orders_alias")
	q := rows.SelectTransactionValue(a, expr.AsSelected(expr.Row(expr.At(rows.TransactionScope(a), orm.NumberExpr(models.Orders.TotalCents)))))
	return rows.ForUpdate(q).Of(rows.Target(rows.TransactionScope(a))).SkipLocked().Limit(10).All(ctx, tx)
}
