package read_record

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type tag struct{}

func Run(ctx context.Context, ex database.Executor, yield func(models.Order) error) error {
	a := rows.AsRecord[tag](rows.CTE("selected_orders", rows.ModelRecord(models.Orders.Query())), "o")
	q := rows.SelectRecord(a, rows.Scope(a))
	if _, err := rows.CompileRead(q); err != nil {
		return err
	}
	if err := rows.PreflightRead(ctx, q); err != nil {
		return err
	}
	return rows.EachRead(ctx, ex, q, yield)
}
