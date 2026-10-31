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
		rows.Map(UserSummaries.ID, expr.Value(orm.KeyExpr(u.ID))), rows.Map(UserSummaries.Email, expr.Value(orm.TextExpr(u.Email))), rows.Map(UserSummaries.Nickname, expr.Value(orm.NullableTextExpr(u.Nickname))), rows.Map(UserSummaries.Status, expr.Value(orm.EnumExpr(u.Status))))

	got, err := rows.CompileRead(rows.Read(report))
	check(got, err)
	o := models.Orders
	totals := rows.GroupBy(rows.Select(rows.From(o.Query()), BuyerTotalRows,
		rows.Map(BuyerTotalRows.BuyerID, expr.Value(orm.ColumnExpr(o.BuyerID))), rows.MapSelected(BuyerTotalRows.Total, expr.AsSelected(expr.Sum(orm.NumberExpr(o.TotalCents)))), rows.MapSelected(BuyerTotalRows.Orders, expr.AsSelected(expr.Count[models.Order]())), rows.MapSelected(BuyerTotalRows.Average, expr.AsSelected(expr.Avg(orm.NumberExpr(o.TotalCents))))), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID))))

	got, err = rows.CompileRead(rows.Read(totals))
	check(got, err)
}
