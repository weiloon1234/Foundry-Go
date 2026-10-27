package source_clauses

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type a struct{}
type b struct{}
type latest struct{}
type total struct{ Count int64 }
type totalFields struct {
	rows.Row[total]
	Count rows.Field[total, int64]
}

var totals = rows.Define(func(b rows.Builder[total], r *total) totalFields {
	return totalFields{b.Row(), rows.NumberOf(b, &r.Count)}
})

func Scalar() orm.Query[models.User] {
	q := models.Users.Query()
	alias := rows.AsRecord[a](rows.CTE("eligible", rows.ModelRecord(q)), "eligible")
	age := expr.At(rows.Scope(alias), models.Users.Age)
	selected := rows.LimitScalar(rows.OffsetScalar(rows.OrderScalar(rows.SelectScalar(alias, expr.Row(age).Value()), age.Asc()), 1), 1)
	return expr.Where(q, expr.Equal(expr.NullableRow(expr.Value(models.Users.Age)), rows.ScalarRowQuery(rows.From(q), selected)))
}
func Correlated() orm.Query[models.User] {
	q := models.Users.Query()
	outer, inner := rows.As[a](q, "a"), rows.As[b](q, "b")
	c := rows.Correlate(outer, inner)
	scope := rows.InnerScope(c, rows.Scope(inner))
	age := expr.At(scope, models.Users.Age)
	scalar := rows.DistinctOnCorrelatedScalar(rows.SelectCorrelatedScalar(c, expr.Row(age).Value()), expr.Group(age)).OrderBy(age.Asc())
	return expr.Where(q, rows.ExistsQuery(rows.From(q), rows.SelectScalar(outer, rows.CorrelatedScalarRowQuery(scalar).Value())))
}
func Record() orm.Query[models.User] {
	q := models.Users.Query()
	outer, inner := rows.As[a](q, "a"), rows.As[b](q, "b")
	c := rows.Correlate(outer, inner)
	record := rows.ProjectCorrelated(c, totals, rows.MapSelected(totals.Count, expr.Count[rows.Correlation[rows.Alias[a, models.User], rows.Alias[b, models.User]]]().Value()))
	record = rows.DistinctCorrelatedRecord(rows.HavingCorrelatedRecord(record, expr.Count[rows.Correlation[rows.Alias[a, models.User], rows.Alias[b, models.User]]]().Gt(0)))
	joined := rows.CrossJoinLateral(outer, rows.AsLateral[latest](record, "latest"))
	return expr.Where(q, rows.ExistsQuery(rows.From(q), joined))
}
