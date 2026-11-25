package intervalqueries

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestIntervalDeclarationSQLParity(t *testing.T) {
	type statement interface {
		SQL() string
		Arguments() []any
	}
	compare := func(got statement, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}

		if !oracleEqual(t, "consumer/intervalqueries/orm_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatal("frozen legacy comparison differs")
		}
	}
	got, err := orm.Compile(Samples.Query().Where(orm.Eq(Samples.Period, temporal.Months(1))))
	compare(got, err)
	got, err = orm.Compile(Plans.Query().Where(orm.Has(PlanLinks.Samples)))
	compare(got, err)
	got, err = orm.Compile(Labels.Query().Where(orm.Eq(Labels.ID, temporal.Days(30))))
	compare(got, err)
	got, err = orm.Compile(Links.Query())
	compare(got, err)
	projection := PeriodSummary()
	got, err = rows.CompileRead(rows.Read(projection))

	compare(got, err)
}
