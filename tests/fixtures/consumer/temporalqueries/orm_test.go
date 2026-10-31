package temporalqueries

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestTemporalDeclarationSQLParity(t *testing.T) {
	got, err := orm.Compile(expr.Where(Samples.Query(), expr.Eq(expr.Year(expr.Value(orm.TimeExpr(Samples.Date))), 2024)))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/temporalqueries/orm_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("temporal row predicate SQL changed")
	}
	year := expr.Year(expr.Value(orm.TimeExpr(Samples.Date)))
	report := rows.OrderBy(rows.GroupBy(rows.Select(rows.From(Samples.Query()), CalendarSummaries, rows.MapSelected(CalendarSummaries.Year, expr.AsSelected(year)), rows.MapSelected(CalendarSummaries.Rows, expr.AsSelected(expr.Count[Sample]())), rows.MapSelected(CalendarSummaries.Latest, expr.AsSelected(expr.Max(orm.TimeExpr(Samples.Date))))), expr.Group(year)), expr.Asc(year))
	got, err = rows.CompileRead(rows.Read(report))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/temporalqueries/orm_test/2", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("temporal grouped projection SQL changed")
	}
}
