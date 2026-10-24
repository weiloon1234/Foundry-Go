package transactionjoins

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type a struct{}
type b struct{}

func Read(ctx context.Context, tx *database.Tx) ([]value.Nullable[int64], error) {
	left := rows.AsTransaction[a](rows.LockedRecord(models.Orders.Query().ForUpdate()), "a")
	right := rows.AsTransaction[b](rows.TransactionOf(rows.ModelRecord(models.Orders.Query())), "b")
	ls, rs := rows.TransactionScope(left), rows.TransactionScope(right)
	on := rows.On(expr.At(ls, models.Orders.BuyerID), expr.At(rs, models.Orders.BuyerID))
	j := rows.TransactionFullJoin(left, right, on)
	x := expr.AtNullable(rows.NullableRightScope(j, rs), models.Orders.TotalCents)
	scalar := rows.SelectTransactionValue(right, expr.Select(expr.At(rs, models.Orders.TotalCents))).Limit(1)
	return rows.SelectTransactionValue(j, expr.Select(x)).Where(rows.TransactionExistsQuery(j, rows.TransactionScalarOf(scalar).ForShare())).All(ctx, tx)
}
func Records(ctx context.Context, tx *database.Tx) ([]models.Order, error) {
	left := rows.AsTransaction[a](rows.TransactionOf(rows.ModelRecord(models.Orders.Query())), "a")
	right := rows.AsTransaction[b](rows.TransactionOf(rows.ModelRecord(models.Orders.Query())), "b")
	j := rows.TransactionCrossJoin(left, right)
	return rows.SelectTransactionRecord(j, rows.LeftScope(j, rows.TransactionScope(left))).All(ctx, tx)
}
