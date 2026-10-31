package transactioncorrelation

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

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
type latest struct{}

func Read(ctx context.Context, tx *database.Tx) ([]value.Nullable[int64], error) {
	parent := rows.AsTransaction[a](rows.TransactionOf(rows.ModelRecord(models.Users.Query())), "parent")
	child := rows.AsTransaction[b](rows.TransactionOf(rows.ModelRecord(models.Orders.Query())), "child")
	c := rows.TransactionCorrelate(parent, child)
	outer, inner := rows.TransactionOuterScope(c, rows.TransactionScope(parent)), rows.TransactionInnerScope(c, rows.TransactionScope(child))
	c = c.Where(expr.Equal(expr.At(inner, orm.ColumnExpr(models.Orders.BuyerID)), expr.At(outer, orm.KeyExpr(models.Users.ID))))
	record := rows.SelectTransactionCorrelatedRecord(c, inner).OrderBy(expr.Desc(expr.At(inner, orm.NumberExpr(models.Orders.TotalCents)))).Limit(1).ForUpdate().Of(rows.Target(inner)).SkipLocked()
	lat := rows.AsTransactionLateral[latest](record, "latest")
	j := rows.TransactionLeftJoinLateral(parent, lat)
	return rows.SelectTransactionValue(j, expr.AsSelected(expr.Row(expr.AtNullable(rows.NullableRightScope(j, rows.TransactionLateralScope(lat)), orm.NumberExpr(models.Orders.TotalCents))))).All(ctx, tx)
}
func Scalar(ctx context.Context, tx *database.Tx) ([]value.Nullable[int64], error) {
	parent := rows.AsTransaction[a](rows.TransactionOf(rows.ModelRecord(models.Users.Query())), "parent")
	child := rows.AsTransaction[b](rows.TransactionOf(rows.ModelRecord(models.Orders.Query())), "child")
	c := rows.TransactionCorrelate(parent, child)
	inner := rows.TransactionInnerScope(c, rows.TransactionScope(child))
	selected := rows.SelectTransactionCorrelatedScalar(c, expr.AsSelected(expr.Row(expr.At(inner, orm.NumberExpr(models.Orders.TotalCents))))).Limit(1).ForShare()
	return rows.SelectTransactionValue(parent, expr.AsSelected(rows.TransactionCorrelatedScalarRowQuery(selected))).Where(selected.Exists()).All(ctx, tx)
}
