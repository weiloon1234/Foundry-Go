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
	orm.HasManyThroughOf(Regions.Employees, orm.KeyExpr(Regions.ID), orm.ColumnExpr(Offices.RegionID), orm.KeyExpr(Offices.ID), orm.ColumnExpr(Employees.OfficeID)),
	orm.HasOneThroughOf(Regions.LatestHire, orm.KeyExpr(Regions.ID), orm.ColumnExpr(Offices.RegionID), orm.KeyExpr(Offices.ID), orm.ColumnExpr(Employees.OfficeID)).OfMany(orm.TimeDesc(Employees.HiredAt)),
	orm.MorphManyOf(Regions.Notes, orm.TextExpr(Regions.Code), orm.TextExpr(Notes.SubjectID), orm.ColumnExpr(Notes.SubjectType)),
}
var OfficeLinks = struct {
	Newest, Earliest, TopEarner orm.HasOne[Office, Employee]
	Region                      orm.BelongsTo[Office, Region]
	Colleagues                  orm.HasMany[Office, Employee]
	Notes                       orm.HasMany[Office, Note]
	Labels                      orm.ThroughRelation[Office, Label, Labeling]
	Desks                       orm.HasMany[Office, Desk]
}{
	orm.HasOneOf(Offices.Newest, orm.KeyExpr(Offices.ID), orm.ColumnExpr(Employees.OfficeID)).LatestOfMany(),
	orm.HasOneOf(Offices.Earliest, orm.KeyExpr(Offices.ID), orm.ColumnExpr(Employees.OfficeID)).OfMany(orm.TimeAsc(Employees.HiredAt)),
	orm.HasOneOf(Offices.TopEarner, orm.KeyExpr(Offices.ID), orm.ColumnExpr(Employees.OfficeID)).OfMany(orm.NumberDesc(Employees.Salary)),
	orm.BelongsToOf(Offices.Region, orm.ColumnExpr(Offices.RegionID), orm.KeyExpr(Regions.ID)),
	orm.HasManyOf(Offices.Colleagues, orm.KeyExpr(Offices.ID), orm.ColumnExpr(Employees.OfficeID)),
	orm.MorphManyOf(Offices.Notes, orm.TextExpr(Offices.Code), orm.TextExpr(Notes.SubjectID), orm.ColumnExpr(Notes.SubjectType)),
	orm.MorphToManyOf(Offices.Labels, orm.TextExpr(Offices.Code), orm.TextExpr(Labelings.LabelableID), orm.ColumnExpr(Labelings.LabelableType), orm.ColumnExpr(Labelings.LabelID), orm.KeyExpr(Labels.ID)),
	orm.HasManyOf(Offices.Desks, orm.KeyExpr(Offices.ID), orm.ColumnExpr(Desks.OfficeID)),
}
var OfficeValues = struct {
	Headcount orm.AggregateRelation[Office, int64]
}{
	Offices.Headcount.Using(expr.Related(OfficeLinks.Colleagues, expr.Count[Employee]())),
}
var EmployeeLinks = struct{ Region orm.HasOne[Employee, Region] }{
	orm.HasOneThroughOf(Employees.Region, orm.ColumnExpr(Employees.OfficeID), orm.KeyExpr(Offices.ID), orm.ColumnExpr(Offices.RegionID), orm.KeyExpr(Regions.ID)),
}
var NoteLinks = struct {
	Region orm.BelongsTo[Note, Region]
	Office orm.BelongsTo[Note, Office]
}{
	orm.MorphToOf(Notes.Region, orm.TextExpr(Notes.SubjectID), orm.ColumnExpr(Notes.SubjectType), orm.TextExpr(Regions.Code)),
	orm.MorphToOf(Notes.Office, orm.TextExpr(Notes.SubjectID), orm.ColumnExpr(Notes.SubjectType), orm.TextExpr(Offices.Code)),
}
var LabelLinks = struct {
	Offices orm.ThroughRelation[Label, Office, Labeling]
}{
	orm.MorphedByManyOf(Labels.Offices, orm.KeyExpr(Labels.ID), orm.ColumnExpr(Labelings.LabelID), orm.ColumnExpr(Labelings.LabelableType), orm.TextExpr(Labelings.LabelableID), orm.TextExpr(Offices.Code)),
}
var DeskLinks = struct{ Office orm.BelongsTo[Desk, Office] }{deskOffice(Desks)}

func deskOffice(t DeskTable) orm.BelongsTo[Desk, Office] {
	return orm.BelongsToOf(t.Office, orm.ColumnExpr(t.OfficeID), orm.KeyExpr(Offices.ID))
}
func OpenOfficeScope(t DeskTable) orm.GlobalScope[Desk] {
	return orm.Scope(t.Table, "open_office", func(context.Context) (orm.Predicate[Desk], error) { return orm.Has(deskOffice(t)), nil })
}
