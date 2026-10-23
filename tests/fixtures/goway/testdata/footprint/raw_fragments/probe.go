package raw_fragments

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type result struct{ Total int64 }
type fields struct {
	rows.Row[result]
	Total rows.Field[result, int64]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields { return fields{b.Row(), rows.NumberOf(b, &r.Total)} })

func Run(ctx context.Context, db database.Executor) ([]result, error) {
	source := rows.FromModel(models.Orders.Query().Where(orm.WhereRaw[models.Order]("total_cents > ?", 0)))
	return rows.Select(source, schema, rows.MapSelected(schema.Total, expr.Raw[models.Order, int64]("sum(total_cents)"))).All(ctx, db)
}
