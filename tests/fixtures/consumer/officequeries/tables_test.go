package officequeries

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestORMOfficeDeclarationParity(t *testing.T) {
	if err := orm.Check(append(Tables(), RegionLinks.Employees, RegionLinks.LatestHire, RegionLinks.Notes, OfficeLinks.Newest, OfficeLinks.Earliest, OfficeLinks.TopEarner, OfficeLinks.Region, OfficeLinks.Colleagues, OfficeLinks.Notes, OfficeLinks.Labels, OfficeLinks.Desks, OfficeValues.Headcount, EmployeeLinks.Region, NoteLinks.Region, NoteLinks.Office, LabelLinks.Offices, DeskLinks.Office)...); err != nil {
		t.Fatal(err)
	}
	check := func(got orm.Statement, err error, legacy func() (query.Statement, error)) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		want, err := legacy()
		if err != nil {
			t.Fatal(err)
		}
		if want.SQL() != got.SQL() || !reflect.DeepEqual(want.Arguments(), got.Arguments()) || !oracleEqual(t, "consumer/officequeries/tables_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatalf("office SQL changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
		}
	}
	got, err := orm.Compile(orm.WithScopeContext(Desks.Query(), t.Context()))
	check(got, err, QueryOfficeDesks().WithScopeContext(t.Context()).Compile)
	got, err = orm.Compile(Regions.Query().Where(orm.Has(RegionLinks.Employees)))
	check(got, err, QueryOfficeRegions().WhereHas(RegionRelations().Employees).Compile)
	got, err = orm.Compile(Offices.Query().Where(orm.Has(OfficeLinks.Labels)))
	check(got, err, QueryOfficeOffices().WhereHas(OfficeRelations().Labels).Compile)
	got, err = orm.Compile(Notes.Query().Where(orm.Has(NoteLinks.Region)))
	check(got, err, QueryOfficeNotes().WhereHas(NoteRelations().Region).Compile)
}
