package temporalqueries

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestTemporalDeclarationSQLParity(t *testing.T) {
	got, err := orm.Compile(expr.Where(Samples.Query(), expr.Year(expr.Value(Samples.Date)).Eq(2024)))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := QueryTemporalSamples().Where(query.Year(SampleFields().Date).Eq(2024)).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL() != legacy.SQL() || !reflect.DeepEqual(got.Arguments(), legacy.Arguments()) {
		t.Fatal("temporal row predicate SQL changed")
	}
	year := expr.Year(expr.Value(Samples.Date))
	report := rows.OrderBy(rows.GroupBy(rows.Select(rows.From(Samples.Query()), CalendarSummaries, rows.MapSelected(CalendarSummaries.Year, year.Value()), rows.MapSelected(CalendarSummaries.Rows, expr.Count[Sample]().Value()), rows.MapSelected(CalendarSummaries.Latest, expr.Max(Samples.Date).Value())), expr.Group(year)), year.Asc())
	got, err = rows.Compile(report)
	if err != nil {
		t.Fatal(err)
	}
	f := SampleFields()
	oldYear := query.Year(f.Date)
	legacy, err = ProjectCalendarSummary(QueryTemporalSamples()).SelectYear(oldYear.Value()).SelectRows(query.Count[Sample]().Value()).SelectLatest(query.Max(f.Date).Value()).Query().GroupBy(oldYear.Group()).OrderBy(oldYear.Asc()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL() != legacy.SQL() || !reflect.DeepEqual(got.Arguments(), legacy.Arguments()) {
		t.Fatal("temporal grouped projection SQL changed")
	}
}
