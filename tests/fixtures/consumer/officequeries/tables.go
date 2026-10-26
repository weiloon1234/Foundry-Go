package officequeries

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// RegionTable binds Region storage and typed relation slots.
type RegionTable struct {
	orm.Table[Region, model.ID[Region]]
	ID         orm.Key[Region, model.ID[Region]]
	Code       orm.Text[Region, string]
	Name       orm.Text[Region, string]
	Employees  orm.Many[Region, Employee]
	Notes      orm.Many[Region, Note]
	LatestHire orm.One[Region, Employee]
}

var Regions = orm.Define("office_regions", func(b orm.Builder[Region], m *Region) RegionTable {
	table := orm.TableOf(b, &m.ID)
	return RegionTable{Table: table, ID: table.Key(), Code: orm.TextOf(b, &m.Code), Name: orm.TextOf(b, &m.Name), Employees: orm.ManyOf(b, &m.Employees), Notes: orm.ManyOf(b, &m.Notes), LatestHire: orm.OneOf(b, &m.LatestHire)}
})

// OfficeTable binds Office storage and typed relation slots.
type OfficeTable struct {
	orm.Table[Office, model.ID[Office]]
	ID         orm.Key[Office, model.ID[Office]]
	Code       orm.Text[Office, string]
	RegionID   orm.Column[Office, model.ID[Region]]
	Name       orm.Text[Office, string]
	DeletedAt  orm.SoftDelete[Office, time.Time]
	Newest     orm.One[Office, Employee]
	Earliest   orm.One[Office, Employee]
	TopEarner  orm.One[Office, Employee]
	Region     orm.One[Office, Region]
	Headcount  orm.Value[Office, int64]
	Colleagues orm.Many[Office, Employee]
	Notes      orm.Many[Office, Note]
	Labels     orm.Through[Office, Label, Labeling]
	Desks      orm.Many[Office, Desk]
	Soft       orm.SoftDeletable[Office]
}

var Offices = orm.Define("office_offices", func(b orm.Builder[Office], m *Office) OfficeTable {
	table := orm.TableOf(b, &m.ID)
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return OfficeTable{Table: table, ID: table.Key(), Code: orm.TextOf(b, &m.Code), RegionID: orm.IDOf(b, &m.RegionID), Name: orm.TextOf(b, &m.Name), DeletedAt: deleted, Newest: orm.OneOf(b, &m.Newest), Earliest: orm.OneOf(b, &m.Earliest), TopEarner: orm.OneOf(b, &m.TopEarner), Region: orm.OneOf(b, &m.Region), Headcount: orm.ValueOf(b, &m.Headcount), Colleagues: orm.ManyOf(b, &m.Colleagues), Notes: orm.ManyOf(b, &m.Notes), Labels: orm.ThroughOf(b, &m.Labels), Desks: orm.ManyOf(b, &m.Desks), Soft: orm.SoftDeletes(deleted)}
})

// EmployeeTable binds Employee storage and typed relation slots.
type EmployeeTable struct {
	orm.Table[Employee, model.ID[Employee]]
	ID       orm.Key[Employee, model.ID[Employee]]
	OfficeID orm.Column[Employee, model.ID[Office]]
	Name     orm.Text[Employee, string]
	Salary   orm.Number[Employee, int64]
	HiredAt  orm.Time[Employee, time.Time]
	Region   orm.One[Employee, Region]
}

var Employees = orm.Define("office_employees", func(b orm.Builder[Employee], m *Employee) EmployeeTable {
	table := orm.TableOf(b, &m.ID)
	return EmployeeTable{Table: table, ID: table.Key(), OfficeID: orm.IDOf(b, &m.OfficeID), Name: orm.TextOf(b, &m.Name), Salary: orm.NumberOf(b, &m.Salary), HiredAt: orm.TimeOf(b, &m.HiredAt), Region: orm.OneOf(b, &m.Region)}
})

// NoteTable binds Note storage and typed relation slots.
type NoteTable struct {
	orm.Table[Note, model.ID[Note]]
	ID          orm.Key[Note, model.ID[Note]]
	SubjectType orm.Column[Note, model.MorphName]
	SubjectID   orm.Text[Note, string]
	Body        orm.Text[Note, string]
	Region      orm.One[Note, Region]
	Office      orm.One[Note, Office]
}

var Notes = orm.Define("office_notes", func(b orm.Builder[Note], m *Note) NoteTable {
	table := orm.TableOf(b, &m.ID)
	return NoteTable{Table: table, ID: table.Key(), SubjectType: orm.ColumnOf(b, &m.SubjectType), SubjectID: orm.TextOf(b, &m.SubjectID), Body: orm.TextOf(b, &m.Body), Region: orm.OneOf(b, &m.Region), Office: orm.OneOf(b, &m.Office)}
})

// LabelTable binds Label storage and typed relation slots.
type LabelTable struct {
	orm.Table[Label, model.ID[Label]]
	ID      orm.Key[Label, model.ID[Label]]
	Name    orm.Text[Label, string]
	Offices orm.Through[Label, Office, Labeling]
}

var Labels = orm.Define("office_labels", func(b orm.Builder[Label], m *Label) LabelTable {
	table := orm.TableOf(b, &m.ID)
	return LabelTable{Table: table, ID: table.Key(), Name: orm.TextOf(b, &m.Name), Offices: orm.ThroughOf(b, &m.Offices)}
})

// LabelingTable binds Labeling storage and typed relation slots.
type LabelingTable struct {
	orm.Table[Labeling, model.ID[Labeling]]
	ID            orm.Key[Labeling, model.ID[Labeling]]
	LabelID       orm.Column[Labeling, model.ID[Label]]
	LabelableType orm.Column[Labeling, model.MorphName]
	LabelableID   orm.Text[Labeling, string]
}

var Labelings = orm.Define("office_labelings", func(b orm.Builder[Labeling], m *Labeling) LabelingTable {
	table := orm.TableOf(b, &m.ID)
	return LabelingTable{Table: table, ID: table.Key(), LabelID: orm.IDOf(b, &m.LabelID), LabelableType: orm.ColumnOf(b, &m.LabelableType), LabelableID: orm.TextOf(b, &m.LabelableID)}
})

// DeskTable binds Desk storage and typed relation slots.
type DeskTable struct {
	orm.Table[Desk, model.ID[Desk]]
	ID       orm.Key[Desk, model.ID[Desk]]
	OfficeID orm.Column[Desk, model.ID[Office]]
	Label    orm.Text[Desk, string]
	Office   orm.One[Desk, Office]
}

var Desks = orm.Define("office_desks", func(b orm.Builder[Desk], m *Desk) DeskTable {
	table := orm.TableOf(b, &m.ID)
	return DeskTable{Table: table, ID: table.Key(), OfficeID: orm.IDOf(b, &m.OfficeID), Label: orm.TextOf(b, &m.Label), Office: orm.OneOf(b, &m.Office)}
}, orm.WithGlobalScopes(func(t DeskTable) []orm.GlobalScope[Desk] { return []orm.GlobalScope[Desk]{OpenOfficeScope(t)} }))

// Tables exposes thin declarations for application validation.
func Tables() []orm.CheckedTable {
	return []orm.CheckedTable{Regions.Table, Offices.Table, Employees.Table, Notes.Table, Labels.Table, Labelings.Table, Desks.Table}
}
