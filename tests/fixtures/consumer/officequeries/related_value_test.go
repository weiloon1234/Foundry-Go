package officequeries

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestOfficeRelatedValueSQLParity(t *testing.T) {
	check := func(got orm.Statement, err error, old func() (query.Statement, error)) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		want, err := old()
		if err != nil {
			t.Fatal(err)
		}
		if want.SQL() != got.SQL() || !reflect.DeepEqual(want.Arguments(), got.Arguments()) || !oracleEqual(t, "consumer/officequeries/related_value_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatalf("new: %s %v\nold: %s %v", got.SQL(), got.Arguments(), want.SQL(), want.Arguments())
		}
	}
	count := expr.RelatedValue(RegionLinks.Employees, expr.Count[Employee]())
	oldCount := query.RelatedValue(RegionRelations().Employees, query.Count[Employee]())
	got, err := rows.CompileRead(rows.WithValue(expr.OrderBy(Regions.Query(), count.Desc()), count))
	check(got, err, query.WithValue(QueryOfficeRegions().OrderBy(oldCount.Desc()).Query, oldCount).Compile)
	headcount := expr.RelatedValue(OfficeLinks.Colleagues, expr.Count[Employee]())
	payroll := expr.RelatedValue(OfficeLinks.Colleagues, expr.Sum(Employees.Salary))
	oldHeadcount := query.RelatedValue(OfficeRelations().Colleagues, query.Count[Employee]())
	oldPayroll := query.RelatedValue(OfficeRelations().Colleagues, query.Sum(EmployeeFields().Salary))
	got, err = rows.CompileRead(rows.WithValue(expr.OrderBy(Offices.Query().Where(Offices.Name.Ne("closed")).With(OfficeLinks.Colleagues).Limit(3).Offset(1), headcount.Desc(), expr.Value(Offices.Name).Asc()), payroll))
	check(got, err, query.WithValue(QueryOfficeOffices().Where(OfficeFields().Name.Ne("closed")).With(OfficeRelations().Colleagues).Limit(3).Offset(1).OrderBy(oldHeadcount.Desc(), OfficeFields().Name.Asc()).Query, oldPayroll).Compile)
	got, err = orm.Compile(expr.Where(Offices.Query(), expr.OrderRow(headcount).Gte(2)))
	check(got, err, QueryOfficeOffices().Where(query.OrderRow(oldHeadcount).Gte(2)).Compile)
	labels := expr.RelatedValue(OfficeLinks.Labels.Where(Labels.Name.Ne("hidden")), expr.Count[Label]())
	oldLabels := query.RelatedValue(OfficeRelations().Labels.Where(LabelFields().Name.Ne("hidden")), query.Count[Label]())
	got, err = rows.CompileRead(rows.WithValue(Offices.Query(), labels))
	check(got, err, query.WithValue(QueryOfficeOffices().Query, oldLabels).Compile)
	filtered := expr.RelatedValue(OfficeLinks.Colleagues.Where(Employees.Name.Ne("hidden")), expr.Count[Employee]().Filter(expr.OrderRow(expr.Value(Employees.Salary)).Gt(10)))
	oldFiltered := query.RelatedValue(OfficeRelations().Colleagues.Where(EmployeeFields().Name.Ne("hidden")), query.Count[Employee]().Filter(EmployeeFields().Salary.Gt(10)))
	got, err = rows.CompileRead(rows.WithValue(Offices.Query(), filtered))
	check(got, err, query.WithValue(QueryOfficeOffices().Query, oldFiltered).Compile)
}
