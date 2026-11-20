package officequeries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var RegionLinks = struct {
	Employees  orm.HasMany[Region, Employee]
	LatestHire orm.HasOne[Region, Employee]
	Notes      orm.HasMany[Region, Note]
}{
	orm.HasManyThroughOf(Regions.Employees, orm.JoinKeys(Regions.ID, Offices.RegionID), orm.JoinKeys(Offices.ID, Employees.OfficeID)),
	orm.HasOneThroughOf(Regions.LatestHire, orm.JoinKeys(Regions.ID, Offices.RegionID), orm.JoinKeys(Offices.ID, Employees.OfficeID)).OfMany(orm.Desc(Employees.HiredAt)),
	orm.MorphManyOf(Regions.Notes, orm.JoinKeys(Regions.Code, Notes.SubjectID), Notes.SubjectType),
}
var OfficeLinks = struct {
	Newest, Earliest, TopEarner orm.HasOne[Office, Employee]
	Region                      orm.BelongsTo[Office, Region]
	Colleagues                  orm.HasMany[Office, Employee]
	Notes                       orm.HasMany[Office, Note]
	Labels                      orm.ThroughRelation[Office, Label, Labeling]
	Desks                       orm.HasMany[Office, Desk]
}{
	orm.HasOneOf(Offices.Newest, Offices.ID, Employees.OfficeID).LatestOfMany(),
	orm.HasOneOf(Offices.Earliest, Offices.ID, Employees.OfficeID).OfMany(orm.Asc(Employees.HiredAt)),
	orm.HasOneOf(Offices.TopEarner, Offices.ID, Employees.OfficeID).OfMany(orm.Desc(Employees.Salary)),
	orm.BelongsToOf(Offices.Region, Offices.RegionID, Regions.ID),
	orm.HasManyOf(Offices.Colleagues, Offices.ID, Employees.OfficeID),
	orm.MorphManyOf(Offices.Notes, orm.JoinKeys(Offices.Code, Notes.SubjectID), Notes.SubjectType),
	orm.MorphToManyOf(Offices.Labels, orm.JoinKeys(Offices.Code, Labelings.LabelableID), Labelings.LabelableType, orm.JoinKeys(Labelings.LabelID, Labels.ID)),
	orm.HasManyOf(Offices.Desks, Offices.ID, Desks.OfficeID),
}
var OfficeValues = struct {
	Headcount orm.AggregateRelation[Office, int64]
}{
	Offices.Headcount.Using(expr.Related(OfficeLinks.Colleagues, expr.Count[Employee]())),
}
var EmployeeLinks = struct{ Region orm.HasOne[Employee, Region] }{
	orm.HasOneThroughOf(Employees.Region, orm.JoinKeys(Employees.OfficeID, Offices.ID), orm.JoinKeys(Offices.RegionID, Regions.ID)),
}
var NoteLinks = struct {
	Region orm.BelongsTo[Note, Region]
	Office orm.BelongsTo[Note, Office]
}{
	orm.MorphToOf(Notes.Region, orm.JoinKeys(Notes.SubjectID, Regions.Code), Notes.SubjectType),
	orm.MorphToOf(Notes.Office, orm.JoinKeys(Notes.SubjectID, Offices.Code), Notes.SubjectType),
}
var LabelLinks = struct {
	Offices orm.ThroughRelation[Label, Office, Labeling]
}{
	orm.MorphedByManyOf(Labels.Offices, orm.JoinKeys(Labels.ID, Labelings.LabelID), Labelings.LabelableType, orm.JoinKeys(Labelings.LabelableID, Offices.Code)),
}
var DeskLinks = struct{ Office orm.BelongsTo[Desk, Office] }{deskOffice(Desks)}

func deskOffice(t DeskTable) orm.BelongsTo[Desk, Office] {
	return orm.BelongsToOf(t.Office, t.OfficeID, Offices.ID)
}
func OpenOfficeScope(t DeskTable) orm.GlobalScope[Desk] {
	return orm.Scope(t.Table, "open_office", func(context.Context) (orm.Predicate[Desk], error) { return orm.Has(deskOffice(t)), nil })
}
