package correlation

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type result struct{ Count value.Nullable[int64] }
type fields struct {
	rows.Row[result]
	Count rows.Field[result, value.Nullable[int64]]
}
type nestedTag struct{}

var schema = rows.Define(func(b rows.Builder[result], r *result) fields {
	return fields{b.Row(), rows.NullableNumberOf(b, &r.Count)}
})

func F() rows.Query[models.User, result] {
	outer := rows.From(models.Users.Query())
	c := rows.Correlate(outer, rows.From(models.Orders.Query()))
	parentScope := rows.OuterScope(c, rows.ModelScope(models.Users.Query()))
	childScope := rows.InnerScope(c, rows.ModelScope(models.Orders.Query()))
	parent := expr.At(parentScope, models.Users.ID)
	buyer := expr.At(childScope, models.Orders.BuyerID)
	c = c.Where(expr.Equal(buyer, parent))
	third := rows.As[nestedTag](models.Users.Query(), "nested_user")
	nested := rows.Correlate(c, third)
	nparent := expr.At(rows.OuterScope(nested, parentScope), models.Users.ID)
	nchild := expr.At(rows.InnerScope(nested, rows.Scope(third)), models.Users.ID)
	c = c.Where(nested.Where(expr.Equal(nparent, nchild)).Limit(1).Exists())
	count := rows.SelectCorrelatedScalar(c, expr.Count[rows.Correlation[models.User, models.Order]]().Value())
	ids := rows.SelectCorrelatedScalar(c, expr.Row(buyer).Value())
	return rows.Select(outer, schema, rows.MapSelected(schema.Count, rows.CorrelatedScalarRowQuery(count).Value())).Where(c.Exists(), rows.InCorrelatedQuery(expr.Value(models.Users.ID), ids))
}
