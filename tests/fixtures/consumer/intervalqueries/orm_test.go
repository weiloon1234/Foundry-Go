package intervalqueries

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestIntervalDeclarationSQLParity(t *testing.T) {
	type statement interface {
		SQL() string
		Arguments() []any
	}
	compare := func(got statement, err error, legacy func() (query.Statement, error)) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		want, err := legacy()
		if err != nil {
			t.Fatal(err)
		}
		if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
			t.Fatalf("interval SQL changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
		}
	}
	got, err := orm.Compile(Samples.Query().Where(Samples.Period.Eq(temporal.Months(1))))
	compare(got, err, QueryIntervalSamples().Where(SampleFields().Period.Eq(temporal.Months(1))).Compile)
	got, err = orm.Compile(Plans.Query().Where(orm.Has(PlanLinks.Samples)))
	compare(got, err, QueryIntervalPlans().Where(PlanRelations().Samples.Exists()).Compile)
	got, err = orm.Compile(Labels.Query().Where(Labels.ID.Eq(temporal.Days(30))))
	compare(got, err, QueryIntervalLabels().Where(LabelFields().ID.Eq(temporal.Days(30))).Compile)
	got, err = orm.Compile(Links.Query())
	compare(got, err, QueryIntervalLinks().Compile)
	projection := rows.Select(rows.From(Samples.Query()), Summaries,
		rows.MapSelected(Summaries.Total, expr.SumInterval(Samples.Period).Value()),
		rows.MapSelected(Summaries.Average, expr.AvgInterval(Samples.Period).Value()),
		rows.MapSelected(Summaries.Count, expr.Count[Sample]().Value()))
	got, err = rows.Compile(projection)
	legacy := ProjectSummary(QueryIntervalSamples()).SelectTotal(query.Sum(SampleFields().Period).Value()).SelectAverage(query.Avg(SampleFields().Period).Value()).SelectCount(query.Count[Sample]().Value()).Query()
	compare(got, err, legacy.Compile)
}
