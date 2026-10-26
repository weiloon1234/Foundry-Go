package key_clauses

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

func Correlated() orm.Query[models.User] {
	q := models.Users.Query()
	c := rows.Correlate(rows.From(q), rows.From(models.Orders.Query()))
	parent := expr.At(rows.OuterScope(c, rows.ModelScope(q)), models.Users.ID)
	scope := rows.InnerScope(c, rows.ModelScope(models.Orders.Query()))
	buyer, total := expr.At(scope, models.Orders.BuyerID), expr.At(scope, models.Orders.TotalCents)
	count := expr.Count[rows.Correlation[models.User, models.Order]]()
	selected := rows.SelectCorrelatedScalar(c.Where(expr.Equal(buyer, parent)), count.Value())
	selected = rows.GroupByCorrelatedScalar(selected, expr.Group(total))
	selected = rows.OrderCorrelatedScalar(rows.DistinctOnCorrelatedScalarValues(selected, count.Value().Key()), count.Desc()).Limit(1)
	return expr.Where(q, selected.Exists())
}

func Combined() orm.Query[models.User] {
	q := models.Users.Query()
	scalar := rows.Values(rows.SelectScalar(rows.From(q), expr.Row(expr.Value(models.Users.Nickname)).Value()))
	set := rows.UnionAllValues(scalar, scalar)
	rank := expr.RowNumber(expr.Window[rows.Set[value.Nullable[string]]]{}.OrderBy(set.Value().Asc()))
	selected := rows.DistinctOnValueSet(set, rank.Key()).OrderBy(rank.Asc()).Limit(2)
	return expr.Where(q, rows.ExistsQuery(rows.From(q), selected))
}
