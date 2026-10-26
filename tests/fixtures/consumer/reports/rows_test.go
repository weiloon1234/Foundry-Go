package reports

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestRowsSQLParity(t *testing.T) {
	check := func(got orm.Statement, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}

		if !oracleEqual(t, "consumer/reports/rows_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatal("frozen legacy comparison differs")
		}
	}
	u := models.Users
	report := rows.Select(rows.From(u.Query()), UserSummaries,
		rows.Map(UserSummaries.ID, expr.Value(u.ID)), rows.Map(UserSummaries.Email, expr.Value(u.Email)), rows.Map(UserSummaries.Nickname, expr.Value(u.Nickname)), rows.Map(UserSummaries.Status, expr.Value(u.Status)))

	got, err := rows.Compile(report)
	check(got, err)
	o := models.Orders
	totals := rows.GroupBy(rows.Select(rows.From(o.Query()), BuyerTotalRows,
		rows.Map(BuyerTotalRows.BuyerID, expr.Value(o.BuyerID)), rows.MapSelected(BuyerTotalRows.Total, expr.Sum(o.TotalCents).Value()), rows.MapSelected(BuyerTotalRows.Orders, expr.Count[models.Order]().Value()), rows.MapSelected(BuyerTotalRows.Average, expr.Avg(o.TotalCents).Value())), expr.Group(expr.Value(o.BuyerID)))

	got, err = rows.Compile(totals)
	check(got, err)
}
