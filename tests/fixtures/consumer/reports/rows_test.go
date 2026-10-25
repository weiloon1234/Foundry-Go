package reports

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

func TestRowsSQLParity(t *testing.T) {
	check := func(got orm.Statement, err error, legacy func() (query.Statement, error)) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		want, err := legacy()
		if err != nil {
			t.Fatal(err)
		}
		if want.SQL() != got.SQL() || !reflect.DeepEqual(want.Arguments(), got.Arguments()) || !oracleEqual(t, "consumer/reports/rows_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatalf("report SQL changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
		}
	}
	u := models.Users
	report := rows.Select(rows.From(u.Query()), UserSummaries,
		rows.Map(UserSummaries.ID, expr.Value(u.ID)), rows.Map(UserSummaries.Email, expr.Value(u.Email)), rows.Map(UserSummaries.Nickname, expr.Value(u.Nickname)), rows.Map(UserSummaries.Status, expr.Value(u.Status)))
	f := models.UserFields()
	got, err := rows.Compile(report)
	check(got, err, ProjectUserSummary(models.QueryUsers()).SelectID(query.Value(f.ID)).SelectEmail(query.Value(f.Email)).SelectNickname(query.Value(f.Nickname)).SelectStatus(query.Value(f.Status)).Query().Compile)
	o := models.Orders
	totals := rows.GroupBy(rows.Select(rows.From(o.Query()), BuyerTotalRows,
		rows.Map(BuyerTotalRows.BuyerID, expr.Value(o.BuyerID)), rows.MapSelected(BuyerTotalRows.Total, expr.Sum(o.TotalCents).Value()), rows.MapSelected(BuyerTotalRows.Orders, expr.Count[models.Order]().Value()), rows.MapSelected(BuyerTotalRows.Average, expr.Avg(o.TotalCents).Value())), expr.Group(expr.Value(o.BuyerID)))
	of := models.OrderFields()
	got, err = rows.Compile(totals)
	check(got, err, ProjectBuyerTotals(models.QueryOrders()).SelectBuyerID(query.Value(of.BuyerID)).SelectTotal(query.Sum(of.TotalCents).Value()).SelectOrders(query.Count[models.Order]().Value()).SelectAverage(query.Avg(of.TotalCents).Value()).Query().GroupBy(of.BuyerID.Group()).Compile)
}
