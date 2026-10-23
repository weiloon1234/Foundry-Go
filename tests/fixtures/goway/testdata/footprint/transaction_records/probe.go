package transactionrecords

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type tag struct{}
type summary struct{ Amount int64 }
type fields struct {
	rows.Row[summary]
	Amount rows.Field[summary, int64]
}

var schema = rows.Define(func(b rows.Builder[summary], r *summary) fields { return fields{b.Row(), rows.NumberOf(b, &r.Amount)} })

func Model(ctx context.Context, tx *database.Tx) ([]models.Order, error) {
	a := rows.AsTransaction[tag](rows.LockedRecord(models.Orders.Query().ForUpdate().SkipLocked()), "orders")
	return rows.SelectTransactionRecord(a, rows.TransactionScope(a)).Limit(5).All(ctx, tx)
}
func Projected(ctx context.Context, tx *database.Tx) ([]summary, error) {
	a := rows.AsTransaction[tag](rows.TransactionOf(rows.ModelRecord(models.Orders.Query())), "orders")
	return rows.SelectTransaction(a, schema, rows.Map(schema.Amount, expr.At(rows.TransactionScope(a), models.Orders.TotalCents))).All(ctx, tx)
}
