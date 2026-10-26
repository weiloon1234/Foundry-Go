package officequeries

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestOfficeRelatedValueSQLParity(t *testing.T) {
	check := func(got orm.Statement, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if !oracleEqual(t, "consumer/officequeries/related_value_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatal("frozen related value SQL differs")
		}
	}
	count := expr.RelatedValue(RegionLinks.Employees, expr.Count[Employee]())

	got, err := rows.CompileRead(rows.WithValue(expr.OrderBy(Regions.Query(), count.Desc()), count))
	check(got, err)
	headcount := expr.RelatedValue(OfficeLinks.Colleagues, expr.Count[Employee]())
	payroll := expr.RelatedValue(OfficeLinks.Colleagues, expr.Sum(Employees.Salary))

	got, err = rows.CompileRead(rows.WithValue(expr.OrderBy(Offices.Query().Where(Offices.Name.Ne("closed")).With(OfficeLinks.Colleagues).Limit(3).Offset(1), headcount.Desc(), expr.Value(Offices.Name).Asc()), payroll))
	check(got, err)
	got, err = orm.Compile(expr.Where(Offices.Query(), expr.OrderRow(headcount).Gte(2)))
	check(got, err)
	labels := expr.RelatedValue(OfficeLinks.Labels.Where(Labels.Name.Ne("hidden")), expr.Count[Label]())

	got, err = rows.CompileRead(rows.WithValue(Offices.Query(), labels))
	check(got, err)
	filtered := expr.RelatedValue(OfficeLinks.Colleagues.Where(Employees.Name.Ne("hidden")), expr.Count[Employee]().Filter(expr.OrderRow(expr.Value(Employees.Salary)).Gt(10)))

	got, err = rows.CompileRead(rows.WithValue(Offices.Query(), filtered))
	check(got, err)
}
