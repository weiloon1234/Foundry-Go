package cte_recursive

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type tag struct{}

func Read(ctx context.Context, db *database.DB) ([]models.Order, error) {
	c := rows.RecursiveCTE("tree", rows.ModelRecord(models.Orders.Query()), func(self rows.RecursiveSelf[models.Order]) rows.RecordSource[models.Order] {
		a := rows.AsRecord[tag](self, "parent")
		return rows.SelectRecord(a, rows.Scope(a)).Where(expr.At(rows.Scope(a), models.Orders.TotalCents).Eq(0))
	})
	a := rows.AsRecord[tag](rows.CTE("dependent", c).Materialized(), "r")
	return rows.ReadRecord(rows.SelectRecord(a, rows.Scope(a)).Limit(5)).All(ctx, db)
}
func Locked(ctx context.Context, tx *database.Tx) ([]models.Order, error) {
	c := rows.TransactionCTE("claimed", rows.LockedRecord(models.Orders.Query().ForUpdate().SkipLocked()))
	a := rows.AsTransaction[tag](c, "claim")
	return rows.SelectTransactionRecord(a, rows.TransactionScope(a)).All(ctx, tx)
}
