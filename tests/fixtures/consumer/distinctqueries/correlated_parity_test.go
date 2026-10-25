package distinctqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestCorrelatedDistinctOnSQLParity(t *testing.T) {
	q := models.Users.Query()
	a := rows.As[purchaseAlias](models.Orders.Query(), "purchase")
	c := rows.Correlate(rows.From(q), a)
	parent := expr.At(rows.OuterScope(c, rows.ModelScope(q)), models.Users.ID)
	scope := rows.InnerScope(c, rows.Scope(a))
	selected := rows.SelectCorrelatedScalar(c.Where(expr.Equal(expr.At(scope, models.Orders.BuyerID), parent)), expr.Select(expr.At(scope, models.Orders.ID)))
	selected = rows.DistinctOnCorrelatedScalar(selected, expr.Group(parent)).OrderBy(parent.Asc(), expr.At(scope, models.Orders.TotalCents).Desc())
	got, err := orm.Compile(expr.Where(q, selected.Exists()))
	if err != nil {
		t.Fatal(err)
	}
	oldQ := models.QueryUsers()
	oldA := query.As[purchaseAlias](models.QueryOrders(), "purchase")
	oldC := query.Correlate(oldQ, oldA)
	u := models.UserFieldsAt(query.OuterScope(oldC, oldQ.Scope()))
	o := models.OrderFieldsAt(query.InnerScope(oldC, oldA.Scope()))
	oldSelected := query.SelectCorrelatedValue(oldC, query.Value(o.ID)).Where(o.BuyerID.EqColumn(u.ID)).DistinctOn(u.ID.Group()).OrderBy(u.ID.Asc(), o.TotalCents.Desc())
	want, err := oldQ.Where(oldSelected.Exists()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if want.SQL() != got.SQL() || !reflect.DeepEqual(want.Arguments(), got.Arguments()) || !oracleEqual(t, "consumer/distinctqueries/correlated_parity_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatalf("correlated keys changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
	}
}
