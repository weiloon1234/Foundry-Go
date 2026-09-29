package officequeries_test

import (
	"slices"
	"testing"
	"time"

	"foundry.test/consumer/officequeries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type offices struct {
	db              *database.DB
	north, south    officequeries.Region
	alpha, beta, xi officequeries.Office
}

func setup(t *testing.T) offices {
	t.Helper()
	db := pgtest.Isolate(t).Open(t)
	for _, ddl := range []string{
		`CREATE TABLE office_regions(id uuid PRIMARY KEY, code text NOT NULL UNIQUE, name text NOT NULL)`,
		`CREATE TABLE office_offices(id uuid PRIMARY KEY, code text NOT NULL UNIQUE, region_id uuid NOT NULL REFERENCES office_regions(id), name text NOT NULL, deleted_at timestamptz)`,
		`CREATE TABLE office_notes(id uuid PRIMARY KEY, subject_type text NOT NULL, subject_id text NOT NULL, body text NOT NULL)`,
		`CREATE TABLE office_labels(id uuid PRIMARY KEY, name text NOT NULL)`,
		`CREATE TABLE office_labelings(id uuid PRIMARY KEY, label_id uuid NOT NULL REFERENCES office_labels(id), labelable_type text NOT NULL, labelable_id text NOT NULL)`,
		`CREATE TABLE office_employees(id uuid PRIMARY KEY, office_id uuid NOT NULL REFERENCES office_offices(id), name text NOT NULL, salary bigint NOT NULL, hired_at timestamptz NOT NULL)`,
		`CREATE TABLE office_desks(id uuid PRIMARY KEY, office_id uuid NOT NULL REFERENCES office_offices(id), label text NOT NULL)`,
	} {
		if _, err := db.Exec(t.Context(), ddl); err != nil {
			t.Fatal(err)
		}
	}
	ctx := t.Context()
	region := func(name string) officequeries.Region {
		r, err := officequeries.QueryOfficeRegions().Create(ctx, db, officequeries.RegionDraft{}.SetName(name).SetCode("r-"+name))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	office := func(region officequeries.Region, name string) officequeries.Office {
		o, err := officequeries.QueryOfficeOffices().Create(ctx, db, officequeries.OfficeDraft{}.SetRegionID(region.ID).SetName(name).SetCode("o-"+name))
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	f := offices{db: db, north: region("north"), south: region("south")}
	f.alpha, f.beta, f.xi = office(f.north, "alpha"), office(f.north, "beta"), office(f.south, "xi")
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, employee := range []struct {
		office officequeries.Office
		name   string
		salary int64
	}{{f.alpha, "ann", 10}, {f.alpha, "bob", 30}, {f.alpha, "cid", 20}, {f.beta, "dee", 5}, {f.xi, "eve", 7}} {
		if _, err := officequeries.QueryOfficeEmployees().Create(ctx, db, officequeries.EmployeeDraft{}.
			SetOfficeID(employee.office.ID).SetName(employee.name).SetSalary(employee.salary).SetHiredAt(base.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func names(employees []officequeries.Employee) []string {
	result := make([]string, len(employees))
	for i, employee := range employees {
		result[i] = employee.Name
	}
	slices.Sort(result)
	return result
}

func TestRelationshipsThroughAnIntermediate(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	regions, err := officequeries.QueryOfficeRegions().With(officequeries.RegionRelations().Employees).
		OrderBy(officequeries.RegionFields().Name.Asc()).All(ctx, f.db)
	if err != nil || len(regions) != 2 {
		t.Fatal(err)
	}
	north, _ := regions[0].Employees.Get()
	south, _ := regions[1].Employees.Get()
	if !slices.Equal(names(north), []string{"ann", "bob", "cid", "dee"}) || !slices.Equal(names(south), []string{"eve"}) {
		t.Fatal("has-many-through grouping", names(north), names(south))
	}
	// One of many through an intermediate chooses per region, not per office:
	// north's two offices yield one hire, the latest across both (dee at beta).
	assertLatestHires(t, f, "dee", "eve")
	// The intermediate's soft deletion hides its employees from the region.
	if _, err := officequeries.QueryOfficeOffices().Delete(ctx, f.db, f.beta.ID); err != nil {
		t.Fatal(err)
	}
	regions, err = officequeries.QueryOfficeRegions().With(officequeries.RegionRelations().Employees).
		Where(officequeries.RegionFields().ID.Eq(f.north.ID)).All(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	if north, _ = regions[0].Employees.Get(); !slices.Equal(names(north), []string{"ann", "bob", "cid"}) {
		t.Fatal("intermediate scope ignored", names(north))
	}
	e := officequeries.EmployeeFields()
	withSouth, err := officequeries.QueryOfficeRegions().WhereHas(officequeries.RegionRelations().Employees.Where(e.Name.Eq("eve"))).All(ctx, f.db)
	if err != nil || len(withSouth) != 1 || withSouth[0].ID != f.south.ID {
		t.Fatal("WhereHas through the intermediate", withSouth, err)
	}
	employees, err := officequeries.QueryOfficeEmployees().With(officequeries.EmployeeRelations().Region).
		Where(e.Name.Eq("eve")).All(ctx, f.db)
	if err != nil || len(employees) != 1 {
		t.Fatal(err)
	}
	if region, loaded := employees[0].Region.Get(); !loaded || !region.IsSet() {
		t.Fatal("has-one-through not loaded")
	} else if stored, _ := region.Get(); stored.ID != f.south.ID {
		t.Fatal("has-one-through loaded the wrong region")
	}
	// With beta soft-deleted, alpha's latest hire is the north region's.
	assertLatestHires(t, f, "cid", "eve")
	count := query.RelatedValue(officequeries.RegionRelations().Employees, query.Count[officequeries.Employee]())
	counted, err := query.WithValue(officequeries.QueryOfficeRegions().OrderBy(count.Desc()).Query, count).All(ctx, f.db)
	if err != nil || len(counted) != 2 || counted[0].Model.ID != f.north.ID || counted[0].Value != 3 || counted[1].Value != 1 {
		t.Fatal("related value through the intermediate", counted, err)
	}
}

// assertLatestHires checks HasOneThrough(...).OfMany per region, ordered by name.
func assertLatestHires(t *testing.T, f offices, north, south string) {
	t.Helper()
	latest, err := officequeries.QueryOfficeRegions().With(officequeries.RegionRelations().LatestHire).
		OrderBy(officequeries.RegionFields().Name.Asc()).All(t.Context(), f.db)
	if err != nil || len(latest) != 2 {
		t.Fatal("one-of-many through an intermediate", err)
	}
	for i, want := range []string{north, south} {
		hire, loaded := latest[i].LatestHire.Get()
		stored, present := hire.Get()
		if !loaded || !present || stored.Name != want {
			t.Fatal("latest hire per region", i, stored.Name, want)
		}
	}
}

func TestOneOfManyAndAdHocAggregates(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	r := officequeries.OfficeRelations()
	loaded, err := officequeries.QueryOfficeOffices().With(r.Newest, r.Earliest, r.TopEarner).
		Where(officequeries.OfficeFields().ID.Eq(f.alpha.ID)).All(ctx, f.db)
	if err != nil || len(loaded) != 1 {
		t.Fatal(err)
	}
	one := func(slot interface {
		Get() (value.Optional[officequeries.Employee], bool)
	}) string {
		selected, _ := slot.Get()
		employee, _ := selected.Get()
		return employee.Name
	}
	if one(loaded[0].Earliest) != "ann" || one(loaded[0].TopEarner) != "bob" || one(loaded[0].Newest) == "" {
		t.Fatal("one-of-many choices", one(loaded[0].Earliest), one(loaded[0].TopEarner), one(loaded[0].Newest))
	}
	e := officequeries.EmployeeFields()
	cheap, err := officequeries.QueryOfficeOffices().With(r.TopEarner.Where(e.Salary.Lt(25))).
		Where(officequeries.OfficeFields().ID.Eq(f.alpha.ID)).All(ctx, f.db)
	if err != nil || one(cheap[0].TopEarner) != "cid" {
		t.Fatal("filters take part in the one-of-many choice", err)
	}
	headcount := query.RelatedValue(r.Colleagues, query.Count[officequeries.Employee]())
	payroll := query.RelatedValue(r.Colleagues, e.Salary.Sum())
	ranked, err := query.WithValue(officequeries.QueryOfficeOffices().OrderBy(headcount.Desc(), officequeries.OfficeFields().Name.Asc()).Query, payroll).All(ctx, f.db)
	if err != nil || len(ranked) != 3 || ranked[0].Model.ID != f.alpha.ID {
		t.Fatal("ordering by relation count", ranked, err)
	}
	if total, present := ranked[0].Value.Get(); !present || total.String() != "60" {
		t.Fatal("ad-hoc sum", ranked[0].Value)
	}
	busy, err := officequeries.QueryOfficeOffices().Where(query.OrderRow(headcount).Gte(2)).Count(ctx, f.db)
	if err != nil || busy != 1 {
		t.Fatal("filtering by relation count", busy, err)
	}
	// Generated aggregate slots and ad-hoc values agree.
	slots, err := officequeries.QueryOfficeOffices().With(officequeries.OfficeAggregates().Headcount).All(ctx, f.db)
	if err != nil || len(slots) != 3 {
		t.Fatal(err)
	}
}

func TestPolymorphicRelationshipsUseTypedMorphNames(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	note := func(subject, id, body string) {
		t.Helper()
		if _, err := f.db.Exec(ctx, `INSERT INTO office_notes VALUES (gen_random_uuid(), $1, $2, $3)`, subject, id, body); err != nil {
			t.Fatal(err)
		}
	}
	note("office", f.alpha.Code, "alpha lease")
	note("region", f.north.Code, "north budget")
	note("office", f.north.Code, "no such office")

	offices, err := officequeries.QueryOfficeOffices().With(officequeries.OfficeRelations().Notes).
		Where(officequeries.OfficeFields().ID.Eq(f.alpha.ID)).All(ctx, f.db)
	if err != nil || len(offices) != 1 {
		t.Fatal(err)
	}
	if notes, _ := offices[0].Notes.Get(); len(notes) != 1 || notes[0].Body != "alpha lease" {
		t.Fatal("morph-many ignored the morph name", notes)
	}
	regions, err := officequeries.QueryOfficeRegions().With(officequeries.RegionRelations().Notes).
		Where(officequeries.RegionFields().ID.Eq(f.north.ID)).All(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	if notes, _ := regions[0].Notes.Get(); len(notes) != 1 || notes[0].Body != "north budget" {
		t.Fatal("morph-many crossed target types", notes)
	}
	r := officequeries.NoteRelations()
	notes, err := officequeries.QueryOfficeNotes().With(r.Region, r.Office).
		OrderBy(officequeries.NoteFields().Body.Asc()).All(ctx, f.db)
	if err != nil || len(notes) != 3 {
		t.Fatal(err)
	}
	for _, loaded := range notes {
		region, regionLoaded := loaded.Region.Get()
		office, officeLoaded := loaded.Office.Get()
		if !regionLoaded || !officeLoaded {
			t.Fatal("morph-to slots not loaded")
		}
		switch loaded.Body {
		case "alpha lease":
			if stored, ok := office.Get(); !ok || stored.ID != f.alpha.ID || region.IsSet() {
				t.Fatal("office note resolved to the wrong parent")
			}
		case "north budget":
			if stored, ok := region.Get(); !ok || stored.ID != f.north.ID || office.IsSet() {
				t.Fatal("region note resolved to the wrong parent")
			}
		default:
			if region.IsSet() || office.IsSet() {
				t.Fatal("a morph name mismatch loaded a parent")
			}
		}
	}
	withRegion, err := officequeries.QueryOfficeNotes().WhereHas(r.Region).Count(ctx, f.db)
	if err != nil || withRegion != 1 {
		t.Fatal("morph-to existence", withRegion, err)
	}

	label, err := officequeries.QueryOfficeLabels().Create(ctx, f.db, officequeries.LabelDraft{}.SetName("remote"))
	if err != nil {
		t.Fatal(err)
	}
	labels := officequeries.OfficeRelations().Labels
	changes, err := labels.Sync(ctx, f.db, f.alpha, []officequeries.Label{label}, officequeries.LabelingDraft{}, nil)
	if err != nil || len(changes.Attached) != 1 || changes.Attached[0].LabelableType != "office" || changes.Attached[0].LabelableID != f.alpha.Code {
		t.Fatal("morph-to-many did not fill the morph name", changes, err)
	}
	if _, err := labels.Attach(ctx, f.db, f.beta, label, officequeries.LabelingDraft{}); err != nil {
		t.Fatal(err)
	}
	byLabel, err := officequeries.QueryOfficeLabels().With(officequeries.LabelRelations().Offices).All(ctx, f.db)
	if err != nil || len(byLabel) != 1 {
		t.Fatal(err)
	}
	if links, _ := byLabel[0].Offices.Get(); len(links) != 2 {
		t.Fatal("morphed-by-many", len(links))
	}
}
