package correlations_test

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestORMCorrelatedGroupingAndRelationParity(t *testing.T) {
	link := rows.Correlate(rows.From(models.Users.Query()), rows.From(models.Orders.Query()))
	buyer := rows.OuterScope(link, rows.ModelScope(models.Users.Query()))
	order := rows.InnerScope(link, rows.ModelScope(models.Orders.Query()))
	matched := link.Where(expr.Equal(expr.At(order, models.Orders.BuyerID), expr.At(buyer, models.Users.ID)))
	grouped := rows.Having(rows.GroupBy(rows.FromModel(models.Users.Query()), expr.Group(expr.Value(models.Users.ID))), expr.Grouped(matched.Exists()))
	got, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.FromRows(grouped), expr.Count[models.User]().Value())))
	if err != nil {
		t.Fatal(err)
	}
	oldLink := query.Correlate(models.QueryUsers(), models.QueryOrders())
	oldBuyer := models.UserFieldsAt(query.OuterScope(oldLink, models.QueryUsers().Scope()))
	oldOrder := models.OrderFieldsAt(query.InnerScope(oldLink, models.QueryOrders().Scope()))
	oldMatched := oldLink.Where(oldOrder.BuyerID.EqColumn(oldBuyer.ID))
	want, err := query.SelectValue(models.QueryUsers(), query.Count[models.User]().Value()).GroupBy(models.UserFields().ID.Group()).Having(query.Grouped(oldMatched.Exists())).Compile()
	if err != nil {
		t.Fatal(err)
	}
	check := func(got orm.Statement, want query.Statement) {
		t.Helper()
		if want.SQL() != got.SQL() || !reflect.DeepEqual(want.Arguments(), got.Arguments()) || !oracleEqual(t, "consumer/correlations/sql_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatalf("correlation SQL changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
		}
	}
	check(got, want)
	friends := models.UserLinks.Friends.Where(expr.RelationFilter(models.UserLinks.Friends, matched.Exists()))
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(friends)))
	if err != nil {
		t.Fatal(err)
	}
	want, err = models.QueryUsers().WhereHas(models.UserRelations().Friends.Where(oldMatched.Exists())).Compile()
	if err != nil {
		t.Fatal(err)
	}
	check(got, want)
	ungrouped := rows.Having(rows.FromModel(models.Users.Query()), expr.Grouped(matched.Exists()))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.FromRows(ungrouped), expr.Count[models.User]().Value()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped outer reference accepted", err)
	}
}
