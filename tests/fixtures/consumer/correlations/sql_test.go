package correlations_test

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestORMCorrelatedGroupingAndRelationParity(t *testing.T) {
	link := rows.Correlate(rows.From(models.Users.Query()), rows.From(models.Orders.Query()))
	buyer := rows.OuterScope(link, rows.ModelScope(models.Users.Query()))
	order := rows.InnerScope(link, rows.ModelScope(models.Orders.Query()))
	matched := link.Where(expr.Equal(expr.At(order, orm.ColumnExpr(models.Orders.BuyerID)), expr.At(buyer, orm.KeyExpr(models.Users.ID))))
	grouped := rows.Having(rows.GroupBy(rows.FromModel(models.Users.Query()), expr.Group(expr.Value(orm.KeyExpr(models.Users.ID)))), expr.Grouped(matched.Exists()))
	got, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.FromRows(grouped), expr.AsSelected(expr.Count[models.User]()))))
	if err != nil {
		t.Fatal(err)
	}

	check := func(got orm.Statement) {
		t.Helper()
		if !oracleEqual(t, "consumer/correlations/sql_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatal("frozen legacy comparison differs")
		}
	}
	check(got)
	friends := models.UserLinks.Friends.Where(expr.RelationFilter(models.UserLinks.Friends, matched.Exists()))
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(friends)))
	if err != nil {
		t.Fatal(err)
	}

	check(got)
	ungrouped := rows.Having(rows.FromModel(models.Users.Query()), expr.Grouped(matched.Exists()))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.FromRows(ungrouped), expr.AsSelected(expr.Count[models.User]())))); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped outer reference accepted", err)
	}
}
