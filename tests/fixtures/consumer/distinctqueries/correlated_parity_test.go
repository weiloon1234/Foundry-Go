package distinctqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestCorrelatedDistinctOnSQLParity(t *testing.T) {
	q := models.Users.Query()
	a := rows.As[purchaseAlias](models.Orders.Query(), "purchase")
	c := rows.Correlate(rows.From(q), a)
	parent := expr.At(rows.OuterScope(c, rows.ModelScope(q)), orm.KeyExpr(models.Users.ID))
	scope := rows.InnerScope(c, rows.Scope(a))
	selected := rows.SelectCorrelatedScalar(c.Where(expr.Equal(expr.At(scope, orm.ColumnExpr(models.Orders.BuyerID)), parent)), expr.Row(expr.At(scope, orm.KeyExpr(models.Orders.ID))).Value())
	selected = rows.DistinctOnCorrelatedScalar(selected, expr.Group(parent)).OrderBy(parent.Asc(), expr.At(scope, orm.NumberExpr(models.Orders.TotalCents)).Desc())
	got, err := orm.Compile(expr.Where(q, selected.Exists()))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/distinctqueries/correlated_parity_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("frozen legacy comparison differs")
	}
}
