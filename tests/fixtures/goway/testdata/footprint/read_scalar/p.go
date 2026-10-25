package read_scalar

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type tag struct{}

func Run(ctx context.Context, ex database.Executor, yield func(int64) error) error {
	q := rows.ReadScalar(rows.SelectScalar(rows.From(models.Orders.Query()), expr.Select(expr.Value(models.Orders.TotalCents))))
	if _, err := rows.CompileRead(q); err != nil {
		return err
	}
	if err := rows.PreflightRead(ctx, q); err != nil {
		return err
	}
	return rows.EachRead(ctx, ex, q, yield)
}
