package scalar_temporal

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type result struct {
	Day      temporal.Date
	Seconds  decimal.Decimal
	Period   temporal.Interval
	Distance float64
}
type fields struct {
	rows.Row[result]
	Day      rows.Field[result, temporal.Date]
	Seconds  rows.Field[result, decimal.Decimal]
	Period   rows.Field[result, temporal.Interval]
	Distance rows.Field[result, float64]
}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields {
	return fields{b.Row(), rows.TimeOf(b, &r.Day), rows.NumberOf(b, &r.Seconds), rows.FieldOf(b, &r.Period, orm.Codec(codec.Interval())), rows.NumberOf(b, &r.Distance)}
})

func Run(ctx context.Context, db database.Executor) ([]result, error) {
	n := expr.Value(orm.NumberExpr(models.Orders.TotalCents))
	at := expr.FromUnixMillis(n)
	local := expr.LocalAt(at, expr.UTCZone())
	period := expr.InstantDifference(expr.TransactionTime(rows.ModelScope(models.Orders.Query())), at)
	distance := expr.SphericalDistance(n, n, expr.GeographicPoint{})
	return rows.Read(rows.Select(rows.FromModel(models.Orders.Query()), schema, rows.MapSelected(schema.Day, expr.AsSelected(expr.DateOf(local))), rows.MapSelected(schema.Seconds, expr.AsSelected(expr.Second(local))), rows.MapSelected(schema.Period, expr.AsSelected(expr.NegateInterval(period))), rows.MapSelected(schema.Distance, expr.AsSelected(distance)))).All(ctx, db)
}
