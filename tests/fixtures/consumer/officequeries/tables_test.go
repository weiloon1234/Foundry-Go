package officequeries

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMOfficeDeclarationParity(t *testing.T) {
	if err := orm.Check(append(Tables(), RegionLinks.Employees, RegionLinks.LatestHire, RegionLinks.Notes, OfficeLinks.Newest, OfficeLinks.Earliest, OfficeLinks.TopEarner, OfficeLinks.Region, OfficeLinks.Colleagues, OfficeLinks.Notes, OfficeLinks.Labels, OfficeLinks.Desks, OfficeValues.Headcount, EmployeeLinks.Region, NoteLinks.Region, NoteLinks.Office, LabelLinks.Offices, DeskLinks.Office)...); err != nil {
		t.Fatal(err)
	}
	check := func(got orm.Statement, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}

		if !oracleEqual(t, "consumer/officequeries/tables_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatal("frozen legacy comparison differs")
		}
	}
	got, err := orm.Compile(orm.WithScopeContext(Desks.Query(), t.Context()))
	check(got, err)
	got, err = orm.Compile(Regions.Query().Where(orm.Has(RegionLinks.Employees)))
	check(got, err)
	got, err = orm.Compile(Offices.Query().Where(orm.Has(OfficeLinks.Labels)))
	check(got, err)
	got, err = orm.Compile(Notes.Query().Where(orm.Has(NoteLinks.Region)))
	check(got, err)
}
