package window_frames

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type result struct {
	Count    int64
	Previous value.Nullable[int64]
}
type fields struct {
	rows.Row[result]
	Count    rows.Field[result, int64]
	Previous rows.Field[result, value.Nullable[int64]]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields {
	return fields{b.Row(), rows.NumberOf(b, &r.Count), rows.NullableNumberOf(b, &r.Previous)}
})

func Run(ctx context.Context, db database.Executor) ([]result, error) {
	v := expr.Value(models.Orders.TotalCents)
	r := expr.NumericRange(expr.WindowFor(rows.ModelScope(models.Orders.Query())), expr.Row(v).Value())
	w := r.Between(r.Preceding(10), r.CurrentRow()).Named("nearby")
	return rows.Select(rows.FromModel(models.Orders.Query()), schema, rows.MapSelected(schema.Count, expr.Over(expr.Count[models.Order](), w)), rows.MapSelected(schema.Previous, expr.Lag(expr.Row(v).Value(), 1, w))).All(ctx, db)
}
