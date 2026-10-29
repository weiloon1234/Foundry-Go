package temporalqueries

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestTemporalDeclarationSQLParity(t *testing.T) {
	got, err := orm.Compile(expr.Where(Samples.Query(), expr.Year(expr.Value(orm.TimeExpr(Samples.Date))).Eq(2024)))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/temporalqueries/orm_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("temporal row predicate SQL changed")
	}
	year := expr.Year(expr.Value(orm.TimeExpr(Samples.Date)))
	report := rows.OrderBy(rows.GroupBy(rows.Select(rows.From(Samples.Query()), CalendarSummaries, rows.MapSelected(CalendarSummaries.Year, year.Value()), rows.MapSelected(CalendarSummaries.Rows, expr.Count[Sample]().Value()), rows.MapSelected(CalendarSummaries.Latest, expr.Max(orm.TimeExpr(Samples.Date)).Value())), expr.Group(year)), year.Asc())
	got, err = rows.CompileRead(rows.Read(report))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/temporalqueries/orm_test/2", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("temporal grouped projection SQL changed")
	}
}
