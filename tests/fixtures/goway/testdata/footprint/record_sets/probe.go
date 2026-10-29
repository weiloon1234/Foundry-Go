package record_sets

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Read(ctx context.Context, db *database.DB) ([]models.Order, error) {
	a, b := rows.ModelRecord(models.Orders.Query().Limit(5)), rows.ModelRecord(models.Orders.Query().Where(orm.NumberGt(models.Orders.TotalCents, 1)))
	q := rows.Except(rows.UnionAll(a, b), rows.Intersect(a, b))
	return rows.ReadSet(q.Where(expr.At(q.Scope(), orm.NumberExpr(models.Orders.TotalCents)).Eq(0)).OrderBy(expr.At(q.Scope(), orm.NumberExpr(models.Orders.TotalCents)).Asc()).Limit(10)).All(ctx, db)
}
func Projected(ctx context.Context, db *database.DB) ([]reporting.OrderRow, error) {
	q := rows.Union(rows.RecordOf(reporting.SelectOrders(1)), rows.RecordOf(reporting.SelectOrders(2)))
	return rows.ReadSet(q.OrderBy(rows.At(q.Scope(), reporting.OrderRows.Amount).Asc())).All(ctx, db)
}
