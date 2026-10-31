package window_rank

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type result struct{ Rank int64 }
type fields struct {
	rows.Row[result]
	Rank rows.Field[result, int64]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields { return fields{b.Row(), rows.NumberOf(b, &r.Rank)} })

func Run(ctx context.Context, db database.Executor) ([]result, error) {
	w := expr.WindowFor(rows.ModelScope(models.Orders.Query())).OrderBy(expr.Desc(expr.Value(orm.NumberExpr(models.Orders.TotalCents))))
	return rows.Read(rows.Select(rows.FromModel(models.Orders.Query()), schema, rows.MapSelected(schema.Rank, expr.RowNumber(w)))).All(ctx, db)
}
