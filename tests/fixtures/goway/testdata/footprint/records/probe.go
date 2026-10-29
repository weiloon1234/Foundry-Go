package records

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type tag struct{}

func Model(ctx context.Context, db *database.DB) ([]models.Order, error) {
	a := rows.AsRecord[tag](rows.ModelRecord(models.Orders.Query().Limit(5)), "orders")
	return rows.ReadRecord(rows.SelectRecord(a, rows.Scope(a)).Where(expr.At(rows.Scope(a), orm.NumberExpr(models.Orders.TotalCents)).Eq(0))).All(ctx, db)
}
func Projected(ctx context.Context, db *database.DB) ([]reporting.OrderRow, error) {
	a := rows.AsRecord[tag](rows.RecordOf(reporting.SelectOrders(1)), "report")
	return rows.ReadRecord(rows.SelectRecord(a, rows.Scope(a)).OrderBy(rows.At(rows.Scope(a), reporting.OrderRows.Amount).Asc()).Limit(5)).All(ctx, db)
}
