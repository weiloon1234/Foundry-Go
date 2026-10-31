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
	q := rows.ReorderUnwindowed(rows.SelectValue(models.Orders.Query(), orm.NumberExpr(models.Orders.TotalCents)), expr.Asc(expr.Value(orm.KeyExpr(models.Orders.ID))))
	if _, err := rows.CompileRead(rows.Read(q)); err != nil {
		return err
	}
	if err := rows.PreflightRead(ctx, rows.Read(q)); err != nil {
		return err
	}
	if _, err := rows.Read(q).Count(ctx, ex); err != nil {
		return err
	}
	if _, err := rows.Read(q).Paginate(ctx, ex, orm.Page(1, 20)); err != nil {
		return err
	}
	if _, err := rows.Read(q).SimplePaginate(ctx, ex, orm.Page(1, 20)); err != nil {
		return err
	}
	return rows.EachRead(ctx, ex, rows.Read(q), yield)
}
