package report_reads

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Read(ctx context.Context, ex database.Executor, yield func(int64) error) error {
	q := rows.ReorderUnwindowed(rows.SelectValue(models.Orders.Query(), models.Orders.TotalCents), expr.Value(models.Orders.ID).Asc())
	if _, err := rows.Compile(q); err != nil {
		return err
	}
	if err := rows.Preflight(ctx, q); err != nil {
		return err
	}
	if _, err := rows.Count(ctx, ex, q); err != nil {
		return err
	}
	if _, err := rows.Paginate(ctx, ex, q, orm.Page(1, 20)); err != nil {
		return err
	}
	if _, err := rows.SimplePaginate(ctx, ex, q, orm.Page(1, 20)); err != nil {
		return err
	}
	return rows.Each(ctx, ex, q, yield)
}
