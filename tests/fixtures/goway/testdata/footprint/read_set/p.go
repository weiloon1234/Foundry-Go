package read_set

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type tag struct{}

func Run(ctx context.Context, ex database.Executor, yield func(models.Order) error) error {
	r := rows.ModelRecord(models.Orders.Query())
	q := rows.ReadSet(rows.Union(r, r))
	if _, err := rows.CompileRead(q); err != nil {
		return err
	}
	if err := rows.PreflightRead(ctx, q); err != nil {
		return err
	}
	return rows.EachRead(ctx, ex, q, yield)
}
